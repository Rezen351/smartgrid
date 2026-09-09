package repository

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/almuzky/iot/services/module/internal/model"
	"github.com/google/uuid"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("record not found")

// OutboxRow is a pending event awaiting relay to NATS (ADR-007).
type OutboxRow struct {
	ID      string
	MsgID   string
	Subject string
	Payload string
}

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// DB exposes the underlying handle so callers may run their own transactions
// (used to write a business row + an outbox row atomically).
func (r *Repository) DB() *sql.DB { return r.db }

// Transact runs fn inside a single SQL transaction. fn receives the *sql.Tx to
// use for both the business write and the outbox insert so they commit atomically.
func (r *Repository) Transact(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("[repo] rollback failed: %v (orig err: %v)", rbErr, err)
		}
		return err
	}
	return tx.Commit()
}

// InsertOutboxTx writes an outbox row using the provided transaction handle.
// Callers must use the same tx as the business write (see Transact).
func (r *Repository) InsertOutboxTx(ctx context.Context, tx *sql.Tx, subject, payload, msgID string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO outbox (id, msg_id, subject, payload, sent, created_at) VALUES (?, ?, ?, ?, 0, NOW())`,
		uuid.New().String(), msgID, subject, payload)
	return err
}

// ─── Modules ─────────────────────────────────────────────────────────────────

func (r *Repository) CreateModule(ctx context.Context, m *model.Module) error {
	m.ID = uuid.New().String()
	now := time.Now()
	m.CreatedAt, m.UpdatedAt = now, now
	if m.Config == "" {
		m.Config = "{}"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO modules (id, name, description, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, m.Name, m.Description, m.Config, m.CreatedAt, m.UpdatedAt)
	return err
}

func (r *Repository) ListModules(ctx context.Context) ([]model.Module, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, config, created_at, updated_at
		 FROM modules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Module
	for rows.Next() {
		var m model.Module
		if err := rows.Scan(&m.ID, &m.Name, &m.Description, &m.Config, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) GetModule(ctx context.Context, id string) (*model.Module, error) {
	var m model.Module
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, config, created_at, updated_at
		 FROM modules WHERE id = ?`, id).
		Scan(&m.ID, &m.Name, &m.Description, &m.Config, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	nodes, err := r.ListNodesByModule(ctx, id)
	if err != nil {
		return nil, err
	}
	m.Nodes = nodes
	return &m, nil
}

func (r *Repository) UpdateModule(ctx context.Context, id string, req model.UpdateModuleRequest) (*model.Module, error) {
	m, err := r.GetModule(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		m.Name = *req.Name
	}
	if req.Description != nil {
		m.Description = *req.Description
	}
	if req.Config != nil {
		m.Config = *req.Config
	}
	m.UpdatedAt = time.Now()
	_, err = r.db.ExecContext(ctx,
		`UPDATE modules SET name = ?, description = ?, config = ?, updated_at = ? WHERE id = ?`,
		m.Name, m.Description, m.Config, m.UpdatedAt, id)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *Repository) DeleteModule(ctx context.Context, id string) error {
	// Detach nodes (unpair) instead of deleting them — they remain discoverable.
	if _, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET module_id = NULL, paired = 0, updated_at = ? WHERE module_id = ?`,
		time.Now(), id); err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM modules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ModuleExists(ctx context.Context, id string) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM modules WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ─── Nodes ───────────────────────────────────────────────────────────────────

// UpsertDiscovered inserts a newly discovered node or refreshes an existing one.
// Returns true if the node was newly created.
func (r *Repository) UpsertDiscovered(ctx context.Context, n *model.Node) (bool, error) {
	existing, err := r.GetNodeByNodeID(ctx, n.NodeID)
	now := time.Now()
	if errors.Is(err, ErrNotFound) {
		n.ID = uuid.New().String()
		n.DiscoveredAt, n.CreatedAt, n.UpdatedAt = now, now, now
		n.LastSeenAt = &now
		if n.Status == "" {
			n.Status = model.StatusUnknown
		}
		_, err := r.db.ExecContext(ctx,
			`INSERT INTO nodes (id, node_id, module_id, name, mac, ip, fw_version, status, paired, last_seen_at, discovered_at, created_at, updated_at)
			 VALUES (?, ?, NULL, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
			n.ID, n.NodeID, n.Name, n.MAC, n.IP, n.FWVersion, n.Status, n.LastSeenAt, n.DiscoveredAt, n.CreatedAt, n.UpdatedAt)
		return true, err
	}
	if err != nil {
		return false, err
	}
	// Refresh mutable fields; keep pairing state intact.
	if n.MAC == "" {
		n.MAC = existing.MAC
	}
	if n.FWVersion == "" {
		n.FWVersion = existing.FWVersion
	}
	status := n.Status
	if status == "" {
		status = model.StatusUnknown
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE nodes SET mac = ?, ip = ?, fw_version = ?, status = ?, last_seen_at = ?, updated_at = ? WHERE node_id = ?`,
		n.MAC, n.IP, n.FWVersion, status, now, now, n.NodeID)
	return false, err
}

// UpdateStatus updates only the connectivity status + last_seen of a node.
// The IP is only overwritten when a non-empty value is provided, so that
// periodic status/LWT messages (which typically omit ip) do not erase the
// address learned earlier via discovery or a richer status payload.
func (r *Repository) UpdateStatus(ctx context.Context, nodeID, status, ip string) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET status = ?, last_seen_at = ?, ip = COALESCE(NULLIF(?, ''), ip), updated_at = ? WHERE node_id = ?`,
		status, now, ip, now, nodeID)
	return err
}

// TouchNode marks a node alive: refreshes last_seen_at and sets status online.
// Called for ANY MQTT payload (telemetry/heartbeat/etc.), not just the status
// topic, so "last seen" stays fresh between infrequent LWT/online events.
func (r *Repository) TouchNode(ctx context.Context, nodeID string) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET last_seen_at = ?, status = ?, updated_at = ? WHERE node_id = ?`,
		now, model.StatusOnline, now, nodeID)
	return err
}

func (r *Repository) GetNodeByNodeID(ctx context.Context, nodeID string) (*model.Node, error) {
	return r.scanNode(r.db.QueryRowContext(ctx, nodeSelect+` WHERE node_id = ?`, nodeID))
}

// ─── Node tag mappings (modular telemetry acquisition) ───────────────────────

func (r *Repository) GetModuleIDByNode(ctx context.Context, nodeID string) (*string, error) {
	var moduleID sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT module_id FROM nodes WHERE node_id = ?`, nodeID).Scan(&moduleID)
	if err != nil {
		return nil, err
	}
	if !moduleID.Valid {
		return nil, nil
	}
	v := moduleID.String
	return &v, nil
}

func (r *Repository) ListNodeTags(ctx context.Context, nodeID string) ([]model.NodeTag, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, node_id, kind, source_key, tag_name, display_name, unit, data_type, enabled, created_at, updated_at
		 FROM node_tags WHERE node_id = ? AND kind IN ('sensor','') ORDER BY source_key`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.NodeTag{}
	for rows.Next() {
		var t model.NodeTag
		var kind, sourceKey string
		var created, updated time.Time
		if err := rows.Scan(&t.ID, &t.NodeID, &kind, &sourceKey, &t.TagName, &t.DisplayName, &t.Unit, &t.DataType, &t.Enabled, &created, &updated); err != nil {
			return nil, err
		}
		t.Kind, t.SourceKey = kind, sourceKey
		t.CreatedAt, t.UpdatedAt = created, updated
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListActuatorTags returns only actuator (kind="actuator") tags for a node — the
// controllable outputs the user explicitly mapped.
func (r *Repository) ListActuatorTags(ctx context.Context, nodeID string) ([]model.NodeTag, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, node_id, kind, source_key, tag_name, display_name, unit, data_type, enabled, created_at, updated_at
		 FROM node_tags WHERE node_id = ? AND kind = 'actuator' ORDER BY source_key`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.NodeTag{}
	for rows.Next() {
		var t model.NodeTag
		var kind, sourceKey string
		var created, updated time.Time
		if err := rows.Scan(&t.ID, &t.NodeID, &kind, &sourceKey, &t.TagName, &t.DisplayName, &t.Unit, &t.DataType, &t.Enabled, &created, &updated); err != nil {
			return nil, err
		}
		t.Kind, t.SourceKey = kind, sourceKey
		t.CreatedAt, t.UpdatedAt = created, updated
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpsertNodeTag inserts or updates a single tag mapping for a node.
func (r *Repository) UpsertNodeTag(ctx context.Context, t *model.NodeTag) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	now := time.Now()
	t.CreatedAt, t.UpdatedAt = now, now
	if t.DataType == "" {
		t.DataType = "float"
	}
	if t.Kind == "" {
		t.Kind = "sensor"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO node_tags (id, node_id, source_key, kind, tag_name, display_name, unit, data_type, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   tag_name = VALUES(tag_name), display_name = VALUES(display_name),
		   unit = VALUES(unit), data_type = VALUES(data_type), enabled = VALUES(enabled), updated_at = VALUES(updated_at)`,
		t.ID, t.NodeID, t.SourceKey, t.Kind, t.TagName, t.DisplayName, t.Unit, t.DataType, t.Enabled, t.CreatedAt, t.UpdatedAt)
	return err
}

func (r *Repository) DeleteNodeTag(ctx context.Context, nodeID, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM node_tags WHERE node_id = ? AND id = ?`, nodeID, id)
	return err
}

// DeleteSensorTagsExcept removes every sensor-kind tag for a node whose id is
// NOT in keepIDs. Used by SaveNodeTags to make it a true replace: rows the user
// removed from the mapping are actually deleted instead of merely left untouched.
// Actuator (kind="actuator") tags are never affected.
func (r *Repository) DeleteSensorTagsExcept(ctx context.Context, nodeID string, keepIDs []string) error {
	if len(keepIDs) == 0 {
		_, err := r.db.ExecContext(ctx, `DELETE FROM node_tags WHERE node_id = ? AND kind IN ('sensor','')`, nodeID)
		return err
	}
	ph := strings.Repeat("?,", len(keepIDs))
	ph = ph[:len(ph)-1]
	args := make([]any, 0, len(keepIDs)+1)
	args = append(args, nodeID)
	for _, id := range keepIDs {
		args = append(args, id)
	}
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM node_tags WHERE node_id = ? AND kind IN ('sensor','') AND id NOT IN (`+ph+`)`, args...)
	return err
}

func (r *Repository) ListNodes(ctx context.Context, paired *bool, moduleID, status string) ([]model.Node, error) {
	q := nodeSelect + ` WHERE 1=1`
	var args []any
	if paired != nil {
		q += ` AND paired = ?`
		if *paired {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if moduleID != "" {
		q += ` AND module_id = ?`
		args = append(args, moduleID)
	}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY discovered_at DESC`

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Node
	for rows.Next() {
		n, err := r.scanNodeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (r *Repository) ListNodesByModule(ctx context.Context, moduleID string) ([]model.Node, error) {
	rows, err := r.db.QueryContext(ctx, nodeSelect+` WHERE module_id = ? ORDER BY discovered_at DESC`, moduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Node
	for rows.Next() {
		n, err := r.scanNodeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// Pair assigns a discovered node to a module.
func (r *Repository) Pair(ctx context.Context, nodeID, moduleID, name string) (*model.Node, error) {
	node, err := r.GetNodeByNodeID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = node.Name
	}
	now := time.Now()
	_, err = r.db.ExecContext(ctx,
		`UPDATE nodes SET module_id = ?, paired = 1, name = ?, updated_at = ? WHERE node_id = ?`,
		moduleID, name, now, nodeID)
	if err != nil {
		return nil, err
	}
	return r.GetNodeByNodeID(ctx, nodeID)
}

// Unpair detaches a node from its module (returns it to the discovered pool).
func (r *Repository) Unpair(ctx context.Context, nodeID string) (*model.Node, error) {
	if _, err := r.GetNodeByNodeID(ctx, nodeID); err != nil {
		return nil, err
	}
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET module_id = NULL, paired = 0, updated_at = ? WHERE node_id = ?`,
		now, nodeID)
	if err != nil {
		return nil, err
	}
	return r.GetNodeByNodeID(ctx, nodeID)
}

func (r *Repository) DeleteNode(ctx context.Context, nodeID string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM nodes WHERE node_id = ?`, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── scan helpers ────────────────────────────────────────────────────────────

const nodeSelect = `SELECT id, node_id, module_id, name, mac, ip, fw_version, status, paired, last_seen_at, discovered_at, created_at, updated_at FROM nodes`

type scanner interface {
	Scan(dest ...any) error
}

func (r *Repository) scanNode(row scanner) (*model.Node, error) {
	n, err := scanNodeFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (r *Repository) scanNodeRows(rows *sql.Rows) (*model.Node, error) {
	return scanNodeFrom(rows)
}

func scanNodeFrom(s scanner) (*model.Node, error) {
	var n model.Node
	var moduleID sql.NullString
	var lastSeen sql.NullTime
	if err := s.Scan(&n.ID, &n.NodeID, &moduleID, &n.Name, &n.MAC, &n.IP, &n.FWVersion,
		&n.Status, &n.Paired, &lastSeen, &n.DiscoveredAt, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	if moduleID.Valid {
		n.ModuleID = &moduleID.String
	}
	if lastSeen.Valid {
		n.LastSeenAt = &lastSeen.Time
	}
	return &n, nil
}

// ─── Outbox relay (ADR-007) ───────────────────────────────────────────────────

// ListUnsentOutbox returns up to limit pending outbox rows (sent=false), oldest first.
func (r *Repository) ListUnsentOutbox(ctx context.Context, limit int) ([]OutboxRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, msg_id, subject, payload FROM outbox WHERE sent = 0 ORDER BY created_at ASC, id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutboxRow{}
	for rows.Next() {
		var o OutboxRow
		if err := rows.Scan(&o.ID, &o.MsgID, &o.Subject, &o.Payload); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// MarkOutboxSent marks a single outbox row as delivered (idempotent).
func (r *Repository) MarkOutboxSent(ctx context.Context, id string) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE outbox SET sent = 1, sent_at = ? WHERE id = ? AND sent = 0`, now, id)
	return err
}
