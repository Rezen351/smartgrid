package model

import "time"

// TelemetryRow is a single reading from the module_ts telemetry hypertable.
type TelemetryRow struct {
	Time     time.Time `json:"time"`
	NodeID   string    `json:"node_id"`
	ModuleID *string   `json:"module_id"`
	Metric   string    `json:"metric"`
	Value    float64   `json:"value"`
}

// ExportQuery describes the filters accepted by the export endpoint.
type ExportQuery struct {
	NodeIDs []string
	Metrics []string
	From    time.Time
	To      time.Time
	Format  string // csv (default)
	Limit   int
	Cursor  string // opaque cursor for stable pagination
}

// Cursor is the opaque pagination token for stable export pagination.
type Cursor struct {
	Time   time.Time `json:"t"`
	Node   string    `json:"n"`
	Metric string    `json:"m"`
}

// Page is one page of exported telemetry rows plus the next cursor.
type Page struct {
	Rows       []TelemetryRow `json:"rows"`
	NextCursor string         `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
}

// NodeMetric describes a node that has telemetry and the metrics available.
type NodeMetric struct {
	NodeID   string   `json:"node_id"`
	ModuleID string   `json:"module_id"`
	Metrics  []string `json:"metrics"`
}
