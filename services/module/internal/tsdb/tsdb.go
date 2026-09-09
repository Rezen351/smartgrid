package tsdb

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store writes time-series telemetry readings into the TimescaleDB hypertable.
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

// WriteReading appends one measurement to the telemetry hypertable.
// moduleID may be nil (unpaired node); it is stored as NULL.
func (s *Store) WriteReading(ctx context.Context, nodeID string, moduleID *string, metric string, value float64, raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO telemetry (time, node_id, module_id, metric, value, raw)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		time.Now().UTC(), nodeID, moduleID, metric, value, raw,
	)
	if err != nil {
		log.Printf("[tsdb] write reading failed node=%s metric=%s: %v", nodeID, metric, err)
	}
	return err
}
