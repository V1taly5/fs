// Package nodestorage provides persistent storage for distributed network nodes
// using SQLite as the underlying database.
package nodestorage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Common errors
var (
	ErrNodeNotFound      = errors.New("node not found")
	ErrEndpointNotFound  = errors.New("endpoint not found")
	ErrDBOperationFailed = errors.New("database operation failed")
	ErrInvalidInput      = errors.New("invalid input parameters")
)

// Protocol defines the connection protocol type
type Protocol string

// Source defines where the endpoint information was discovered
type Source string

// Available protocols
const (
	ProtocolTCP  Protocol = "tcp"
	ProtocolUDP  Protocol = "udp"
	ProtocolQUIC Protocol = "quic"
)

// Available endpoint sources
const (
	SourceStatic Source = "static"
	SourceLocal  Source = "local"
	SourceGlobal Source = "global"
	SourceRelay  Source = "relay"
)

// buildWhereClause constructs a WHERE clause and corresponding arguments for filtering nodes
func (n *NodeFilter) buildWhereClause() (string, []interface{}) {
	where := []string{}
	args := []interface{}{}

	if n.IsIntroducer != nil {
		where = append(where, "is_introducer = ?")
		args = append(args, *n.IsIntroducer)
	}
	if n.IsTrusted != nil {
		where = append(where, "is_trusted = ?")
		args = append(args, *n.IsTrusted)
	}
	if n.MinLastSeen != nil {
		where = append(where, "min_last_seen >= ?")
		args = append(args, n.MinLastSeen.Unix())
	}
	if n.NameLike != nil && *n.NameLike != "" {
		where = append(where, "name LIKE ?")
		args = append(args, "%"+*n.NameLike+"%")
	}
	if n.HasCapability != nil && *n.HasCapability != "" {
		where = append(where, "capabilities LIKE ?")
		args = append(args, "%\""+*n.HasCapability+"\"%")
	}
	var query string
	if len(where) > 0 {
		query = strings.Join(where, " AND ")
	}
	return query, args
}

// Config contains configuration for the NodeStorage
type Config struct {
	DBPath            string
	MigrationsPath    string
	ConnectionTimeout time.Duration
	LogLevel          slog.Level
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() Config {
	return Config{
		DBPath:            "database.sqlite",
		MigrationsPath:    "migrations",
		ConnectionTimeout: 5 * time.Second,
		LogLevel:          slog.LevelInfo,
	}
}

// NodeStorage provides an interface to store and retrieve node information
type NodeStorage struct {
	db     *sql.DB
	logger *slog.Logger
	config Config
	mu     sync.Mutex // mutex for serializing write operations
}

// New creates a new NodeStorage with the given configuration
func New(config Config, logger *slog.Logger) (*NodeStorage, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: config.LogLevel}))
	}

	db, err := sql.Open("sqlite", config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// set connection parameters
	db.SetMaxOpenConns(1) // SQLite supports only one writer at a time
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(10 * time.Minute)

	// Create storage instance
	storage := &NodeStorage{
		db:     db,
		logger: logger,
		config: config,
	}

	return storage, nil
}

// Close closes the database connection
func (s *NodeStorage) Close() error {
	s.logger.Info("Closing node storage")
	return s.db.Close()
}

// SaveNode stores a node in the database, creating it if it doesn't exist
// or updating it if it does
func (s *NodeStorage) SaveNode(ctx context.Context, node Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: failed to begin transaction: %v", ErrDBOperationFailed, err)
	}
	defer tx.Rollback() // rollback if not committed

	// check if node already exists
	var exists bool
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE node_id = ?", node.ID).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("%w: failed to check node existence: %v", ErrDBOperationFailed, err)
	}

	// marshal capabilities to JSON
	capabilitiesJSON, err := json.Marshal(node.Capabilities)
	if err != nil {
		return fmt.Errorf("%w: failed to marshal capabilities: %v", ErrInvalidInput, err)
	}

	if err == sql.ErrNoRows || !exists {
		// node doesn't exist, create it
		_, err = tx.ExecContext(ctx,
			`INSERT INTO nodes (
				node_id, public_key, name, first_seen, last_seen, 
				capabilities, is_introducer, is_trusted
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			node.ID, node.PublicKey, node.Name, node.FirstSeen.Unix(), node.LastSeen.Unix(),
			capabilitiesJSON, node.IsIntroducer, node.IsTrusted,
		)
		if err != nil {
			return fmt.Errorf("%w: failed to insert node: %v", ErrDBOperationFailed, err)
		}

		s.logger.Info("Created new node", "node_id", node.ID, "name", node.Name)
	} else {
		// node exists, update it
		_, err = tx.ExecContext(ctx,
			`UPDATE nodes SET 
				public_key = ?, name = ?, last_seen = ?, 
				capabilities = ?, is_introducer = ?, is_trusted = ?
			WHERE node_id = ?`,
			node.PublicKey, node.Name, node.LastSeen.Unix(),
			capabilitiesJSON, node.IsIntroducer, node.IsTrusted,
			node.ID,
		)
		if err != nil {
			return fmt.Errorf("%w: failed to update node: %v", ErrDBOperationFailed, err)
		}

		s.logger.Debug("Updated existing node", "node_id", node.ID, "name", node.Name)
	}

	// handle endpoints if provided
	if len(node.Endpoints) > 0 {
		for _, endpoint := range node.Endpoints {
			endpoint.NodeID = node.ID // Ensure NodeID is set correctly

			if endpoint.ID > 0 {
				// update existing endpoint
				_, err = tx.ExecContext(ctx,
					`UPDATE endpoints SET 
						address = ?, port = ?, protocol = ?, source = ?,
						last_success = ?, success_count = ?, failure_count = ?, priority = ?
					WHERE id = ? AND node_id = ?`,
					endpoint.Address, endpoint.Port, endpoint.Protocol, endpoint.Source,
					nullableTime(endpoint.LastSuccess), endpoint.SuccessCount, endpoint.FailureCount, endpoint.Priority,
					endpoint.ID, endpoint.NodeID,
				)
				if err != nil {
					return fmt.Errorf("%w: failed to update endpoint: %v", ErrDBOperationFailed, err)
				}
			} else {
				// insert new endpoint
				result, err := tx.ExecContext(ctx,
					`INSERT INTO endpoints (
						node_id, address, port, protocol, source,
						last_success, success_count, failure_count, priority
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					endpoint.NodeID, endpoint.Address, endpoint.Port, endpoint.Protocol, endpoint.Source,
					nullableTime(endpoint.LastSuccess), endpoint.SuccessCount, endpoint.FailureCount, endpoint.Priority,
				)
				if err != nil {
					return fmt.Errorf("%w: failed to insert endpoint: %v", ErrDBOperationFailed, err)
				}

				// get the newly created endpoint ID
				id, err := result.LastInsertId()
				if err == nil {
					endpoint.ID = id
				}
			}
		}
	}

	// Handle shared folders if provided
	if len(node.SharedFolders) > 0 {
		for _, folder := range node.SharedFolders {
			folder.NodeID = node.ID // Ensure NodeID is set correctly

			// Marshal vector clock to JSON
			vectorClockJSON, err := json.Marshal(folder.VectorClock)
			if err != nil {
				return fmt.Errorf("%w: failed to marshal vector clock: %v", ErrInvalidInput, err)
			}

			// Use REPLACE to handle both insert and update
			_, err = tx.ExecContext(ctx,
				`REPLACE INTO shared_folders (
					node_id, folder_id, permissions, vector_clock, last_index_update
				) VALUES (?, ?, ?, ?, ?)`,
				folder.NodeID, folder.FolderID, folder.Permissions,
				vectorClockJSON, nullableTime(folder.LastIndexUpdate),
			)
			if err != nil {
				return fmt.Errorf("%w: failed to save shared folder: %v", ErrDBOperationFailed, err)
			}
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: failed to commit transaction: %v", ErrDBOperationFailed, err)
	}

	return nil
}

// GetNode retrieves a node by its ID, including all related data
func (s *NodeStorage) GetNode(ctx context.Context, nodeID string) (Node, error) {
	var node Node

	// query the main node data
	row := s.db.QueryRowContext(ctx, `
		SELECT node_id, public_key, name, first_seen, last_seen, 
		       capabilities, is_introducer, is_trusted
		FROM nodes WHERE node_id = ?`, nodeID)

	var firstSeenUnix, lastSeenUnix int64
	var capabilitiesJSON string
	err := row.Scan(
		&node.ID, &node.PublicKey, &node.Name, &firstSeenUnix, &lastSeenUnix,
		&capabilitiesJSON, &node.IsIntroducer, &node.IsTrusted,
	)
	if err == sql.ErrNoRows {
		return node, ErrNodeNotFound
	}
	if err != nil {
		return node, fmt.Errorf("%w: failed to get node: %v", ErrDBOperationFailed, err)
	}

	// convert Unix timestamps to time.Time
	node.FirstSeen = time.Unix(firstSeenUnix, 0)
	node.LastSeen = time.Unix(lastSeenUnix, 0)

	// parse capabilities JSON
	if capabilitiesJSON != "" {
		if err := json.Unmarshal([]byte(capabilitiesJSON), &node.Capabilities); err != nil {
			s.logger.Warn("Failed to unmarshal node capabilities",
				"node_id", node.ID, "error", err)
			// Continue even if capabilities can't be parsed
			node.Capabilities = make(map[string]any)
		}
	} else {
		node.Capabilities = make(map[string]any)
	}

	// get endpoints
	node.Endpoints, err = s.GetNodeEndpoints(ctx, nodeID)
	if err != nil {
		s.logger.Warn("Failed to get node endpoints",
			"node_id", node.ID, "error", err)
		// Continue even if we can't get endpoints
	}

	// get shared folders
	node.SharedFolders, err = s.getNodeSharedFolders(ctx, nodeID)
	if err != nil {
		s.logger.Warn("Failed to get node shared folders",
			"node_id", node.ID, "error", err)
		// Continue even if we can't get shared folders
	}

	return node, nil
}

// getNodeEndpoints retrieves all endpoints for a given node
func (s *NodeStorage) GetNodeEndpoints(ctx context.Context, nodeID string) ([]Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_id, address, port, protocol, source,
		       last_success, success_count, failure_count, priority
		FROM endpoints WHERE node_id = ?
		ORDER BY priority DESC`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query endpoints: %w", err)
	}
	defer rows.Close()

	var endpoints []Endpoint
	for rows.Next() {
		var e Endpoint
		var lastSuccessUnix sql.NullInt64
		err := rows.Scan(
			&e.ID, &e.NodeID, &e.Address, &e.Port, &e.Protocol, &e.Source,
			&lastSuccessUnix, &e.SuccessCount, &e.FailureCount, &e.Priority,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan endpoint: %w", err)
		}

		if lastSuccessUnix.Valid {
			e.LastSuccess = time.Unix(lastSuccessUnix.Int64, 0)
		}

		endpoints = append(endpoints, e)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating through endpoints: %w", err)
	}

	return endpoints, nil
}

// getNodeSharedFolders retrieves all shared folders for a given node
func (s *NodeStorage) getNodeSharedFolders(ctx context.Context, nodeID string) ([]SharedFolder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, folder_id, permissions, vector_clock, last_index_update
		FROM shared_folders WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query shared folders: %w", err)
	}
	defer rows.Close()

	var folders []SharedFolder
	for rows.Next() {
		var f SharedFolder
		var vectorClockJSON string
		var lastIndexUpdateUnix sql.NullInt64

		err := rows.Scan(
			&f.NodeID, &f.FolderID, &f.Permissions, &vectorClockJSON, &lastIndexUpdateUnix,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan shared folder: %w", err)
		}

		// parse vector clock JSON
		if vectorClockJSON != "" {
			if err := json.Unmarshal([]byte(vectorClockJSON), &f.VectorClock); err != nil {
				s.logger.Warn("Failed to unmarshal vector clock",
					"node_id", f.NodeID, "folder_id", f.FolderID, "error", err)
				// continue even if vector clock can't be parsed
				f.VectorClock = make(map[string]int64)
			}
		} else {
			f.VectorClock = make(map[string]int64)
		}

		if lastIndexUpdateUnix.Valid {
			f.LastIndexUpdate = time.Unix(lastIndexUpdateUnix.Int64, 0)
		}

		folders = append(folders, f)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating through shared folders: %w", err)
	}

	return folders, nil
}

// ListNodes returns all nodes matching the given filter
func (s *NodeStorage) ListNodes(ctx context.Context, filter NodeFilter) ([]Node, error) {
	// build query based on filter
	query := "SELECT node_id FROM nodes"
	args := []interface{}{}

	whereClause, filterArgs := filter.buildWhereClause()
	if whereClause != "" {
		query += " WHERE " + whereClause
		args = append(args, filterArgs...)
	}

	// apply ordering
	query += " ORDER BY last_seen DESC"

	// apply limit if specified
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	// execute query
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to list nodes: %v", ErrDBOperationFailed, err)
	}
	defer rows.Close()

	// collect node IDs
	var nodeIDs []string
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, fmt.Errorf("%w: failed to scan node ID: %v", ErrDBOperationFailed, err)
		}
		nodeIDs = append(nodeIDs, nodeID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: error iterating through nodes: %v", ErrDBOperationFailed, err)
	}

	// fetch complete node data for each ID
	nodes := make([]Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, err := s.GetNode(ctx, nodeID)
		if err != nil {
			s.logger.Warn("Failed to get node during list operation",
				"node_id", nodeID, "error", err)
			continue // Skip this node but continue with others
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// DeleteNode removes a node and all associated data
func (s *NodeStorage) DeleteNode(ctx context.Context, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: failed to begin transaction: %v", ErrDBOperationFailed, err)
	}
	defer tx.Rollback() // rollback if not committed

	// check if node exists
	var exists bool
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE node_id = ?", nodeID).Scan(&exists)
	if err == sql.ErrNoRows {
		return ErrNodeNotFound
	}
	if err != nil {
		return fmt.Errorf("%w: failed to check node existence: %v", ErrDBOperationFailed, err)
	}

	// delete from nodes table - will cascade to related tables due to foreign key constraints
	result, err := tx.ExecContext(ctx, "DELETE FROM nodes WHERE node_id = ?", nodeID)
	if err != nil {
		return fmt.Errorf("%w: failed to delete node: %v", ErrDBOperationFailed, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: failed to get rows affected: %v", ErrDBOperationFailed, err)
	}

	if rowsAffected == 0 {
		return ErrNodeNotFound
	}

	// commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: failed to commit transaction: %v", ErrDBOperationFailed, err)
	}

	s.logger.Info("Deleted node", "node_id", nodeID)
	return nil
}

// AddEndpoint adds a new endpoint to a node if it doesn't already exist
func (s *NodeStorage) AddEndpoint(ctx context.Context, nodeID string, newEndpoint Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: failed to begin transaction: %v", ErrDBOperationFailed, err)
	}
	defer tx.Rollback()

	// check if the endpoint already exists for the given node
	var exists bool
	err = tx.QueryRowContext(ctx,
		`SELECT EXISTS(
            SELECT 1 FROM endpoints
            WHERE node_id = ? AND address = ? AND port = ? AND protocol = ?
        )`, nodeID, newEndpoint.Address, newEndpoint.Port, newEndpoint.Protocol).Scan(&exists)
	if err != nil {
		return fmt.Errorf("%w: failed to check endpoint existence: %v", ErrDBOperationFailed, err)
	}

	if exists {
		s.logger.Info("Endpoint already exists", "node_id", nodeID, "address", newEndpoint.Address, "port", newEndpoint.Port)
		return nil
	}

	// insert the new endpoint
	_, err = tx.ExecContext(ctx,
		`INSERT INTO endpoints (
            node_id, address, port, protocol, source,
            last_success, success_count, failure_count, priority
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nodeID, newEndpoint.Address, newEndpoint.Port, newEndpoint.Protocol, newEndpoint.Source,
		nullableTime(newEndpoint.LastSuccess), newEndpoint.SuccessCount, newEndpoint.FailureCount, newEndpoint.Priority,
	)
	if err != nil {
		return fmt.Errorf("%w: failed to insert endpoint: %v", ErrDBOperationFailed, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: failed to commit transaction: %v", ErrDBOperationFailed, err)
	}

	s.logger.Info("Added new endpoint", "node_id", nodeID, "address", newEndpoint.Address, "port", newEndpoint.Port)
	return nil
}

// UpdateEndpoint updates or adds an endpoint for a node
func (s *NodeStorage) UpdateEndpoint(ctx context.Context, endpoint Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// check if node exists
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE node_id = ?", endpoint.NodeID).Scan(&exists)
	if err == sql.ErrNoRows {
		return ErrNodeNotFound
	}
	if err != nil {
		return fmt.Errorf("%w: failed to check node existence: %v", ErrDBOperationFailed, err)
	}

	// if endpoint ID is provided, update existing endpoint
	if endpoint.ID > 0 {
		result, err := s.db.ExecContext(ctx,
			`UPDATE endpoints SET 
				address = ?, port = ?, protocol = ?, source = ?,
				last_success = ?, success_count = ?, failure_count = ?, priority = ?
			WHERE id = ? AND node_id = ?`,
			endpoint.Address, endpoint.Port, endpoint.Protocol, endpoint.Source,
			nullableTime(endpoint.LastSuccess), endpoint.SuccessCount, endpoint.FailureCount, endpoint.Priority,
			endpoint.ID, endpoint.NodeID,
		)
		if err != nil {
			return fmt.Errorf("%w: failed to update endpoint: %v", ErrDBOperationFailed, err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("%w: failed to get rows affected: %v", ErrDBOperationFailed, err)
		}

		if rowsAffected == 0 {
			return ErrEndpointNotFound
		}

		s.logger.Debug("Updated endpoint",
			"endpoint_id", endpoint.ID, "node_id", endpoint.NodeID)

	} else {
		// insert new endpoint
		result, err := s.db.ExecContext(ctx,
			`INSERT INTO endpoints (
				node_id, address, port, protocol, source,
				last_success, success_count, failure_count, priority
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			endpoint.NodeID, endpoint.Address, endpoint.Port, endpoint.Protocol, endpoint.Source,
			nullableTime(endpoint.LastSuccess), endpoint.SuccessCount, endpoint.FailureCount, endpoint.Priority,
		)
		if err != nil {
			return fmt.Errorf("%w: failed to insert endpoint: %v", ErrDBOperationFailed, err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("%w: failed to get endpoint ID: %v", ErrDBOperationFailed, err)
		}

		endpoint.ID = id
		s.logger.Debug("Created new endpoint",
			"endpoint_id", endpoint.ID, "node_id", endpoint.NodeID)
	}

	return nil
}

// UpdateEndpointStats updates the statistics of an endpoint based on connection success or failure
func (s *NodeStorage) UpdateEndpointStats(ctx context.Context, endpointID int64, isSuccess bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var query string
	var args []interface{}

	if isSuccess {
		query = `
			UPDATE endpoints
			SET last_success = ?, success_count = success_count + 1
			WHERE id = ?`
		args = []interface{}{time.Now().Unix(), endpointID}
	} else {
		query = `
			UPDATE endpoints
			SET failure_count = failure_count + 1
			WHERE id = ?`
		args = []interface{}{endpointID}
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%w: failed to update endpoint stats: %v", ErrDBOperationFailed, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: failed to get rows affected: %v", ErrDBOperationFailed, err)
	}

	if rowsAffected == 0 {
		return ErrEndpointNotFound
	}

	s.logger.Debug("Update endpoint stats",
		"endpoint_id", endpointID,
		"is_seccess", isSuccess,
	)
	return nil
}

// DeleteEndpoint removes an endpoint
func (s *NodeStorage) DeleteEndpoint(ctx context.Context, endpointID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, "DELETE FROM endpoints WHERE id = ?", endpointID)
	if err != nil {
		return fmt.Errorf("%w: failed to delete endpoint: %v", ErrDBOperationFailed, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: failed to get rows affected: %v", ErrDBOperationFailed, err)
	}

	if rowsAffected == 0 {
		return ErrEndpointNotFound
	}

	s.logger.Debug("Deleted endpoint", "endpoint_id", endpointID)
	return nil
}

// RecordConnection adds a new connection record
func (s *NodeStorage) RecordConnection(ctx context.Context, record ConnectionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// check if node and endpoint exist
	var exists bool
	err := s.db.QueryRowContext(ctx,
		"SELECT 1 FROM nodes n JOIN endpoints e ON n.node_id = e.node_id WHERE n.node_id = ? AND e.id = ?",
		record.NodeID, record.EndpointID).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("node or endpoint not found")
	}
	if err != nil {
		return fmt.Errorf("%w: failed to check node/endpoint existence: %v", ErrDBOperationFailed, err)
	}

	// insert connection record
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO connection_history (
			node_id, endpoint_id, connected_at, disconnected_at, 
			disconnect_reason, bytes_sent, bytes_received, latency_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.NodeID, record.EndpointID, record.ConnectedAt.Unix(),
		nullableTime(record.DisconnectedAt), record.DisconnectReason,
		record.BytesSent, record.BytesReceived, record.LatencyMs,
	)
	if err != nil {
		return fmt.Errorf("%w: failed to insert connection record: %v", ErrDBOperationFailed, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("%w: failed to get connection record ID: %v", ErrDBOperationFailed, err)
	}

	record.ID = id
	s.logger.Debug("Recorded new connection",
		"record_id", record.ID, "node_id", record.NodeID, "endpoint_id", record.EndpointID)

	return nil
}

// PruneOldConnections removes connection records older than the specified time
func (s *NodeStorage) PruneOldConnections(ctx context.Context, olderThan time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`DELETE FROM connection_history WHERE connected_at < ?`,
		olderThan.Unix(),
	)

	if err != nil {
		return 0, fmt.Errorf("%w: failed to prune old connections: %v", ErrDBOperationFailed, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to get rows affected: %v", ErrDBOperationFailed, err)
	}

	if rowsAffected > 0 {
		s.logger.Info("Pruned old connection records", "count", rowsAffected, "older_than", olderThan)
	}

	return rowsAffected, nil
}

// PruneOldNodes removes nodes that haven't been seen for a specified duration
func (s *NodeStorage) PruneOldNodes(ctx context.Context, notSeenSince time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`DELETE FROM nodes WHERE last_seen < ? AND is_trusted = FALSE`,
		notSeenSince.Unix(),
	)

	if err != nil {
		return 0, fmt.Errorf("%w: failed to prune old nodes: %v", ErrDBOperationFailed, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to get rows affected: %v", ErrDBOperationFailed, err)
	}

	if rowsAffected > 0 {
		s.logger.Info("Pruned old nodes", "count", rowsAffected, "not_seen_since", notSeenSince)
	}

	return rowsAffected, nil
}

// Utility function to handle nullable timestamps for SQL
func nullableTime(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}
