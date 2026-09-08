package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/almuzky/iot/services/module/internal/model"
	"github.com/almuzky/iot/services/module/internal/repository"
)

// ─── Fakes ──────────────────────────────────────────────────────────────────

type fakeRepo struct {
	modules      map[string]*model.Module
	nodes        map[string]*model.Node
	nodeTags     map[string][]model.NodeTag
	actTags      map[string][]model.NodeTag
	moduleExists bool
	nodeExists   bool
	listNodes    []model.Node
	err          error
	discovered   bool
	transactErr  error
	outbox       []repository.OutboxRow
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		modules:      map[string]*model.Module{},
		nodes:        map[string]*model.Node{},
		nodeTags:     map[string][]model.NodeTag{},
		actTags:      map[string][]model.NodeTag{},
		moduleExists: true,
		nodeExists:   true,
	}
}

func (f *fakeRepo) Transact(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if f.transactErr != nil {
		return f.transactErr
	}
	return fn(nil)
}
func (f *fakeRepo) InsertOutboxTx(ctx context.Context, tx *sql.Tx, subject, payload, msgID string) error {
	f.outbox = append(f.outbox, repository.OutboxRow{ID: msgID, Subject: subject, Payload: payload})
	return f.err
}
func (f *fakeRepo) CreateModule(ctx context.Context, m *model.Module) error {
	if f.err != nil {
		return f.err
	}
	m.ID = "mod-" + m.Name
	f.modules[m.ID] = m
	return nil
}
func (f *fakeRepo) ListModules(ctx context.Context) ([]model.Module, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]model.Module, 0, len(f.modules))
	for _, m := range f.modules {
		out = append(out, *m)
	}
	return out, nil
}
func (f *fakeRepo) GetModule(ctx context.Context, id string) (*model.Module, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.modules[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return m, nil
}
func (f *fakeRepo) UpdateModule(ctx context.Context, id string, req model.UpdateModuleRequest) (*model.Module, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.modules[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if req.Name != nil {
		m.Name = *req.Name
	}
	if req.Description != nil {
		m.Description = *req.Description
	}
	return m, nil
}
func (f *fakeRepo) DeleteModule(ctx context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.modules[id]; !ok {
		return repository.ErrNotFound
	}
	delete(f.modules, id)
	return nil
}
func (f *fakeRepo) ModuleExists(ctx context.Context, id string) (bool, error) {
	return f.moduleExists, nil
}
func (f *fakeRepo) UpsertDiscovered(ctx context.Context, n *model.Node) (bool, error) {
	created := false
	if _, ok := f.nodes[n.NodeID]; !ok {
		created = true
	}
	f.nodes[n.NodeID] = n
	return created, nil
}
func (f *fakeRepo) UpdateStatus(ctx context.Context, nodeID, status, ip string) error {
	if f.err != nil {
		return f.err
	}
	if n, ok := f.nodes[nodeID]; ok {
		n.Status = status
	}
	return nil
}
func (f *fakeRepo) TouchNode(ctx context.Context, nodeID string) error { return f.err }
func (f *fakeRepo) GetNodeByNodeID(ctx context.Context, nodeID string) (*model.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	n, ok := f.nodes[nodeID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return n, nil
}
func (f *fakeRepo) GetModuleIDByNode(ctx context.Context, nodeID string) (*string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if n, ok := f.nodes[nodeID]; ok && n.ModuleID != nil {
		return n.ModuleID, nil
	}
	return nil, nil
}
func (f *fakeRepo) ListNodeTags(ctx context.Context, nodeID string) ([]model.NodeTag, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.nodeTags[nodeID], nil
}
func (f *fakeRepo) ListActuatorTags(ctx context.Context, nodeID string) ([]model.NodeTag, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.actTags[nodeID], nil
}
func (f *fakeRepo) UpsertNodeTag(ctx context.Context, t *model.NodeTag) error {
	if f.err != nil {
		return f.err
	}
	if t.Kind == "actuator" {
		f.actTags[t.NodeID] = append(f.actTags[t.NodeID], *t)
	} else {
		f.nodeTags[t.NodeID] = append(f.nodeTags[t.NodeID], *t)
	}
	return nil
}
func (f *fakeRepo) DeleteNodeTag(ctx context.Context, nodeID, id string) error {
	if f.err != nil {
		return f.err
	}
	tags := f.nodeTags[nodeID]
	for i, t := range tags {
		if t.ID == id {
			f.nodeTags[nodeID] = append(tags[:i], tags[i+1:]...)
			break
		}
	}
	return nil
}
func (f *fakeRepo) DeleteSensorTagsExcept(ctx context.Context, nodeID string, keepIDs []string) error {
	return f.err
}
func (f *fakeRepo) ListNodes(ctx context.Context, paired *bool, moduleID, status string) ([]model.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.listNodes != nil {
		return f.listNodes, nil
	}
	out := make([]model.Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		out = append(out, *n)
	}
	return out, nil
}
func (f *fakeRepo) ListNodesByModule(ctx context.Context, moduleID string) ([]model.Node, error) {
	return nil, f.err
}
func (f *fakeRepo) Pair(ctx context.Context, nodeID, moduleID, name string) (*model.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	if _, ok := f.nodes[nodeID]; !ok {
		return nil, repository.ErrNotFound
	}
	n := f.nodes[nodeID]
	mid := moduleID
	n.ModuleID = &mid
	n.Paired = true
	n.Name = name
	return n, nil
}
func (f *fakeRepo) Unpair(ctx context.Context, nodeID string) (*model.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	n, ok := f.nodes[nodeID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	n.Paired = false
	n.ModuleID = nil
	return n, nil
}
func (f *fakeRepo) DeleteNode(ctx context.Context, nodeID string) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.nodes[nodeID]; !ok {
		return repository.ErrNotFound
	}
	delete(f.nodes, nodeID)
	return nil
}
func (f *fakeRepo) ListUnsentOutbox(ctx context.Context, limit int) ([]repository.OutboxRow, error) {
	return f.outbox, f.err
}
func (f *fakeRepo) MarkOutboxSent(ctx context.Context, id string) error { return f.err }

type fakeStatusCache struct {
	status  map[string]string
	latest  map[string][]byte
	modules []model.Module
}

func (c *fakeStatusCache) SetStatus(ctx context.Context, nodeID, status string, ttl time.Duration) {
	if c.status == nil {
		c.status = map[string]string{}
	}
	c.status[nodeID] = status
}
func (c *fakeStatusCache) GetStatus(ctx context.Context, nodeID string) string {
	if c.status == nil {
		return ""
	}
	return c.status[nodeID]
}
func (c *fakeStatusCache) SetLatest(ctx context.Context, nodeID string, raw []byte, ttl time.Duration) {
	if c.latest == nil {
		c.latest = map[string][]byte{}
	}
	c.latest[nodeID] = raw
}
func (c *fakeStatusCache) GetLatest(ctx context.Context, nodeID string) []byte {
	if c.latest == nil {
		return nil
	}
	return c.latest[nodeID]
}
func (c *fakeStatusCache) GetCachedModules(ctx context.Context) ([]model.Module, bool) {
	if c.modules != nil {
		return c.modules, true
	}
	return nil, false
}
func (c *fakeStatusCache) SetCachedModules(ctx context.Context, modules []model.Module, ttl time.Duration) {
	c.modules = modules
}
func (c *fakeStatusCache) InvalidateModules(ctx context.Context) {
	c.modules = nil
}

type fakeTSDB struct {
	written []string
	err     error
}

func (t *fakeTSDB) WriteReading(ctx context.Context, nodeID string, moduleID *string, metric string, value float64, raw json.RawMessage) error {
	if t.err != nil {
		return t.err
	}
	t.written = append(t.written, metric)
	return nil
}

type fakeNATS struct{ count int }

func (n *fakeNATS) Publish(subject string, data []byte) error {
	if n == nil {
		return errors.New("nil nats")
	}
	n.count++
	return nil
}

type fakeJS struct{ count int }

func (n *fakeJS) Publish(subject string, data []byte) error {
	if n == nil {
		return errors.New("nil js")
	}
	n.count++
	return nil
}

func newSvc(repo Repository, c StatusCache, ts TSDB) *ModuleService {
	return New(repo, c, &fakeNATS{}, &fakeJS{}, ts)
}

// ─── Tests ─────────────────────────────────────────────────────────────────

func TestCreateModuleNameRequired(t *testing.T) {
	svc := newSvc(newFakeRepo(), &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.CreateModule(context.Background(), model.CreateModuleRequest{Name: ""}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestCreateModuleSuccess(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	m, err := svc.CreateModule(context.Background(), model.CreateModuleRequest{Name: "Greenhouse-A"})
	if err != nil || m.ID == "" {
		t.Fatalf("expected module created, got %v err %v", m, err)
	}
}

func TestListModules(t *testing.T) {
	repo := newFakeRepo()
	repo.modules["m1"] = &model.Module{ID: "m1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	ms, err := svc.ListModules(context.Background())
	if err != nil || len(ms) != 1 {
		t.Fatalf("expected 1 module, got %v err %v", ms, err)
	}
}

func TestGetModuleNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.GetModule(context.Background(), "x"); !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("expected ErrModuleNotFound, got %v", err)
	}
}

func TestUpdateModuleNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.UpdateModule(context.Background(), "x", model.UpdateModuleRequest{}); !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("expected ErrModuleNotFound, got %v", err)
	}
}

func TestUpdateModuleSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.modules["m1"] = &model.Module{ID: "m1", Name: "old"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	newName := "new"
	m, err := svc.UpdateModule(context.Background(), "m1", model.UpdateModuleRequest{Name: &newName})
	if err != nil || m.Name != "new" {
		t.Fatalf("expected updated name, got %v err %v", m, err)
	}
}

func TestDeleteModuleNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if err := svc.DeleteModule(context.Background(), "x"); !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("expected ErrModuleNotFound, got %v", err)
	}
}

func TestDeleteModuleSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.modules["m1"] = &model.Module{ID: "m1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if err := svc.DeleteModule(context.Background(), "m1"); err != nil {
		t.Fatal(err)
	}
	if len(repo.modules) != 0 {
		t.Error("expected module removed")
	}
}

func TestListNodes(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	ns, err := svc.ListNodes(context.Background(), nil, "", "")
	if err != nil || len(ns) != 1 {
		t.Fatalf("expected 1 node, got %v err %v", ns, err)
	}
}

func TestListDiscovered(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	ns, err := svc.ListDiscovered(context.Background())
	if err != nil || len(ns) != 1 {
		t.Fatalf("expected 1 discovered node, got %v err %v", ns, err)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.GetNode(context.Background(), "x"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestPairModuleNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.moduleExists = false
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.Pair(context.Background(), "n1", model.PairRequest{ModuleID: "m1"}); !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("expected ErrModuleNotFound, got %v", err)
	}
}

func TestPairNodeNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.Pair(context.Background(), "n1", model.PairRequest{ModuleID: "m1"}); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestPairSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	n, err := svc.Pair(context.Background(), "n1", model.PairRequest{ModuleID: "m1", Name: "Node1"})
	if err != nil || !n.Paired {
		t.Fatalf("expected paired node, got %v err %v", n, err)
	}
}

func TestUnpairNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.Unpair(context.Background(), "n1"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestUnpairSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1", Paired: true}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	n, err := svc.Unpair(context.Background(), "n1")
	if err != nil || n.Paired {
		t.Fatalf("expected unpaired node, got %v err %v", n, err)
	}
}

func TestDeleteNodeNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if err := svc.DeleteNode(context.Background(), "n1"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestDeleteNodeSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if err := svc.DeleteNode(context.Background(), "n1"); err != nil {
		t.Fatal(err)
	}
}

func TestHandleDiscoveryMissingNodeID(t *testing.T) {
	svc := newSvc(newFakeRepo(), &fakeStatusCache{}, &fakeTSDB{})
	if err := svc.HandleDiscovery(context.Background(), model.DiscoveryMessage{}); err == nil {
		t.Error("expected error for missing node_id")
	}
}

func TestHandleDiscoverySuccess(t *testing.T) {
	repo := newFakeRepo()
	sc := &fakeStatusCache{}
	svc := newSvc(repo, sc, &fakeTSDB{})
	err := svc.HandleDiscovery(context.Background(), model.DiscoveryMessage{NodeID: "n1", MAC: "aa", FWVersion: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.nodes["n1"]; !ok {
		t.Error("expected node upserted")
	}
	if sc.status["n1"] == "" {
		t.Error("expected status cached")
	}
}

func TestHandleStatusRegistersUnknown(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	sc := &fakeStatusCache{}
	svc := newSvc(repo, sc, &fakeTSDB{})
	if err := svc.HandleStatus(context.Background(), "n1", model.StatusMessage{Status: "online"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.nodes["n1"]; !ok {
		t.Error("expected node auto-registered")
	}
}

func TestHandleStatusUpdateKnown(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1", Status: "offline"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if err := svc.HandleStatus(context.Background(), "n1", model.StatusMessage{Status: "online"}); err != nil {
		t.Fatal(err)
	}
	if repo.nodes["n1"].Status != "online" {
		t.Error("expected status updated")
	}
}

func TestTouchNodeAndFlush(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	svc.TouchNode("n1")
	svc.flushTouch()
	if len(repo.nodes) == 0 {
		// node not in repo; TouchNode just records pending; flush calls repo.TouchNode
	}
}

func TestGetNodeTagsNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.GetNodeTags(context.Background(), "n1"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestGetNodeTagsSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	repo.nodeTags["n1"] = []model.NodeTag{{ID: "t1", NodeID: "n1"}}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	tags, err := svc.GetNodeTags(context.Background(), "n1")
	if err != nil || len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v err %v", tags, err)
	}
}

func TestSaveNodeTags(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	reqs := []model.NodeTagRequest{{SourceKey: "temp", TagName: "temperature"}}
	if err := svc.SaveNodeTags(context.Background(), "n1", reqs); err != nil {
		t.Fatal(err)
	}
	if len(repo.nodeTags["n1"]) != 1 {
		t.Error("expected tag upserted")
	}
}

func TestDeleteNodeTag(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if err := svc.DeleteNodeTag(context.Background(), "n1", "t1"); err != nil {
		t.Fatal(err)
	}
}

func TestGetActuatorTagsNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.GetActuatorTags(context.Background(), "n1"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestCreateActuatorTagMissingSourceKey(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if _, err := svc.CreateActuatorTag(context.Background(), "n1", model.NodeTagRequest{}); err == nil {
		t.Error("expected error for missing source_key")
	}
}

func TestCreateActuatorTagSuccess(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	tag, err := svc.CreateActuatorTag(context.Background(), "n1", model.NodeTagRequest{SourceKey: "pump"})
	if err != nil || tag.Kind != "actuator" {
		t.Fatalf("expected actuator tag, got %v err %v", tag, err)
	}
}

func TestDeleteActuatorTagNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	if err := svc.DeleteActuatorTag(context.Background(), "n1", "t1"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestIngestTelemetryWritesReading(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	repo.nodeTags["n1"] = []model.NodeTag{{ID: "t1", NodeID: "n1", SourceKey: "temp", TagName: "temperature", Enabled: true, DataType: "float"}}
	ts := &fakeTSDB{}
	svc := newSvc(repo, &fakeStatusCache{}, ts)
	payload := []byte(`{"temp":22.5}`)
	svc.IngestTelemetry(context.Background(), "n1", payload)
	if len(ts.written) != 1 || ts.written[0] != "temperature" {
		t.Fatalf("expected temperature written, got %v", ts.written)
	}
}

func TestIngestTelemetryNilTSDB(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, nil)
	// Should not panic when ts is nil.
	svc.IngestTelemetry(context.Background(), "n1", []byte(`{"temp":1}`))
}

func TestIngestTelemetryBadJSON(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	// Should not panic on invalid JSON.
	svc.IngestTelemetry(context.Background(), "n1", []byte("not json"))
}

func TestNodeExists(t *testing.T) {
	repo := newFakeRepo()
	repo.err = repository.ErrNotFound
	svc := newSvc(repo, &fakeStatusCache{}, &fakeTSDB{})
	exists, err := svc.nodeExists(context.Background(), "n1")
	if err != nil || exists {
		t.Fatalf("expected exists=false, got %v err %v", exists, err)
	}
}

func TestResolvePathAndToFloat(t *testing.T) {
	data := map[string]interface{}{"telemetry": map[string]interface{}{"temp": 22.5}}
	if v, ok := resolvePath(data, "telemetry.temp"); !ok || v != 22.5 {
		t.Errorf("resolvePath failed: %v %v", v, ok)
	}
	if f, ok := toFloat(22.5, "float"); !ok || f != 22.5 {
		t.Errorf("toFloat float failed: %v", f)
	}
	if f, ok := toFloat(true, "bool"); !ok || f != 1 {
		t.Errorf("toFloat bool failed: %v", f)
	}
	if f, ok := toFloat(5, "int"); !ok || f != 5 {
		t.Errorf("toFloat int failed: %v", f)
	}
}

func TestTelemetryBatcher(t *testing.T) {
	b := newTelemetryBatcher()
	b.add("n1", "m1", "temp", 10, 1)
	b.add("n1", "m1", "temp", 20, 2)
	b.add("n1", "m1", "temp", 5, 3)
	entries := b.flush()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Count != 3 || e.Min != 5 || e.Max != 20 || e.Sum != 35 || e.Last != 5 {
		t.Errorf("unexpected aggregate: %+v", e)
	}
	if len(b.flush()) != 0 {
		t.Error("expected empty flush after reset")
	}
}

func TestFlushAndPublishJS(t *testing.T) {
	repo := newFakeRepo()
	repo.nodes["n1"] = &model.Node{NodeID: "n1"}
	js := &fakeJS{}
	svc := New(repo, &fakeStatusCache{}, &fakeNATS{}, js, &fakeTSDB{})
	svc.batch.add("n1", "m1", "temp", 10, 1)
	svc.flushAndPublish(time.Minute)
	if js.count != 1 {
		t.Errorf("expected 1 JetStream publish, got %d", js.count)
	}
}

func TestFlushAndPublishNoData(t *testing.T) {
	repo := newFakeRepo()
	nats := &fakeNATS{}
	svc := New(repo, &fakeStatusCache{}, nats, nil, &fakeTSDB{})
	svc.flushAndPublish(time.Minute)
	if nats.count != 0 {
		t.Error("expected no publish when batch empty")
	}
}

func TestStartBatchPublisherDisabledNoNATS(t *testing.T) {
	repo := newFakeRepo()
	svc := New(repo, &fakeStatusCache{}, nil, nil, &fakeTSDB{})
	// Should return immediately when nats is nil (no hang).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.StartBatchPublisher(ctx, time.Minute)
}

func TestStartBatchPublisherFlushOnCancel(t *testing.T) {
	repo := newFakeRepo()
	nats := &fakeNATS{}
	svc := New(repo, &fakeStatusCache{}, nats, nil, &fakeTSDB{})
	svc.batch.add("n1", "m1", "temp", 10, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	svc.StartBatchPublisher(ctx, 10*time.Millisecond)
	if nats.count == 0 {
		t.Error("expected a publish on cancel flush")
	}
}
