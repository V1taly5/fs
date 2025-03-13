package nodestorage

import "time"

// Node represents a node in the distributed system
type Node struct {
	ID            string         `json:"id"`
	PublicKey     []byte         `json:"public_key"`
	Name          string         `json:"name,omitempty"`
	FirstSeen     time.Time      `json:"first_seen"`
	LastSeen      time.Time      `json:"last_seen"`
	Capabilities  map[string]any `json:"capabilities,omitempty"`
	IsIntroducer  bool           `json:"is_introducer"`
	IsTrusted     bool           `json:"is_trusted"`
	Endpoints     []Endpoint     `json:"endpoints,omitempty"`
	SharedFolders []SharedFolder `json:"shared_folders,omitempty"`
}

// Endpoint represents a connection point for a node
type Endpoint struct {
	ID           int64     `json:"id,omitempty"`
	NodeID       string    `json:"node_id"`
	Address      string    `json:"address"`
	Port         int       `json:"port"`
	Protocol     Protocol  `json:"protocol"`
	Source       Source    `json:"source"`
	LastSuccess  time.Time `json:"last_success,omitempty"`
	SuccessCount int       `json:"success_count"`
	FailureCount int       `json:"failure_count"`
	Priority     int       `json:"priority"`
}

// ConnectionRecord represents a historical connection to a node
type ConnectionRecord struct {
	ID               int64     `json:"id,omitempty"`
	NodeID           string    `json:"node_id"`
	EndpointID       int64     `json:"endpoint_id"`
	ConnectedAt      time.Time `json:"connected_at"`
	DisconnectedAt   time.Time `json:"disconnected_at,omitempty"`
	DisconnectReason string    `json:"disconnect_reason,omitempty"`
	BytesSent        int64     `json:"bytes_sent"`
	BytesReceived    int64     `json:"bytes_received"`
	LatencyMs        int       `json:"latency_ms"`
}

// SharedFolder represents a folder shared with a node
type SharedFolder struct {
	NodeID          string           `json:"node_id"`
	FolderID        string           `json:"folder_id"`
	Permissions     string           `json:"permissions"`
	VectorClock     map[string]int64 `json:"vector_clock,omitempty"`
	LastIndexUpdate time.Time        `json:"last_index_update,omitempty"`
}

// NodeFilter defines filtering options for listing nodes
type NodeFilter struct {
	IsIntroducer  *bool      `json:"is_introducer,omitempty"`  // Filter by introducer status
	IsTrusted     *bool      `json:"is_trusted,omitempty"`     // Filter by trusted status
	MinLastSeen   *time.Time `json:"min_last_seen,omitempty"`  // Filter by minimum last seen time
	NameLike      *string    `json:"name_like,omitempty"`      // Filter by name pattern (SQL LIKE)
	HasCapability *string    `json:"has_capability,omitempty"` // Filter by having a specific capability
	Limit         int        `json:"limit,omitempty"`          // Maximum number of nodes to return
	Offset        int        `json:"offset,omitempty"`         // Number of nodes to skip (pagination)
}
