package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/almuzky/iot/services/export/internal/model"
	"github.com/almuzky/iot/services/export/internal/service"
	"github.com/almuzky/iot/services/export/internal/tsdb"
	"github.com/go-chi/chi/v5"
)

// Handler serves the Export REST API.
type Handler struct {
	svc *service.Service
}

// New builds the Export handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// Health is a liveness probe for Kong upstream healthchecks.
func Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// maxFileRows caps the total exported rows so a single export cannot produce an
// unbounded / DoS-scale payload.
const maxFileRows = 5_000_000

// writeJSON encodes a payload as JSON with the given status code, wrapped in
// the standard response envelope (AGENTS.md §4.4): { success, data }.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": v})
}

func errorResponse(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   map[string]string{"code": code, "message": msg},
	})
}

func badRequest(w http.ResponseWriter, msg string) {
	errorResponse(w, http.StatusBadRequest, "BAD_REQUEST", msg)
}

// parseTime accepts RFC3339, YYYY-MM-DD date strings, or unix-seconds strings.
func parseTime(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t.UTC(), true
	}
	if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(sec, 0).UTC(), true
	}
	return time.Time{}, false
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// TelemetryHandler streams a paginated CSV export of raw telemetry.
//
//	Query: ?node_id&metric&from&to&format=csv&limit&cursor
//	The response is a CSV file (Content-Disposition attachment) so the dashboard
//	/ research tooling can download it directly.
func (h *Handler) TelemetryHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nodeIDs := splitCSV(q.Get("node_id"))
	metrics := splitCSV(q.Get("metric"))
	if len(nodeIDs) == 0 {
		badRequest(w, "node_id is required")
		return
	}
	if len(metrics) == 0 {
		metrics = []string{"*"}
	}

	from, to := service.DefaultWindow()
	if v := q.Get("to"); v != "" {
		if t, ok := parseTime(v); ok {
			if len(v) == 10 { // YYYY-MM-DD format: set to end of day
				to = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			} else {
				to = t
			}
		} else {
			badRequest(w, "invalid 'to' (use RFC3339, YYYY-MM-DD, or unix seconds)")
			return
		}
	}
	if v := q.Get("from"); v != "" {
		if t, ok := parseTime(v); ok {
			from = t
		} else {
			badRequest(w, "invalid 'from' (use RFC3339 or unix seconds)")
			return
		}
	}

	limit := 10000
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100000 {
		limit = 100000 // hard cap per page
	}

	cursor, hasCursor := tsdb.DecodeCursor(q.Get("cursor"))

	eq := model.ExportQuery{
		NodeIDs: nodeIDs,
		Metrics: metrics,
		From:    from,
		To:      to,
		Limit:   limit,
	}

	page, err := h.svc.QueryPage(r.Context(), eq, cursor, hasCursor)
	if err != nil {
		if err == service.ErrWindowTooLarge {
			badRequest(w, "requested time range exceeds the 366-day export limit")
			return
		}
		if errors.Is(err, tsdb.ErrInvalidParam) {
			badRequest(w, "invalid node_id or metric (allowed: a-z A-Z 0-9 _ . - :)")
			return
		}
		log.Printf("[handler] export query failed: %v", err)
		errorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "export failed")
		return
	}

	// Enforce an overall file-size limit by capping the number of rows streamed
	// in a single file response (cursor-based follow-up pages fetch the rest).
	if len(page.Rows) > maxFileRows {
		page.Rows = page.Rows[:maxFileRows]
		page.HasMore = true
		page.NextCursor = tsdb.EncodeCursor(model.Cursor{
			Time:   page.Rows[len(page.Rows)-1].Time,
			Node:   page.Rows[len(page.Rows)-1].NodeID,
			Metric: page.Rows[len(page.Rows)-1].Metric,
		})
	}

	// If format=json is explicitly requested (e.g. for UI preview), return JSON envelope
	if strings.ToLower(q.Get("format")) == "json" {
		writeJSON(w, http.StatusOK, map[string]any{
			"rows":        page.Rows,
			"total":       len(page.Rows),
			"has_more":    page.HasMore,
			"next_cursor": page.NextCursor,
		})
		return
	}

	// Expose the next cursor as a response header so clients can follow pages
	// directly from the file download without parsing the CSV body.
	if page.HasMore && page.NextCursor != "" {
		w.Header().Set("X-Export-Next-Cursor", page.NextCursor)
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=telemetry_%s_%s_%s_%s.csv",
			strings.Join(nodeIDs, "_"), strings.Join(metrics, "_"),
			from.Format("20060102"), to.Format("20060102")))
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)

	// Pivot telemetry rows into wide format (columns per metric) for clean Excel tabular view
	type timeGroupKey struct {
		time     time.Time
		nodeID   string
		moduleID string
	}

	var metricOrder []string
	metricSet := make(map[string]bool)
	groupKeys := []timeGroupKey{}
	groupMap := make(map[timeGroupKey]map[string]float64)

	for _, row := range page.Rows {
		if !metricSet[row.Metric] {
			metricSet[row.Metric] = true
			metricOrder = append(metricOrder, row.Metric)
		}

		modID := ""
		if row.ModuleID != nil {
			modID = *row.ModuleID
		}
		k := timeGroupKey{time: row.Time.UTC(), nodeID: row.NodeID, moduleID: modID}
		if _, exists := groupMap[k]; !exists {
			groupMap[k] = make(map[string]float64)
			groupKeys = append(groupKeys, k)
		}
		groupMap[k][row.Metric] = row.Value
	}

	header := append([]string{"time", "node_id", "module_id"}, metricOrder...)
	if err := cw.Write(header); err != nil {
		log.Printf("[handler] csv header write failed: %v", err)
		return
	}

	for _, k := range groupKeys {
		metricVals := groupMap[k]
		record := make([]string, 0, len(header))
		record = append(record, k.time.Format(time.RFC3339), k.nodeID, k.moduleID)
		for _, m := range metricOrder {
			if val, ok := metricVals[m]; ok {
				record = append(record, strconv.FormatFloat(val, 'f', -1, 64))
			} else {
				record = append(record, "")
			}
		}
		if err := cw.Write(record); err != nil {
			log.Printf("[handler] csv row write failed: %v", err)
			return
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		log.Printf("[handler] csv flush failed: %v", err)
	}
}

// MetadataHandler returns JSON metadata for an export window WITHOUT streaming a
// file — useful for the dashboard to preview counts and the next cursor. This
// exercises the same paginated, cursor-stable query path as the file export.
func (h *Handler) MetadataHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nodeIDs := splitCSV(q.Get("node_id"))
	metrics := splitCSV(q.Get("metric"))
	if len(nodeIDs) == 0 {
		badRequest(w, "node_id is required")
		return
	}
	if len(metrics) == 0 {
		metrics = []string{"*"}
	}

	from, to := service.DefaultWindow()
	if v := q.Get("to"); v != "" {
		if t, ok := parseTime(v); ok {
			if len(v) == 10 { // YYYY-MM-DD format: set to end of day
				to = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			} else {
				to = t
			}
		} else {
			badRequest(w, "invalid 'to' (use RFC3339, YYYY-MM-DD, or unix seconds)")
			return
		}
	}
	if v := q.Get("from"); v != "" {
		if t, ok := parseTime(v); ok {
			from = t
		} else {
			badRequest(w, "invalid 'from' (use RFC3339 or unix seconds)")
			return
		}
	}

	eq := model.ExportQuery{NodeIDs: nodeIDs, Metrics: metrics, From: from, To: to, Limit: 1}
	total, err := h.svc.Count(r.Context(), eq)
	if err != nil {
		if err == service.ErrWindowTooLarge {
			badRequest(w, "requested time range exceeds the 366-day export limit")
			return
		}
		log.Printf("[handler] count failed: %v", err)
		errorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"node_ids": nodeIDs,
		"metrics":  metrics,
		"from":     from.UTC().Format(time.RFC3339),
		"to":       to.UTC().Format(time.RFC3339),
		"total":    total,
	})
}

// NodesHandler lists nodes with telemetry and available metrics.
func (h *Handler) NodesHandler(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.svc.ListNodes(r.Context())
	if err != nil {
		log.Printf("[handler] list nodes failed: %v", err)
		errorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": nodes})
}

// OpenAPIHandler serves the OpenAPI 3 specification for the Export Service.
func (h *Handler) OpenAPIHandler(w http.ResponseWriter, r *http.Request) {
	spec := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "Export Service API",
			"version":     "1.0.0",
			"description": "Historical telemetry export (CSV) with cursor-based pagination and RBAC.",
		},
		"servers": []map[string]interface{}{
			{"url": "http://localhost:8000", "description": "via Kong gateway"},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
		},
		"security": []map[string]interface{}{{"bearerAuth": []string{}}},
		"paths": map[string]interface{}{
			"/export/v1/telemetry": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Stream a paginated CSV export of raw telemetry",
					"parameters": []map[string]interface{}{
						{"name": "node_id", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}, "description": "Comma-separated node IDs"},
						{"name": "metric", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}, "description": "Comma-separated metric names"},
						{"name": "from", "in": "query", "schema": map[string]interface{}{"type": "string"}, "description": "RFC3339 start (default 24h ago)"},
						{"name": "to", "in": "query", "schema": map[string]interface{}{"type": "string"}, "description": "RFC3339 end (default now)"},
						{"name": "limit", "in": "query", "schema": map[string]interface{}{"type": "integer"}, "description": "Rows per page (max 100000)"},
						{"name": "cursor", "in": "query", "schema": map[string]interface{}{"type": "string"}, "description": "Opaque keyset cursor for the next page"},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "CSV file attachment"},
						"400": map[string]interface{}{"description": "Bad request (invalid params / window too large)"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"403": map[string]interface{}{"description": "Forbidden (insufficient role)"},
					},
				},
			},
			"/export/v1/nodes": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "List nodes with telemetry and their available metrics",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Node list"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"403": map[string]interface{}{"description": "Forbidden"},
					},
				},
			},
			"/export/v1/openapi": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "This OpenAPI specification",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "OpenAPI JSON"},
					},
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(spec)
}

// Routes registers the Export HTTP routes. Auth + RBAC (admin/operator) are
// applied to every /export/v1 route so exports are never reachable
// unauthenticated; /health (registered in main.go) stays public for Kong.
func (h *Handler) Routes(r chi.Router, authMw, rbacMw func(http.Handler) http.Handler) {
	r.With(authMw, rbacMw).Get("/export/v1/telemetry", h.TelemetryHandler)
	r.With(authMw, rbacMw).Get("/export/telemetry", h.TelemetryHandler)

	r.With(authMw, rbacMw).Get("/export/v1/nodes", h.NodesHandler)
	r.With(authMw, rbacMw).Get("/export/nodes", h.NodesHandler)

	r.With(authMw, rbacMw).Get("/export/v1/meta", h.MetadataHandler)
	r.With(authMw, rbacMw).Get("/export/meta", h.MetadataHandler)

	r.With(authMw, rbacMw).Get("/export/v1/openapi", h.OpenAPIHandler)
	r.With(authMw, rbacMw).Get("/export/openapi", h.OpenAPIHandler)
}
