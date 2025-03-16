DROP INDEX IF EXISTS idx_endpoints_node_id;
DROP INDEX IF EXISTS idx_connection_history_node_id;
DROP INDEX IF EXISTS idx_connection_history_endpoint_id;
DROP INDEX IF EXISTS idx_nodes_last_seen;

DROP TABLE IF EXISTS shared_folders;
DROP TABLE IF EXISTS connection_history;
DROP TABLE IF EXISTS endpoints;
DROP TABLE IF EXISTS nodes;
