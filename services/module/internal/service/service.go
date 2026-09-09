package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/almuzky/iot/services/module/internal/model"
	"github.com/almuzky/iot/services/module/internal/outbox"
	"github.com/almuzky/iot/services/module/internal/repository"
)

var (
	ErrModuleNotFound = errors.New("module not found")
	ErrNodeNotFound   = errors.New("node not found")
	ErrNameRequired   = errors.New("name is required")
)

// NATSPublisher is a minimal interface for publishing audit/events.
type NATSPublisher interface {
	Publish(subject string, data []byte) error
}

// JetStreamPublisher publishes to a JetStream stream with durability. Optional;
// when nil the batch publisher falls back to core NATS (best-effort, no replay).
type JetStreamPublisher interface {
	Publish(subject string, data []byte) error
}

// statusTTL: nodes not seen within this window are considered stale in Redis.
const statusTTL = 90 * time.Second

// latestTTL: how long the most-recent telemetry payload is kept in Redis.
const latestTTL = 5 * time.Minute

type ModuleService struct {
	repo  Repository
	cache StatusCache
	nats  NATSPublisher
	js    JetStreamPublisher
	ts    TSDB
	batch *telemetryBatcher

	// nodeMetaCache caches the per-node tag mapping + module id so the hot-path
	// telemetry ingest does not hit MariaDB for every reading. Entries carry a
	// short TTL and are invalidated when the mapping changes (pair/unpair/tags).
	metaMu    sync.Mutex
	metaCache map[string]*nodeMeta

	// touchPending batches TouchNode (last_seen refresh) DB writes: a busy node
	// no longer triggers a MariaDB UPDATE on every MQTT message. Flushed by
	// StartTouchFlusher.
	touchMu      sync.Mutex
	touchPending map[string]struct{}
}

func New(repo Repository, c StatusCache, nats NATSPublisher, js JetStreamPublisher, ts TSDB) *ModuleService {
	return &ModuleService{
		repo:         repo,
		cache:        c,
		nats:         nats,
		js:           js,
		ts:           ts,
		batch:        newTelemetryBatcher(),
		metaCache:    make(map[string]*nodeMeta),
		touchPending: make(map[string]struct{}),
	}
}

// nodeMeta is a cached tag-mapping + module id for a single node.
type nodeMeta struct {
	tags     []model.NodeTag
	moduleID string
	expires  time.Time
}

// nodeMetaTTL bounds how stale the cached tag mapping may be. Mapping changes
// are rare (admin edits / pairing), so a short TTL plus explicit invalidation
// keeps the cache both fast and correct.
const nodeMetaTTL = 2 * time.Minute

// getNodeMeta returns the sensor tag mapping and module id for a node, using
// the in-memory cache and falling back to MariaDB on a miss. This converts the
// previous per-reading N+1 (ListNodeTags + GetModuleIDByNode) into ~0 DB reads
// between cache refreshes.
func (s *ModuleService) getNodeMeta(ctx context.Context, nodeID string) ([]model.NodeTag, string, error) {
	s.metaMu.Lock()
	if e, ok := s.metaCache[nodeID]; ok && time.Now().Before(e.expires) {
		tags, moduleID := e.tags, e.moduleID
		s.metaMu.Unlock()
		return tags, moduleID, nil
	}
	s.metaMu.Unlock()

	tags, err := s.repo.ListNodeTags(ctx, nodeID)
	if err != nil {
		return nil, "", err
	}
	moduleIDStr := ""
	if mid, err2 := s.repo.GetModuleIDByNode(ctx, nodeID); err2 == nil && mid != nil {
		moduleIDStr = *mid
	}
	s.metaMu.Lock()
	s.metaCache[nodeID] = &nodeMeta{tags: tags, moduleID: moduleIDStr, expires: time.Now().Add(nodeMetaTTL)}
	s.metaMu.Unlock()
	return tags, moduleIDStr, nil
}

// invalidateMeta drops any cached mapping for a node (e.g. after tag/pair edits).
func (s *ModuleService) invalidateMeta(nodeID string) {
	if nodeID == "" {
		return
	}
	s.metaMu.Lock()
	delete(s.metaCache, nodeID)
	s.metaMu.Unlock()
}

// ─── Modules ─────────────────────────────────────────────────────────────────

func (s *ModuleService) CreateModule(ctx context.Context, req model.CreateModuleRequest) (*model.Module, error) {
	if req.Name == "" {
		return nil, ErrNameRequired
	}
	m := &model.Module{Name: req.Name, Description: req.Description, Config: req.Config}
	if err := s.repo.CreateModule(ctx, m); err != nil {
		return nil, err
	}
	s.cache.InvalidateModules(ctx)
	s.publishAudit("module.created", map[string]string{"module_id": m.ID, "name": m.Name})
	return m, nil
}

func (s *ModuleService) ListModules(ctx context.Context) ([]model.Module, error) {
	if cached, ok := s.cache.GetCachedModules(ctx); ok {
		return cached, nil
	}
	modules, err := s.repo.ListModules(ctx)
	if err == nil {
		s.cache.SetCachedModules(ctx, modules, 30*time.Second)
	}
	return modules, err
}

func (s *ModuleService) GetModule(ctx context.Context, id string) (*model.Module, error) {
	m, err := s.repo.GetModule(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrModuleNotFound
	}
	return m, err
}

func (s *ModuleService) UpdateModule(ctx context.Context, id string, req model.UpdateModuleRequest) (*model.Module, error) {
	m, err := s.repo.UpdateModule(ctx, id, req)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrModuleNotFound
	}
	if err != nil {
		return nil, err
	}
	s.cache.InvalidateModules(ctx)
	s.publishAudit("module.updated", map[string]string{"module_id": id})
	return m, nil
}

func (s *ModuleService) DeleteModule(ctx context.Context, id string) error {
	err := s.repo.DeleteModule(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrModuleNotFound
	}
	if err == nil {
		s.cache.InvalidateModules(ctx)
		s.publishAudit("module.deleted", map[string]string{"module_id": id})
	}
	return err
}

// ─── Nodes ───────────────────────────────────────────────────────────────────

func (s *ModuleService) ListNodes(ctx context.Context, paired *bool, moduleID, status string) ([]model.Node, error) {
	nodes, err := s.repo.ListNodes(ctx, paired, moduleID, status)
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		if live := s.cache.GetStatus(ctx, nodes[i].NodeID); live != "" {
			nodes[i].Status = live
		}
	}
	return nodes, nil
}

// ListDiscovered returns nodes that are not yet paired (onboarding candidates).
func (s *ModuleService) ListDiscovered(ctx context.Context) ([]model.Node, error) {
	no := false
	nodes, err := s.repo.ListNodes(ctx, &no, "", "")
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		if live := s.cache.GetStatus(ctx, nodes[i].NodeID); live != "" {
			nodes[i].Status = live
		}
	}
	return nodes, nil
}

func (s *ModuleService) GetNode(ctx context.Context, nodeID string) (*model.Node, error) {
	n, err := s.repo.GetNodeByNodeID(ctx, nodeID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNodeNotFound
	}
	if err != nil {
		return nil, err
	}
	if live := s.cache.GetStatus(ctx, n.NodeID); live != "" {
		n.Status = live
	}
	return n, nil
}

// GetLiveTelemetry queries the latest telemetry reading payload directly from Redis.
func (s *ModuleService) GetLiveTelemetry(ctx context.Context, nodeID string) ([]byte, error) {
	raw := s.cache.GetLatest(ctx, nodeID)
	if raw == nil {
		return nil, ErrNodeNotFound
	}
	return raw, nil
}

func (s *ModuleService) Pair(ctx context.Context, nodeID string, req model.PairRequest) (*model.Node, error) {
	if req.ModuleID == "" {
		return nil, ErrModuleNotFound
	}
	exists, err := s.repo.ModuleExists(ctx, req.ModuleID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrModuleNotFound
	}
	n, err := s.repo.Pair(ctx, nodeID, req.ModuleID, req.Name)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNodeNotFound
	}
	if err != nil {
		return nil, err
	}
	s.invalidateMeta(nodeID)
	s.publishAudit("node.paired", map[string]string{"node_id": nodeID, "module_id": req.ModuleID})
	return n, nil
}

func (s *ModuleService) Unpair(ctx context.Context, nodeID string) (*model.Node, error) {
	n, err := s.repo.Unpair(ctx, nodeID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNodeNotFound
	}
	if err != nil {
		return nil, err
	}
	s.invalidateMeta(nodeID)
	s.publishAudit("node.unpaired", map[string]string{"node_id": nodeID})
	return n, nil
}

func (s *ModuleService) DeleteNode(ctx context.Context, nodeID string) error {
	err := s.repo.DeleteNode(ctx, nodeID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNodeNotFound
	}
	if err == nil {
		s.invalidateMeta(nodeID)
		s.publishAudit("node.deleted", map[string]string{"node_id": nodeID})
	}
	return err
}

// ─── MQTT ingestion (called by the subscriber) ───────────────────────────────

// HandleDiscovery upserts a node seen via {prefix}/discovery.
func (s *ModuleService) HandleDiscovery(ctx context.Context, msg model.DiscoveryMessage) error {
	if msg.NodeID == "" {
		return errors.New("discovery message missing node_id")
	}
	status := msg.Status
	if status == "" {
		status = model.StatusUnknown
	}
	n := &model.Node{
		NodeID:    msg.NodeID,
		MAC:       msg.MAC,
		IP:        msg.IP,
		FWVersion: msg.FWVersion,
		Status:    status,
	}
	created, err := s.repo.UpsertDiscovered(ctx, n)
	if err != nil {
		return err
	}
	s.cache.SetStatus(ctx, msg.NodeID, status, statusTTL)
	if created {
		s.publishAudit("node.discovered", map[string]string{
			"node_id": msg.NodeID, "mac": msg.MAC, "fw_version": msg.FWVersion,
		})
	}
	return nil
}

// HandleStatus updates connectivity from {prefix}/status/{node_id} (incl. LWT).
func (s *ModuleService) HandleStatus(ctx context.Context, nodeID string, msg model.StatusMessage) error {
	if nodeID == "" {
		return errors.New("status message missing node_id")
	}
	status := msg.Status
	if status == "" {
		status = model.StatusUnknown
	}
	// If node unknown yet, register it from the status payload so nothing is missed.
	if _, err := s.repo.GetNodeByNodeID(ctx, nodeID); errors.Is(err, repository.ErrNotFound) {
		_, _ = s.repo.UpsertDiscovered(ctx, &model.Node{
			NodeID: nodeID, MAC: msg.MAC, IP: msg.IP, FWVersion: msg.FW, Status: status,
		})
	} else if err != nil {
		return err
	} else {
		if err := s.repo.UpdateStatus(ctx, nodeID, status, msg.IP); err != nil {
			return err
		}
	}
	s.cache.SetStatus(ctx, nodeID, status, statusTTL)
	return nil
}

// TouchNode records that a node is alive because an MQTT payload arrived.
// Instead of writing to MariaDB immediately, it marks the node as "pending a
// touch"; StartTouchFlusher persists these in batches, so a node sending
// telemetry every few seconds only triggers one UPDATE every flush interval.
func (s *ModuleService) TouchNode(nodeID string) {
	if nodeID == "" {
		return
	}
	s.touchMu.Lock()
	s.touchPending[nodeID] = struct{}{}
	s.touchMu.Unlock()
}

// StartTouchFlusher periodically persists pending TouchNode writes to MariaDB.
// On context cancellation it performs one final flush so no "last seen" update
// is lost on shutdown.
func (s *ModuleService) StartTouchFlusher(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.flushTouch()
			return
		case <-ticker.C:
			s.flushTouch()
		}
	}
}

// flushTouch writes a single UPDATE for each node touched since the last flush.
func (s *ModuleService) flushTouch() {
	s.touchMu.Lock()
	if len(s.touchPending) == 0 {
		s.touchMu.Unlock()
		return
	}
	nodes := make([]string, 0, len(s.touchPending))
	for id := range s.touchPending {
		nodes = append(nodes, id)
	}
	s.touchPending = make(map[string]struct{})
	s.touchMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, id := range nodes {
		if err := s.repo.TouchNode(ctx, id); err != nil {
			log.Printf("[svc] batched touch node %s failed: %v", id, err)
		}
	}
}

// ─── Telemetry tag mapping (modular acquisition) ─────────────────────────────

// GetNodeTags returns the tag-mapping configuration for a node.
func (s *ModuleService) GetNodeTags(ctx context.Context, nodeID string) ([]model.NodeTag, error) {
	exists, err := s.nodeExists(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNodeNotFound
	}
	return s.repo.ListNodeTags(ctx, nodeID)
}

// SaveNodeTags replaces the full tag-mapping set for a node (idempotent attach).
// Each request row attaches an MQTT source key to a DB tag name. Rows removed by
// the user are actually deleted (rather than merely left behind), so a reduced
// mapping persists after save. Only sensor-kind tags are affected; actuator tags
// are managed separately and left untouched.
func (s *ModuleService) SaveNodeTags(ctx context.Context, nodeID string, reqs []model.NodeTagRequest) error {
	tags := make([]*model.NodeTag, 0, len(reqs))
	for _, r := range reqs {
		tagName := r.TagName
		if tagName == "" {
			tagName = r.SourceKey // default: DB metric mirrors the telemetry key
		}
		tags = append(tags, &model.NodeTag{
			ID:          r.ID,
			NodeID:      nodeID,
			SourceKey:   r.SourceKey,
			TagName:     tagName,
			DisplayName: r.DisplayName,
			Unit:        r.Unit,
			DataType:    r.DataType,
			Enabled:     r.Enabled,
		})
	}

	// Delete sensor tags the user removed from the mapping: anything whose id is
	// not present in the incoming request.
	keepIDs := make([]string, 0, len(tags))
	for _, t := range tags {
		if t.ID != "" {
			keepIDs = append(keepIDs, t.ID)
		}
	}
	if len(keepIDs) == 0 {
		existing, err := s.repo.ListNodeTags(ctx, nodeID)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			return errors.New("refusing to clear all sensor tags: send at least one existing tag id to preserve the mapping, or delete tags individually first")
		}
	}
	if err := s.repo.DeleteSensorTagsExcept(ctx, nodeID, keepIDs); err != nil {
		return err
	}

	for _, t := range tags {
		if err := s.repo.UpsertNodeTag(ctx, t); err != nil {
			return err
		}
	}
	s.invalidateMeta(nodeID)
	return nil
}

// SaveNodeTag upserts a single sensor tag for a node.
func (s *ModuleService) SaveNodeTag(ctx context.Context, nodeID string, req model.NodeTagRequest) error {
	tagName := req.TagName
	if tagName == "" {
		tagName = req.SourceKey
	}
	t := &model.NodeTag{
		ID:          req.ID,
		NodeID:      nodeID,
		SourceKey:   req.SourceKey,
		TagName:     tagName,
		DisplayName: req.DisplayName,
		Unit:        req.Unit,
		DataType:    req.DataType,
		Enabled:     req.Enabled,
		Kind:        "sensor",
	}
	return s.repo.UpsertNodeTag(ctx, t)
}

// DeleteNodeTag removes a single tag-mapping row.
func (s *ModuleService) DeleteNodeTag(ctx context.Context, nodeID, id string) error {
	s.invalidateMeta(nodeID)
	return s.repo.DeleteNodeTag(ctx, nodeID, id)
}

// nodeExists is a guard used by tag/actuator endpoints so they return a proper
// 404 (not an empty 200) when the referenced node does not exist.
func (s *ModuleService) nodeExists(ctx context.Context, nodeID string) (bool, error) {
	_, err := s.repo.GetNodeByNodeID(ctx, nodeID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	return false, err
}

// ─── Actuator tag mapping (control outputs, separate from sensor telemetry) ──

// GetActuatorTags returns the actuator (kind="actuator") tags for a node.
func (s *ModuleService) GetActuatorTags(ctx context.Context, nodeID string) ([]model.NodeTag, error) {
	exists, err := s.nodeExists(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNodeNotFound
	}
	return s.repo.ListActuatorTags(ctx, nodeID)
}

// CreateActuatorTag attaches a single controllable output to a friendly tag.
// sourceKey is the firmware output name (e.g. "pump"); tagName is the DB tag.
func (s *ModuleService) CreateActuatorTag(ctx context.Context, nodeID string, req model.NodeTagRequest) (*model.NodeTag, error) {
	exists, err := s.nodeExists(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNodeNotFound
	}
	if req.SourceKey == "" {
		return nil, errors.New("source_key (firmware output name) is required")
	}
	tagName := req.TagName
	if tagName == "" {
		tagName = req.SourceKey
	}
	dataType := req.DataType
	if dataType == "" {
		dataType = "int"
	}
	t := &model.NodeTag{
		NodeID:      nodeID,
		Kind:        "actuator",
		SourceKey:   req.SourceKey,
		TagName:     tagName,
		DisplayName: req.DisplayName,
		Unit:        req.Unit,
		DataType:    dataType,
		Enabled:     true,
	}
	if err := s.repo.UpsertNodeTag(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteActuatorTag removes a single actuator tag.
func (s *ModuleService) DeleteActuatorTag(ctx context.Context, nodeID, id string) error {
	exists, err := s.nodeExists(ctx, nodeID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNodeNotFound
	}
	return s.repo.DeleteNodeTag(ctx, nodeID, id)
}

// IngestTelemetry writes a telemetry payload to TimescaleDB using the node's
// tag mapping. Only enabled, mapped numeric keys are persisted as metrics; the
// full raw payload is always stored for audit/flexibility.
func (s *ModuleService) IngestTelemetry(ctx context.Context, nodeID string, payload []byte) {
	// Cache the latest raw payload regardless of mapping.
	s.cache.SetLatest(ctx, nodeID, payload, latestTTL)

	if s.ts == nil {
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return
	}

	tags, moduleIDStr, err := s.getNodeMeta(ctx, nodeID)
	if err != nil {
		log.Printf("[svc] list node tags failed node=%s: %v", nodeID, err)
		return
	}
	var moduleIDPtr *string
	if moduleIDStr != "" {
		moduleIDPtr = &moduleIDStr
	}

	for _, t := range tags {
		if !t.Enabled {
			continue
		}
		// source_key supports dot-paths into nested telemetry, e.g.
		// "telemetry.modbus.cwt1.temp" or "network.wifi_rssi".
		val, ok := resolvePath(data, t.SourceKey)
		if !ok {
			continue
		}
		f, ok := toFloat(val, t.DataType)
		if !ok {
			continue // non-numeric (e.g. string) is kept only in raw
		}
		if err := s.ts.WriteReading(ctx, nodeID, moduleIDPtr, t.TagName, f, payload); err == nil {
			s.publishTelemetry(nodeID, t.TagName, f)
			s.batch.add(nodeID, moduleIDStr, t.TagName, f, time.Now().UnixMilli())
		}
	}
}

// resolvePath walks a dot-separated path (e.g. "a.b.c") into a decoded JSON map.
func resolvePath(data map[string]interface{}, path string) (interface{}, bool) {
	cur := interface{}(data)
	for _, p := range strings.Split(path, ".") {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		v, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// toFloat coerces a decoded JSON value to a float64 according to the configured type.
func toFloat(v interface{}, dt string) (float64, bool) {
	switch dt {
	case "int":
		switch n := v.(type) {
		case float64:
			return float64(int64(n)), true
		case int:
			return float64(n), true
		}
		return 0, false
	case "bool":
		switch n := v.(type) {
		case bool:
			if n {
				return 1, true
			}
			return 0, true
		case float64:
			if n == 0 {
				return 0, true
			}
			return 1, true
		case float32:
			if n == 0 {
				return 0, true
			}
			return 1, true
		case int:
			if n == 0 {
				return 0, true
			}
			return 1, true
		case string:
			switch strings.ToLower(strings.TrimSpace(n)) {
			case "1", "true", "on", "yes":
				return 1, true
			case "0", "false", "off", "no":
				return 0, true
			}
		}
		return 0, false
	default: // float / numeric
		switch n := v.(type) {
		case float64:
			return n, true
		case float32:
			return float64(n), true
		case int:
			return float64(n), true
		}
		return 0, false
	}
}

// enqueueOutbox writes an outbox row (subject + payload + msg_id) to MariaDB.
// The relay worker publishes it to NATS and marks it sent (ADR-007). Each call
// runs in its own committed transaction so no event is lost even if NATS is
// unavailable — the row persists and the relay retries.
func (s *ModuleService) enqueueOutbox(subject, payload string) {
	msgID := outbox.NewMsgID()
	if err := s.repo.Transact(context.Background(), func(tx *sql.Tx) error {
		return s.repo.InsertOutboxTx(context.Background(), tx, subject, payload, msgID)
	}); err != nil {
		log.Printf("[outbox] enqueue failed subject=%s: %v", subject, err)
	}
}

// publishTelemetry emits a NATS event per reading (downstream: alert/analytics).
// Written to the outbox; the relay publishes it to telemetry.ingest.
func (s *ModuleService) publishTelemetry(nodeID, metric string, value float64) {
	envelope := fmt.Sprintf(`{"node_id":%q,"metric":%q,"value":%v,"ts":%d}`,
		nodeID, metric, value, time.Now().UnixMilli())
	s.enqueueOutbox("telemetry.ingest", envelope)
}

// ─── Audit helper ────────────────────────────────────────────────────────────

// PublishLive forwards a raw MQTT payload to NATS so the WS-Gateway can stream
// it to dashboard clients subscribed to that node. Subject: mqtt.{node_id}.
func (s *ModuleService) PublishLive(nodeID, topic string, payload []byte) {
	envelope := fmt.Sprintf(`{"topic":%q,"payload":%s,"ts":%d}`, topic, string(payload), time.Now().UnixMilli())
	s.enqueueOutbox("mqtt."+nodeID, envelope)
}

func (s *ModuleService) publishAudit(event string, fields map[string]string) {
	payload := fmt.Sprintf(`{"event":%q,"service":"module","data":%s}`, event, mapToJSON(fields))
	s.enqueueOutbox("audit.log", payload)
}

func mapToJSON(m map[string]string) string {
	out := "{"
	first := true
	for k, v := range m {
		if !first {
			out += ","
		}
		out += fmt.Sprintf(`%q:%q`, k, v)
		first = false
	}
	return out + "}"
}
