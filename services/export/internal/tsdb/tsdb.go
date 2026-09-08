package tsdb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/almuzky/iot/services/export/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store reads telemetry from the Module Service's time-series store.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to TimescaleDB using a libpq DSN.
func New(dsn string) (*Store, error) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// Ping verifies the pool is reachable (used by health checks).
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

const maxExportWindow = 366 * 24 * time.Hour

// ErrInvalidParam indicates caller-supplied node_id / metric failed validation.
var ErrInvalidParam = errInvalidParam{}

type errInvalidParam struct{}

func (errInvalidParam) Error() string { return "invalid node_id or metric" }

// ValidateWindow rejects spans wider than the allowed maximum so a malicious or
// buggy client cannot trigger a full-DB dump / unbounded scan (DoS).
func ValidateWindow(from, to time.Time) (time.Time, time.Time, bool) {
	if to.Before(from) {
		return from, to, false
	}
	if to.Sub(from) > maxExportWindow {
		return from, to, false
	}
	return from, to, true
}

// DecodeCursor decodes an opaque, base64-encoded keyset cursor. A malformed or
// tampered cursor is treated as "no cursor" (start from the beginning), which
// keeps pagination safe and avoids leaking any internal state in the token.
func DecodeCursor(token string) (model.Cursor, bool) {
	var c model.Cursor
	if token == "" {
		return c, false
	}
	raw, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return c, false
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, false
	}
	return c, true
}

// EncodeCursor encodes a keyset cursor into an opaque, URL-safe token.
func EncodeCursor(c model.Cursor) string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(b)
}

// sanitizeIdentifier validates a node_id / metric segment against a strict
// allow-list so user input can only ever be used as a query *value* binding,
// never interpolated into SQL. (Values are still passed as parameters; this is
// defense-in-depth against pathological input.)
func isValidSegment(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	if s == "*" {
		return true
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') && r != '_' && r != '.' && r != '-' && r != ':' && r != '*' {
			return false
		}
	}
	return true
}

// QueryPage returns one page of telemetry using keyset (cursor) pagination.
// Pagination is driven by (time, node_id, metric) so results are stable under
// concurrent inserts and never duplicate or skip a row across pages. The `raw`
// JSONB column is intentionally NOT selected — only safe, public columns are
// returned so the internal schema is never leaked.
func (s *Store) QueryPage(ctx context.Context, q model.ExportQuery, cursor model.Cursor, hasCursor bool) (*model.Page, error) {
	// Validate all caller-supplied segments (defense in depth; values are still
	// bound as parameters below — no string interpolation into SQL).
	for _, n := range q.NodeIDs {
		if !isValidSegment(n) {
			return nil, fmt.Errorf("invalid node_id: %q: %w", n, ErrInvalidParam)
		}
	}
	for _, m := range q.Metrics {
		if !isValidSegment(m) {
			return nil, fmt.Errorf("invalid metric: %q: %w", m, ErrInvalidParam)
		}
	}

	args := []interface{}{}
	arg := func(v interface{}) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	// Only safe, public columns are projected — `raw` JSONB is excluded.
	sql := `SELECT time, node_id, module_id, metric, value FROM telemetry WHERE 1=1`
	hasNodeWildcard := len(q.NodeIDs) == 1 && q.NodeIDs[0] == "*"
	if len(q.NodeIDs) > 0 && !hasNodeWildcard {
		placeholders := make([]string, 0, len(q.NodeIDs))
		for _, n := range q.NodeIDs {
			placeholders = append(placeholders, arg(n))
		}
		sql += ` AND node_id IN (` + strings.Join(placeholders, ",") + `)`
	}

	hasMetricWildcard := len(q.Metrics) == 0 || (len(q.Metrics) == 1 && q.Metrics[0] == "*")
	if len(q.Metrics) > 0 && !hasMetricWildcard {
		placeholders := make([]string, 0, len(q.Metrics))
		for _, m := range q.Metrics {
			placeholders = append(placeholders, arg(m))
		}
		sql += ` AND metric IN (` + strings.Join(placeholders, ",") + `)`
	}
	sql += ` AND time >= ` + arg(q.From) + ` AND time <= ` + arg(q.To)

	// Keyset pagination: rows strictly after the cursor tuple (time, node, metric).
	if hasCursor {
		sql += ` AND (time, node_id, metric) > (` +
			arg(cursor.Time) + `, ` + arg(cursor.Node) + `, ` + arg(cursor.Metric) + `)`
	}

	sql += ` ORDER BY time ASC, node_id ASC, metric ASC LIMIT ` + arg(q.Limit)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query telemetry: %w", err)
	}
	defer rows.Close()

	out := make([]model.TelemetryRow, 0, q.Limit)
	for rows.Next() {
		var r model.TelemetryRow
		var moduleID *string
		if err := rows.Scan(&r.Time, &r.NodeID, &moduleID, &r.Metric, &r.Value); err != nil {
			return nil, fmt.Errorf("scan telemetry row: %w", err)
		}
		r.ModuleID = moduleID
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telemetry rows: %w", err)
	}

	page := &model.Page{Rows: out, HasMore: len(out) == q.Limit}
	if len(out) == q.Limit {
		last := out[len(out)-1]
		page.NextCursor = EncodeCursor(model.Cursor{Time: last.Time, Node: last.NodeID, Metric: last.Metric})
	}
	return page, nil
}

// Count estimates the number of rows in the window (used to size file limits).
func (s *Store) Count(ctx context.Context, q model.ExportQuery) (int, error) {
	args := []interface{}{}
	arg := func(v interface{}) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	sql := `SELECT count(*) FROM telemetry WHERE 1=1`
	if len(q.NodeIDs) > 0 {
		ph := make([]string, 0, len(q.NodeIDs))
		for _, n := range q.NodeIDs {
			ph = append(ph, arg(n))
		}
		sql += ` AND node_id IN (` + strings.Join(ph, ",") + `)`
	}
	if len(q.Metrics) > 0 {
		ph := make([]string, 0, len(q.Metrics))
		for _, m := range q.Metrics {
			ph = append(ph, arg(m))
		}
		sql += ` AND metric IN (` + strings.Join(ph, ",") + `)`
	}
	sql += ` AND time >= ` + arg(q.From) + ` AND time <= ` + arg(q.To)

	var n int
	if err := s.pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count telemetry: %w", err)
	}
	return n, nil
}

// ListNodes returns every node that has telemetry and the metrics available,
// for discovery / the OpenAPI examples.
func (s *Store) ListNodes(ctx context.Context) ([]model.NodeMetric, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT node_id, COALESCE(module_id,''),
		        string_agg(DISTINCT metric, ',' ORDER BY metric)
		 FROM telemetry
		 GROUP BY node_id, module_id
		 ORDER BY node_id`)
	if err != nil {
		log.Printf("[tsdb] list nodes failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	out := make([]model.NodeMetric, 0)
	for rows.Next() {
		var nodeID, moduleID, metricsCSV string
		if err := rows.Scan(&nodeID, &moduleID, &metricsCSV); err != nil {
			return nil, err
		}
		var metrics []string
		if metricsCSV != "" {
			for _, m := range strings.Split(metricsCSV, ",") {
				if m != "" {
					metrics = append(metrics, m)
				}
			}
		}
		out = append(out, model.NodeMetric{NodeID: nodeID, ModuleID: moduleID, Metrics: metrics})
	}
	return out, rows.Err()
}
