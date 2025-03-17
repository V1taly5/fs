package connectionmanager

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
)

// ConnetionConfig содержит настройки для conneciton_manager
type ConnetionConfig struct {
	ConnectionTimeout time.Duration
	BaseRetryInterval time.Duration
	MaxRetryInterval  time.Duration
	// максисальное кол-во попыток (0 - без ограничений)
	MaxRetryCount int
}

type RetryOptions struct {
	// максисальное кол-во попыток (0 - без повторных попыток)
	MaxAttempts    int
	UseExponential bool
	// начальная задержка
	InitialDelay time.Duration
	// максимальная задаржка
	MaxDelay time.Duration
}

// Connection представляет интерфейс установленного соединения
type Connection interface {
	NodeID() string
	Address() string
	Protocol() string
	Close() error
	IsActive() bool
}

// Connector определяет интерфейс для различных типов соединений
type Connector interface {
	// Connect устанавливает соединение с заданным endpoint
	Connect(ctx context.Context, endpoints nodestorage.Endpoint) (Connection, error)
	// SuportProtocol проверяет, проддерживает ли данный протокол
	SuportProtocol(protocol string) bool
	Name() string
}

type ConnectionFactory struct {
	connectors map[string]Connector
	node       *node.Node
	log        *slog.Logger
}

type ConnectionManager struct {
	node         *node.Node
	db           NodeDB
	log          *slog.Logger
	config       *ConnetionConfig
	activeConns  *sync.Map // map[string]Connection
	connAttempts *sync.Map // map[string]int
	connFactory  *ConnectionFactory
	ctx          context.Context
	cancel       context.CancelFunc
}

type NodeDB interface {
	GetAllNodes() ([]nodestorage.Node, error)
	GetNodeByID(nodeID string) (nodestorage.Node, error)
	GetEndpointsForNode(nodeID string) ([]nodestorage.Endpoint, error)
	UpdateEndpointState(endpointID int64, success bool, timestamp int64) error
	UpdateNodeLastSeen(nodeID string, timestamp int64) error
}
