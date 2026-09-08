package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/almuzky/iot/services/module/internal/model"
	"github.com/almuzky/iot/services/module/internal/repository"
	"github.com/almuzky/iot/services/module/internal/service"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *service.ModuleService
}

func New(svc *service.ModuleService) *Handler {
	return &Handler{svc: svc}
}

// ─── Health ──────────────────────────────────────────────────────────────────

func Health(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Modules ─────────────────────────────────────────────────────────────────

func (h *Handler) CreateModule(w http.ResponseWriter, r *http.Request) {
	var req model.CreateModuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validName(req.Name) || !validName(req.Description) {
		respondError(w, http.StatusBadRequest, "name/description contains invalid characters")
		return
	}
	m, err := h.svc.CreateModule(r.Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrNameRequired) {
			respondError(w, http.StatusBadRequest, "name is required")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to create module")
		return
	}
	respond(w, http.StatusCreated, m)
}

func (h *Handler) ListModules(w http.ResponseWriter, r *http.Request) {
	mods, err := h.svc.ListModules(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list modules")
		return
	}
	respond(w, http.StatusOK, map[string]any{"modules": mods, "count": len(mods)})
}

func (h *Handler) GetModule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, err := h.svc.GetModule(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrModuleNotFound) {
			respondError(w, http.StatusNotFound, "module not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get module")
		return
	}
	respond(w, http.StatusOK, m)
}

func (h *Handler) UpdateModule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req model.UpdateModuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil && !validName(*req.Name) {
		respondError(w, http.StatusBadRequest, "name contains invalid characters")
		return
	}
	if req.Description != nil && !validName(*req.Description) {
		respondError(w, http.StatusBadRequest, "description contains invalid characters")
		return
	}
	m, err := h.svc.UpdateModule(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, service.ErrModuleNotFound) {
			respondError(w, http.StatusNotFound, "module not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to update module")
		return
	}
	respond(w, http.StatusOK, m)
}

func (h *Handler) DeleteModule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteModule(r.Context(), id); err != nil {
		if errors.Is(err, service.ErrModuleNotFound) {
			respondError(w, http.StatusNotFound, "module not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete module")
		return
	}
	respond(w, http.StatusOK, map[string]string{"message": "module deleted; its nodes were unpaired"})
}

// ─── Nodes ───────────────────────────────────────────────────────────────────

func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var paired *bool
	if v := q.Get("paired"); v != "" {
		b := v == "true" || v == "1"
		paired = &b
	}
	nodes, err := h.svc.ListNodes(r.Context(), paired, q.Get("module_id"), q.Get("status"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}
	respond(w, http.StatusOK, map[string]any{"nodes": nodes, "count": len(nodes)})
}

// ListDiscovered returns unpaired devices — the onboarding candidates.
func (h *Handler) ListDiscovered(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.svc.ListDiscovered(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list discovered nodes")
		return
	}
	respond(w, http.StatusOK, map[string]any{"nodes": nodes, "count": len(nodes)})
}

func (h *Handler) GetNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	n, err := h.svc.GetNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			respondError(w, http.StatusNotFound, "node not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get node")
		return
	}
	respond(w, http.StatusOK, n)
}

// GetLiveTelemetry returns latest live telemetry payload directly from Redis.
func (h *Handler) GetLiveTelemetry(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	raw, err := h.svc.GetLiveTelemetry(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			respondError(w, http.StatusNotFound, "live telemetry not found or expired")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get live telemetry")
		return
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return
	}
	respond(w, http.StatusOK, data)
}

func (h *Handler) PairNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	var req model.PairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validName(req.Name) {
		respondError(w, http.StatusBadRequest, "name contains invalid characters")
		return
	}
	n, err := h.svc.Pair(r.Context(), nodeID, req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNodeNotFound):
			respondError(w, http.StatusNotFound, "node not found")
		case errors.Is(err, service.ErrModuleNotFound):
			respondError(w, http.StatusBadRequest, "module_id is required and must reference an existing module")
		default:
			respondError(w, http.StatusInternalServerError, "failed to pair node")
		}
		return
	}
	respond(w, http.StatusOK, n)
}

func (h *Handler) UnpairNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	n, err := h.svc.Unpair(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			respondError(w, http.StatusNotFound, "node not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to unpair node")
		return
	}
	respond(w, http.StatusOK, n)
}

func (h *Handler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	if err := h.svc.DeleteNode(r.Context(), nodeID); err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			respondError(w, http.StatusNotFound, "node not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete node")
		return
	}
	respond(w, http.StatusOK, map[string]string{"message": "node deleted"})
}

// ─── Node tag mapping (modular telemetry acquisition) ────────────────────────

// GetNodeTags returns the telemetry tag-mapping config for a node.
func (h *Handler) GetNodeTags(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	tags, err := h.svc.GetNodeTags(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			respondError(w, http.StatusNotFound, "node not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to load tags")
		return
	}
	if tags == nil {
		tags = []model.NodeTag{}
	}
	respond(w, http.StatusOK, map[string]any{"node_id": nodeID, "tags": tags})
}

// SaveNodeTag upserts a single sensor tag for a node.
func (h *Handler) SaveNodeTag(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	var req model.NodeTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.SaveNodeTag(r.Context(), nodeID, req); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save tag")
		return
	}
	tags, _ := h.svc.GetNodeTags(r.Context(), nodeID)
	respond(w, http.StatusOK, map[string]any{"node_id": nodeID, "tags": tags})
}

// DeleteNodeTag removes a single sensor tag mapping for a node.
func (h *Handler) DeleteNodeTag(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteNodeTag(r.Context(), nodeID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "tag not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete tag")
		return
	}
	respond(w, http.StatusOK, map[string]any{"node_id": nodeID, "deleted": id})
}

// SaveNodeTags replaces the full tag-mapping set for a node (attach/detach).
func (h *Handler) SaveNodeTags(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	var reqs []model.NodeTagRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.SaveNodeTags(r.Context(), nodeID, reqs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save tags")
		return
	}
	tags, _ := h.svc.GetNodeTags(r.Context(), nodeID)
	respond(w, http.StatusOK, map[string]any{"node_id": nodeID, "tags": tags})
}

// GetActuatorTags returns actuator (control output) tags for a node.
func (h *Handler) GetActuatorTags(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	tags, err := h.svc.GetActuatorTags(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			respondError(w, http.StatusNotFound, "node not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to load actuator tags")
		return
	}
	if tags == nil {
		tags = []model.NodeTag{}
	}
	respond(w, http.StatusOK, map[string]any{"node_id": nodeID, "tags": tags})
}

// CreateActuatorTag attaches a controllable output to a friendly tag.
func (h *Handler) CreateActuatorTag(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	var req model.NodeTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tag, err := h.svc.CreateActuatorTag(r.Context(), nodeID, req)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			respondError(w, http.StatusNotFound, "node not found")
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, http.StatusCreated, tag)
}

// DeleteActuatorTag removes a single actuator tag.
func (h *Handler) DeleteActuatorTag(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "node_id")
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteActuatorTag(r.Context(), nodeID, id); err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			respondError(w, http.StatusNotFound, "node not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete actuator tag")
		return
	}
	respond(w, http.StatusOK, map[string]string{"message": "actuator tag deleted"})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Standard API response wrapper (AGENTS.md §4.4): { success, data }.
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": v})
}

// respondError emits the standard error envelope (AGENTS.md §4.4):
// { success:false, error:{ code, message } }.
func respondError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   map[string]string{"code": errorCode(status), "message": msg},
	})
}

// errorCode maps an HTTP status to a stable machine-readable error code.
func errorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	default:
		return "INTERNAL_ERROR"
	}
}

// validName rejects control characters and angle brackets to prevent stored
// XSS / injection via free-text fields. JSON output is HTML-escaped by the
// encoder as a second layer of defense.
func validName(s string) bool {
	if strings.ContainsAny(s, "<>") {
		return false
	}
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}
