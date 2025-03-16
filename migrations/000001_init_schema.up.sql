CREATE TABLE IF NOT EXISTS nodes (
    node_id TEXT PRIMARY KEY,
    public_key BLOB NOT NULL,
    name TEXT,
    first_seen INTEGER NOT NULL,
    last_seen INTEGER NOT NULL,
    capabilities TEXT,
    is_introducer BOOLEAN DEFAULT FALSE,
    is_trusted BOOLEAN DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS endpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL,
    address TEXT NOT NULL,
    port INTEGER NOT NULL,
    protocol TEXT NOT NULL,
    source TEXT NOT NULL,
    last_success INTEGER,
    success_count INTEGER DEFAULT 0,
    failure_count INTEGER DEFAULT 0,
    priority INTEGER DEFAULT 0,
    FOREIGN KEY (node_id) REFERENCES nodes(node_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS connection_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL,
    endpoint_id INTEGER NOT NULL,
    connected_at INTEGER NOT NULL,
    disconnected_at INTEGER,
    disconnect_reason TEXT,
    bytes_sent INTEGER DEFAULT 0,
    bytes_received INTEGER DEFAULT 0,
    latency_ms INTEGER,
    FOREIGN KEY (node_id) REFERENCES nodes(node_id) ON DELETE CASCADE,
    FOREIGN KEY (endpoint_id) REFERENCES endpoints(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS shared_folders (
    node_id TEXT NOT NULL,
    folder_id TEXT NOT NULL,
    permissions TEXT NOT NULL,
    vector_clock TEXT,
    last_index_update INTEGER,
    PRIMARY KEY (node_id, folder_id),
    FOREIGN KEY (node_id) REFERENCES nodes(node_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_endpoints_node_id ON endpoints(node_id);
CREATE INDEX IF NOT EXISTS idx_connection_history_node_id ON connection_history(node_id);
CREATE INDEX IF NOT EXISTS idx_connection_history_endpoint_id ON connection_history(endpoint_id);
CREATE INDEX IF NOT EXISTS idx_nodes_last_seen ON nodes(last_seen);