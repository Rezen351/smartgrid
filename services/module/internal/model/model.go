package model

import "time"

// Node status values derived from MQTT discovery / status (LWT) messages.
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusUnknown = "unknown"
)

// Module is a logical configuration container. One module owns many nodes.
// It holds settings (config) shared by / applied to its nodes.
type Module struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Config      string    `json:"config"` // arbitrary JSON settings blob
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Populated on detail reads.
	Nodes []Node `json:"nodes,omitempty"`
}

// Node is a physical ESP32 device (identified by NodeID/MAC from firmware).
// A node may be discovered before it is paired (ModuleID == nil).
type Node struct {
	ID           string     `json:"id"`
	NodeID       string     `json:"node_id"`             // firmware node_id (MAC-based)
	ModuleID     *string    `json:"module_id,omitempty"` // nil until paired
	Name         string     `json:"name"`
	MAC          string     `json:"mac"`
	IP           string     `json:"ip"`
	FWVersion    string     `json:"fw_version"`
	Status       string     `json:"status"`
	Paired       bool       `json:"paired"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	DiscoveredAt time.Time  `json:"discovered_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// ─── Request DTOs ────────────────────────────────────────────────────────────

type CreateModuleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Config      string `json:"config"`
}

type UpdateModuleRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Config      *string `json:"config"`
}

type PairRequest struct {
	ModuleID string `json:"module_id"`
	Name     string `json:"name"` // optional friendly name
}

// DiscoveryMessage is the payload published by firmware on {prefix}/discovery.
type DiscoveryMessage struct {
	NodeID    string `json:"node_id"`
	MAC       string `json:"mac"`
	IP        string `json:"ip"`
	FWVersion string `json:"fw_version"`
	Status    string `json:"status"`
}

// StatusMessage is the retained payload on {prefix}/status/{node_id} (LWT).
type StatusMessage struct {
	Status string `json:"status"`
	MAC    string `json:"mac"`
	FW     string `json:"fw"`
	IP     string `json:"ip"`
}

// ─── Telemetry tag mapping (modular acquisition) ──────────────────────────────

// NodeTag is the declarative mapping that attaches an MQTT telemetry key
// (SourceKey) to a database tag (TagName). Kind distinguishes sensor telemetry
// tags (Kind="sensor") from actuator/control tags (Kind="actuator"). Actuator
// tags are the controllable outputs the user explicitly maps, separate from the
// sensor telemetry schema.
type NodeTag struct {
	ID          string    `json:"id"`
	NodeID      string    `json:"node_id"`
	Kind        string    `json:"kind"`       // sensor | actuator
	SourceKey   string    `json:"source_key"` // sensor: telemetry key; actuator: firmware output name
	TagName     string    `json:"tag_name"`   // friendly DB tag
	DisplayName string    `json:"display_name"`
	Unit        string    `json:"unit"`
	DataType    string    `json:"data_type"` // float | int | bool
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NodeTagRequest is the editable shape sent from the dashboard.
type NodeTagRequest struct {
	ID          string `json:"id,omitempty"`
	Kind        string `json:"kind,omitempty"` // sensor | actuator (default sensor)
	SourceKey   string `json:"source_key"`
	TagName     string `json:"tag_name"`
	DisplayName string `json:"display_name"`
	Unit        string `json:"unit"`
	DataType    string `json:"data_type"`
	Enabled     bool   `json:"enabled"`
}
