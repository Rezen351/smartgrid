// Package outbox implements the Transactional Outbox relay worker (ADR-007).
//
// Business code writes an outbox row (subject + payload + msg_id) in the same
// DB transaction as the business row. This relay polls unsent rows, publishes
// them to NATS (Core or JetStream) with a Nats-Msg-Id header (publisher-side
// dedupe signal), then marks the row sent. If NATS is unavailable the row stays
// unsent and is retried on the next poll, so no event is ever lost on a publish
// failure. Consumer-side idempotency (Audit/Notification/Analytics dedupe the
// msg_id in Redis/DB) guarantees no duplicate effect on redelivery.
package outbox

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/almuzky/iot/services/module/internal/repository"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// Relay drains the outbox table and publishes each row to NATS exactly once
// (effectively) via the Nats-Msg-Id header + consumer-side dedup.
type Relay struct {
	repo   *repository.Repository
	nc     *nats.Conn
	poll   time.Duration
	batch  int
	header string
}

// New constructs a relay. nc may be nil (NATS not yet connected); the relay
// will keep the rows unsent and retry once a connection is supplied via SetNATS.
func New(repo *repository.Repository, nc *nats.Conn) *Relay {
	return &Relay{
		repo:   repo,
		nc:     nc,
		poll:   2 * time.Second,
		batch:  100,
		header: "Nats-Msg-Id",
	}
}

// SetNATS supplies (or replaces) the NATS connection used for publishing.
func (r *Relay) SetNATS(nc *nats.Conn) { r.nc = nc }

// Start runs the relay loop until ctx is cancelled (graceful shutdown).
func (r *Relay) Start(ctx context.Context) {
	ticker := time.NewTicker(r.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[outbox] relay shutting down")
			return
		case <-ticker.C:
			r.drain(ctx)
		}
	}
}

func (r *Relay) drain(ctx context.Context) {
	if r.nc == nil {
		return
	}
	rows, err := r.repo.ListUnsentOutbox(ctx, r.batch)
	if err != nil {
		log.Printf("[outbox] list unsent failed: %v", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	for _, row := range rows {
		if err := r.publish(ctx, row); err != nil {
			log.Printf("[outbox] publish failed subject=%s msg_id=%s: %v", row.Subject, row.MsgID, err)
			return // keep remaining rows unsent; retry next poll
		}
		if err := r.repo.MarkOutboxSent(ctx, row.ID); err != nil {
			log.Printf("[outbox] mark sent failed id=%s: %v", row.ID, err)
			return
		}
	}
	log.Printf("[outbox] relayed %d event(s)", len(rows))
}

func (r *Relay) publish(ctx context.Context, row repository.OutboxRow) error {
	enriched, err := withMsgID([]byte(row.Payload), row.MsgID)
	if err != nil {
		enriched = []byte(row.Payload) // fall back to raw payload; header still carries the id
	}
	return r.nc.PublishMsg(&nats.Msg{
		Subject: row.Subject,
		Data:    enriched,
		Header:  nats.Header{r.header: []string{row.MsgID}},
	})
}

// withMsgID ensures the JSON payload carries a top-level "msg_id" field used by
// consumer-side idempotency (Audit/Notification/Analytics dedupe in Redis/DB).
func withMsgID(payload []byte, msgID string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	if _, ok := m["msg_id"]; ok {
		return payload, nil
	}
	m["msg_id"] = msgID
	return json.Marshal(m)
}

// NewMsgID returns a fresh idempotency key for an outbox row.
func NewMsgID() string { return uuid.NewString() }
