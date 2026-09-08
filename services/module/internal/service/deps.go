package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/almuzky/iot/services/module/internal/model"
	"github.com/almuzky/iot/services/module/internal/repository"
)

// Repository is the persistence seam for modules, nodes, and tags. The
// concrete implementation is *repository.Repository; unit tests inject a fake.
type Repository interface {
	Transact(ctx context.Context, fn func(tx *sql.Tx) error) error
	InsertOutboxTx(ctx context.Context, tx *sql.Tx, subject, payload, msgID string) error
	CreateModule(ctx context.Context, m *model.Module) error
	ListModules(ctx context.Context) ([]model.Module, error)
	GetModule(ctx context.Context, id string) (*model.Module, error)
	UpdateModule(ctx context.Context, id string, req model.UpdateModuleRequest) (*model.Module, error)
	DeleteModule(ctx context.Context, id string) error
	ModuleExists(ctx context.Context, id string) (bool, error)
	UpsertDiscovered(ctx context.Context, n *model.Node) (bool, error)
	UpdateStatus(ctx context.Context, nodeID, status, ip string) error
	TouchNode(ctx context.Context, nodeID string) error
	GetNodeByNodeID(ctx context.Context, nodeID string) (*model.Node, error)
	GetModuleIDByNode(ctx context.Context, nodeID string) (*string, error)
	ListNodeTags(ctx context.Context, nodeID string) ([]model.NodeTag, error)
	ListActuatorTags(ctx context.Context, nodeID string) ([]model.NodeTag, error)
	UpsertNodeTag(ctx context.Context, t *model.NodeTag) error
	DeleteNodeTag(ctx context.Context, nodeID, id string) error
	DeleteSensorTagsExcept(ctx context.Context, nodeID string, keepIDs []string) error
	ListNodes(ctx context.Context, paired *bool, moduleID, status string) ([]model.Node, error)
	ListNodesByModule(ctx context.Context, moduleID string) ([]model.Node, error)
	Pair(ctx context.Context, nodeID, moduleID, name string) (*model.Node, error)
	Unpair(ctx context.Context, nodeID string) (*model.Node, error)
	DeleteNode(ctx context.Context, nodeID string) error
	ListUnsentOutbox(ctx context.Context, limit int) ([]repository.OutboxRow, error)
	MarkOutboxSent(ctx context.Context, id string) error
}

// StatusCache is the realtime node-status and fast-query cache seam. The concrete
// implementation is *cache.StatusCache; unit tests inject a fake.
type StatusCache interface {
	SetStatus(ctx context.Context, nodeID, status string, ttl time.Duration)
	GetStatus(ctx context.Context, nodeID string) string
	SetLatest(ctx context.Context, nodeID string, raw []byte, ttl time.Duration)
	GetLatest(ctx context.Context, nodeID string) []byte
	GetCachedModules(ctx context.Context) ([]model.Module, bool)
	SetCachedModules(ctx context.Context, modules []model.Module, ttl time.Duration)
	InvalidateModules(ctx context.Context)
}

// TSDB is the telemetry timeseries seam. The concrete implementation is
// *tsdb.Store; unit tests inject a fake (or leave nil to skip persist).
type TSDB interface {
	WriteReading(ctx context.Context, nodeID string, moduleID *string, metric string, value float64, raw json.RawMessage) error
}
