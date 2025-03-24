package nodestorage

import (
	"context"
	"database/sql"
	"fs/pkg/migrator"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeFilter_BuildWhereClause(t *testing.T) {

	testCases := []struct {
		name   string
		filter NodeFilter
		where  string
		args   []interface{}
	}{
		{
			name:   "empty filter should return empty where",
			filter: NodeFilter{},
			where:  "",
			args:   []interface{}{},
		},
		{
			name: "Filter by introducer",
			filter: NodeFilter{
				IsIntroducer: ptrBool(true),
			},
			where: "is_introducer = ?",
			args:  []interface{}{true},
		},
		{
			name: "Filter by name like",
			filter: NodeFilter{
				NameLike: ptrString("node"),
			},
			where: "name LIKE ?",
			args:  []interface{}{"%node%"},
		},
		{
			name: "Filter by multiple fields",
			filter: NodeFilter{
				IsIntroducer: ptrBool(false),
				IsTrusted:    ptrBool(true),
			},
			where: "is_introducer = ? AND is_trusted = ?",
			args:  []interface{}{false, true},
		},
		{
			name: "Filter by last seen",
			filter: func() NodeFilter {
				timeVal := time.Unix(1700000000, 0)
				return NodeFilter{MinLastSeen: &timeVal}
			}(),
			where: "min_last_seen >= ?",
			args:  []interface{}{int64(1700000000)},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			where, args := tc.filter.buildWhereClause()
			assert.Equal(t, tc.where, where)
			assert.Equal(t, tc.args, args)
		})
	}
}

func setupTestStorage(t *testing.T) *NodeStorage {

	config := Config{
		DBPath:            "./datebase.sqlite",
		MigrationsPath:    "../../../migrations",
		ConnectionTimeout: 5 * time.Second,
		LogLevel:          slog.LevelDebug,
	}
	db, err := sql.Open("sqlite", config.DBPath)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	migrator := migrator.NewMigrator(db, migrator.Config{MigrationsPath: config.MigrationsPath}, logger)
	require.NoError(t, migrator.MigrateUp())

	storage, err := New(config, logger)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	return storage
}

func TestSaveNode_CreateNewNode(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	node := Node{
		ID:           "node-1",
		PublicKey:    []byte("pubkey-1"),
		Name:         "Test Node",
		FirstSeen:    time.Now(),
		LastSeen:     time.Now(),
		Capabilities: map[string]interface{}{"feature1": true, "feature2": "value"},
		IsIntroducer: true,
		IsTrusted:    false,
	}

	err := storage.SaveNode(ctx, node)
	assert.NoError(t, err)

	savedNode, err := storage.GetNode(ctx, node.ID)
	assert.NoError(t, err)

	assert.Equal(t, node.ID, savedNode.ID)
	assert.Equal(t, node.PublicKey, savedNode.PublicKey)
	assert.Equal(t, node.Name, savedNode.Name)
	assert.Equal(t, node.IsIntroducer, savedNode.IsIntroducer)
	assert.Equal(t, node.IsTrusted, savedNode.IsTrusted)

	// Проверяем capabilities
	assert.Equal(t, 2, len(savedNode.Capabilities))
	assert.Equal(t, true, savedNode.Capabilities["feature1"])
	assert.Equal(t, "value", savedNode.Capabilities["feature2"])

	// Проверяем временные метки с учетом округления до секунд
	assert.WithinDuration(t, node.FirstSeen, savedNode.FirstSeen, time.Second)
	assert.WithinDuration(t, node.LastSeen, savedNode.LastSeen, time.Second)

	err = ResetTestDB(storage.db)
	assert.NoError(t, err)
}

func TestSaveNodeUpdate(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	node := Node{
		ID:           "test-node-2",
		PublicKey:    []byte("test-public-key"),
		Name:         "Test Node 2",
		FirstSeen:    time.Now().Add(-24 * time.Hour),
		LastSeen:     time.Now(),
		Capabilities: map[string]interface{}{"feature1": true},
		IsIntroducer: false,
		IsTrusted:    false,
	}

	err := storage.SaveNode(ctx, node)
	assert.NoError(t, err)

	node.Name = "Updated Test Node 2"
	node.LastSeen = time.Now()
	node.Capabilities = map[string]interface{}{"feature1": false, "feature2": "new value"}
	node.IsIntroducer = true
	node.IsTrusted = true

	err = storage.SaveNode(ctx, node)
	assert.NoError(t, err)

	savedNode, err := storage.GetNode(ctx, node.ID)
	assert.NoError(t, err)

	assert.Equal(t, node.Name, savedNode.Name)
	assert.Equal(t, node.IsIntroducer, savedNode.IsIntroducer)
	assert.Equal(t, node.IsTrusted, savedNode.IsTrusted)

	assert.Equal(t, 2, len(savedNode.Capabilities))
	assert.Equal(t, false, savedNode.Capabilities["feature1"])
	assert.Equal(t, "new value", savedNode.Capabilities["feature2"])

	assert.WithinDuration(t, node.LastSeen, savedNode.LastSeen, time.Second)
}

func TestSaveNodeInvalidCapabilities(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	invalidCap := make(chan int)
	node := Node{
		ID:           "test-node-3",
		PublicKey:    []byte("test-public-key"),
		Name:         "Test Node 3",
		FirstSeen:    time.Now(),
		LastSeen:     time.Now(),
		Capabilities: map[string]interface{}{"invalidFeature": invalidCap},
		IsIntroducer: false,
		IsTrusted:    false,
	}
	err := storage.SaveNode(ctx, node)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), ErrInvalidInput.Error())
}

func TestSaveNodeWithEndpoints(t *testing.T) {
	ctx := context.Background()
	storage := setupTestStorage(t)

	node := Node{
		ID:           "test-node-4",
		PublicKey:    []byte("test-public-key"),
		Name:         "Test Node 4",
		FirstSeen:    time.Now().Add(-24 * time.Hour),
		LastSeen:     time.Now(),
		Capabilities: map[string]interface{}{"feature1": true},
		IsIntroducer: false,
		IsTrusted:    false,
		Endpoints: []Endpoint{
			{
				NodeID:       "test-node-4",
				Address:      "192.168.1.1",
				Port:         8080,
				Protocol:     ProtocolTCP,
				Source:       SourceStatic,
				LastSuccess:  time.Now(),
				SuccessCount: 10,
				FailureCount: 2,
				Priority:     5,
			},
		},
	}

	err := storage.SaveNode(ctx, node)
	assert.NoError(t, err)

	savedNode, err := storage.GetNode(ctx, node.ID)
	assert.NoError(t, err)

	require.Equal(t, 1, len(savedNode.Endpoints))
	assert.Equal(t, node.Endpoints[0].Address, savedNode.Endpoints[0].Address)
	assert.Equal(t, node.Endpoints[0].Port, savedNode.Endpoints[0].Port)
	assert.Equal(t, node.Endpoints[0].Protocol, savedNode.Endpoints[0].Protocol)
	assert.Equal(t, node.Endpoints[0].Source, savedNode.Endpoints[0].Source)
	assert.Equal(t, node.Endpoints[0].SuccessCount, savedNode.Endpoints[0].SuccessCount)
	assert.Equal(t, node.Endpoints[0].FailureCount, savedNode.Endpoints[0].FailureCount)
	assert.Equal(t, node.Endpoints[0].Priority, savedNode.Endpoints[0].Priority)
	assert.WithinDuration(t, node.Endpoints[0].LastSuccess, savedNode.Endpoints[0].LastSuccess, time.Second)
}

func TestSaveNodeUpdateEndpoints(t *testing.T) {
	ctx := context.Background()
	storage := setupTestStorage(t)

	node := Node{
		ID:           "test-node-5",
		PublicKey:    []byte("test-public-key"),
		Name:         "Test Node 5",
		FirstSeen:    time.Now().Add(-24 * time.Hour),
		LastSeen:     time.Now(),
		Capabilities: map[string]interface{}{"feature1": true},
		IsIntroducer: false,
		IsTrusted:    false,
		Endpoints: []Endpoint{
			{
				NodeID:       "test-node-5",
				Address:      "192.168.1.1",
				Port:         8080,
				Protocol:     ProtocolTCP,
				Source:       SourceStatic,
				LastSuccess:  time.Now(),
				SuccessCount: 10,
				FailureCount: 2,
				Priority:     5,
			},
		},
	}

	err := storage.SaveNode(ctx, node)
	assert.NoError(t, err)

	savedNode, err := storage.GetNode(ctx, node.ID)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(savedNode.Endpoints))

	node.Endpoints[0].ID = savedNode.Endpoints[0].ID
	node.Endpoints[0].Address = "192.168.1.2"
	node.Endpoints[0].Port = 9090
	node.Endpoints[0].SuccessCount = 15
	node.Endpoints[0].FailureCount = 3
	node.Endpoints[0].Priority = 10

	node.Endpoints = append(node.Endpoints, Endpoint{
		NodeID:       "test-node-5",
		Address:      "10.0.0.1",
		Port:         443,
		Protocol:     ProtocolQUIC,
		Source:       SourceGlobal,
		LastSuccess:  time.Now(),
		SuccessCount: 5,
		FailureCount: 0,
		Priority:     7,
	})

	err = storage.SaveNode(ctx, node)
	assert.NoError(t, err)

	updatedNode, err := storage.GetNode(ctx, node.ID)
	assert.NoError(t, err)

	assert.Equal(t, 2, len(node.Endpoints))
	assert.Equal(t, node.Endpoints[0].Address, updatedNode.Endpoints[0].Address)
	assert.Equal(t, node.Endpoints[0].Port, updatedNode.Endpoints[0].Port)
	assert.Equal(t, node.Endpoints[0].SuccessCount, updatedNode.Endpoints[0].SuccessCount)
	assert.Equal(t, node.Endpoints[0].FailureCount, updatedNode.Endpoints[0].FailureCount)
	assert.Equal(t, node.Endpoints[0].Priority, updatedNode.Endpoints[0].Priority)
	assert.WithinDuration(t, node.Endpoints[0].LastSuccess, updatedNode.Endpoints[0].LastSuccess, time.Second)

	assert.Equal(t, node.Endpoints[1].Address, updatedNode.Endpoints[1].Address)
	assert.Equal(t, node.Endpoints[1].Port, updatedNode.Endpoints[1].Port)
	assert.Equal(t, node.Endpoints[1].SuccessCount, updatedNode.Endpoints[1].SuccessCount)
	assert.Equal(t, node.Endpoints[1].FailureCount, updatedNode.Endpoints[1].FailureCount)
	assert.Equal(t, node.Endpoints[1].Priority, updatedNode.Endpoints[1].Priority)
	assert.WithinDuration(t, node.Endpoints[1].LastSuccess, updatedNode.Endpoints[1].LastSuccess, time.Second)
}

func TestNodeStorage_GetNodeEndpoints(t *testing.T) {
	ctx := context.Background()
	storage := setupTestStorage(t)

	testCases := []struct {
		name          string
		setupFunc     func(t *testing.T) string
		expectedErr   string
		expectedCount int
	}{
		{
			name: "successful receipt of endpoints",
			setupFunc: func(t *testing.T) string {
				nodeID := "test-node-5"
				node := Node{
					ID:           nodeID,
					PublicKey:    []byte("test-public-key"),
					Name:         "Test Node 5",
					FirstSeen:    time.Now().Add(-24 * time.Hour),
					LastSeen:     time.Now(),
					Capabilities: map[string]interface{}{"feature1": true},
					IsIntroducer: false,
					IsTrusted:    false,
					Endpoints: []Endpoint{
						{
							NodeID:       "test-node-5",
							Address:      "192.168.1.1",
							Port:         8080,
							Protocol:     ProtocolTCP,
							Source:       SourceStatic,
							LastSuccess:  time.Now(),
							SuccessCount: 10,
							FailureCount: 2,
							Priority:     5,
						},
						{
							NodeID:       "test-node-6",
							Address:      "192.168.1.2",
							Port:         8080,
							Protocol:     "http",
							Source:       "discovery",
							SuccessCount: 5,
							FailureCount: 0,
							Priority:     2,
						},
					},
				}

				err := storage.SaveNode(ctx, node)
				assert.NoError(t, err)
				return nodeID
			},
			expectedCount: 2,
		},
		{
			name: "no endpoints for the node",
			setupFunc: func(t *testing.T) string {
				nodeID := "test-node-2"
				node := Node{
					ID:           nodeID,
					PublicKey:    []byte("test-public-key-2"),
					Name:         "Test Node 2",
					FirstSeen:    time.Now().Add(-24 * time.Hour),
					LastSeen:     time.Now(),
					Capabilities: map[string]interface{}{"feature2": true},
					IsIntroducer: true,
					IsTrusted:    true,
					Endpoints:    []Endpoint{}, // Пусто
				}

				err := storage.SaveNode(ctx, node)
				assert.NoError(t, err)
				return nodeID
			},
			expectedCount: 0,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ResetTestDB(storage.db)
			assert.NoError(t, err)

			nodeID := tc.setupFunc(t)

			endpoints, err := storage.GetNodeEndpoints(ctx, nodeID)
			if tc.expectedErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Len(t, endpoints, tc.expectedCount)

				if tc.name == "successful receipt of endpoints" && len(endpoints) >= 2 {
					// Проверка первого эндпоинта
					assert.Equal(t, nodeID, endpoints[0].NodeID)
					assert.Equal(t, "192.168.1.1", endpoints[0].Address)
					assert.Equal(t, 8080, endpoints[0].Port)
					assert.Equal(t, ProtocolTCP, endpoints[0].Protocol)
					assert.Equal(t, SourceStatic, endpoints[0].Source)
					assert.Equal(t, 10, endpoints[0].SuccessCount)
					assert.Equal(t, 2, endpoints[0].FailureCount)
					assert.Equal(t, 5, endpoints[0].Priority)

					// Проверка второго эндпоинта
					assert.Equal(t, nodeID, endpoints[1].NodeID)
					assert.Equal(t, "192.168.1.2", endpoints[1].Address)
				}
			}
			// Очищаем БД после теста
			err = ResetTestDB(storage.db)
			assert.NoError(t, err)
		})
	}
}

func ResetTestDB(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	queries := []string{
		"DELETE FROM connection_history;",
		"DELETE FROM shared_folders;",
		"DELETE FROM endpoints;",
		"DELETE FROM nodes;",
		"DELETE FROM sqlite_sequence;", // Сброс автоинкремента
	}

	for _, query := range queries {
		if _, err := tx.Exec(query); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func ptrBool(b bool) *bool {
	return &b
}

func ptrString(s string) *string {
	return &s
}

// func ptrTime(t time.Time) *time.Time {
// 	return &t
// }
