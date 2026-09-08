package service

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"
)

// batchEntry accumulates telemetry readings for a single (node, metric) pair
// over one aggregation window (1 minute).
type batchEntry struct {
	NodeID   string  `json:"node_id"`
	ModuleID string  `json:"module_id"`
	Metric   string  `json:"metric"`
	Count    int     `json:"count"`
	Sum      float64 `json:"sum"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Last     float64 `json:"last"`
	FirstTS  int64   `json:"first_ts"`
	LastTS   int64   `json:"last_ts"`
}

// telemetryBatcher is a thread-safe in-memory aggregator. Readings are added as
// they are ingested; a periodic flush emits the window as a `telemetry.batch`.
type telemetryBatcher struct {
	mu      sync.Mutex
	entries map[string]*batchEntry
}

func newTelemetryBatcher() *telemetryBatcher {
	return &telemetryBatcher{entries: make(map[string]*batchEntry)}
}

func (b *telemetryBatcher) add(nodeID, moduleID, metric string, value float64, ts int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := nodeID + "\x00" + metric
	e, ok := b.entries[key]
	if !ok {
		e = &batchEntry{NodeID: nodeID, ModuleID: moduleID, Metric: metric, Min: value, Max: value, FirstTS: ts}
		b.entries[key] = e
	}
	e.Count++
	e.Sum += value
	if value < e.Min {
		e.Min = value
	}
	if value > e.Max {
		e.Max = value
	}
	e.Last = value
	e.LastTS = ts
}

// flush returns the accumulated window and resets it for the next interval.
func (b *telemetryBatcher) flush() []*batchEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) == 0 {
		return nil
	}
	out := make([]*batchEntry, 0, len(b.entries))
	for _, e := range b.entries {
		out = append(out, e)
	}
	b.entries = make(map[string]*batchEntry)
	return out
}

// ─── Telemetry batch publisher (telemetry.batch) ────────────────────────────

// StartBatchPublisher periodically flushes the aggregation window and publishes
// a `telemetry.batch` NATS message containing per-node/metric aggregates.
// Downstream services (Analytics, Alert) consume it instead of every raw reading.
// On context cancellation it performs one final flush so no readings are lost.
func (s *ModuleService) StartBatchPublisher(ctx context.Context, interval time.Duration) {
	if s.nats == nil {
		log.Println("[svc] telemetry.batch publisher disabled: NATS unavailable")
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	log.Printf("[svc] telemetry.batch publisher started (interval=%s)", interval)
	for {
		select {
		case <-ctx.Done():
			s.flushAndPublish(interval)
			return
		case <-ticker.C:
			s.flushAndPublish(interval)
		}
	}
}

func (s *ModuleService) flushAndPublish(interval time.Duration) {
	entries := s.batch.flush()
	if len(entries) == 0 {
		return
	}
	rows := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		avg := 0.0
		if e.Count > 0 {
			avg = e.Sum / float64(e.Count)
		}
		rows = append(rows, map[string]interface{}{
			"node_id":   e.NodeID,
			"module_id": e.ModuleID,
			"metric":    e.Metric,
			"count":     e.Count,
			"sum":       e.Sum,
			"min":       e.Min,
			"max":       e.Max,
			"avg":       avg,
			"last":      e.Last,
			"first_ts":  e.FirstTS,
			"last_ts":   e.LastTS,
		})
	}
	payload, err := json.Marshal(map[string]interface{}{
		"window":    interval.String(),
		"rows":      rows,
		"row_count": len(rows),
		"ts":        time.Now().UnixMilli(),
	})
	if err != nil {
		log.Printf("[svc] telemetry.batch marshal failed: %v", err)
		return
	}
	// Prefer JetStream so the batch survives an Analytics outage/restart (durable
	// replay). Fall back to core NATS (best-effort, no replay) only if JetStream
	// is unavailable.
	if s.js != nil {
		if err := s.js.Publish("telemetry.batch", payload); err != nil {
			log.Printf("[nats] publish telemetry.batch (js) failed: %v", err)
			return
		}
		log.Printf("[nats] published telemetry.batch (%d aggregates)", len(rows))
		return
	}
	if s.nats != nil {
		if err := s.nats.Publish("telemetry.batch", payload); err != nil {
			log.Printf("[nats] publish telemetry.batch failed: %v", err)
			return
		}
		log.Printf("[nats] published telemetry.batch (%d aggregates)", len(rows))
	}
}
