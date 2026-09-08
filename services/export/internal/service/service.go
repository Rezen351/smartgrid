package service

import (
	"context"
	"time"

	"github.com/almuzky/iot/services/export/internal/model"
	"github.com/almuzky/iot/services/export/internal/tsdb"
)

// Service implements Export business logic over the time-series store.
type Service struct {
	store *tsdb.Store
}

// New wires the Export Service with its time-series store.
func New(store *tsdb.Store) *Service {
	return &Service{store: store}
}

// QueryPage proxies a keyset-paginated query with window validation.
func (s *Service) QueryPage(ctx context.Context, q model.ExportQuery, cursor model.Cursor, hasCursor bool) (*model.Page, error) {
	if _, _, ok := tsdb.ValidateWindow(q.From, q.To); !ok {
		return nil, ErrWindowTooLarge
	}
	return s.store.QueryPage(ctx, q, cursor, hasCursor)
}

// Count proxies the row count for a query window.
func (s *Service) Count(ctx context.Context, q model.ExportQuery) (int, error) {
	if _, _, ok := tsdb.ValidateWindow(q.From, q.To); !ok {
		return 0, ErrWindowTooLarge
	}
	return s.store.Count(ctx, q)
}

// ListNodes proxies node/metric discovery.
func (s *Service) ListNodes(ctx context.Context) ([]model.NodeMetric, error) {
	return s.store.ListNodes(ctx)
}

// Ping reports store connectivity.
func (s *Service) Ping(ctx context.Context) error {
	return s.store.Ping(ctx)
}

// ErrWindowTooLarge indicates a request exceeding the maximum export span.
var ErrWindowTooLarge = errWindowTooLarge{}

type errWindowTooLarge struct{}

func (errWindowTooLarge) Error() string {
	return "requested time range exceeds the 366-day export limit"
}

// DefaultWindow returns a safe default [to-24h, now] window.
func DefaultWindow() (time.Time, time.Time) {
	to := time.Now().UTC()
	return to.Add(-24 * time.Hour), to
}
