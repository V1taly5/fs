package connectionmanager

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
	fsv1 "fs/proto/gen/go"
)

var (
	ErrNoActiveConnection = errors.New("no active connection to node")
	ErrConnectionInactive = errors.New("connection is inactive")
	ErrMessageBufferFull  = errors.New("message buffer is full")
	ErrContextCanceled    = errors.New("context was canceled")
	ErrConnectionClosed   = errors.New("connection is closed")
)

// ConnectionConfig содержит настройки для conneciton_manager
type ConnectionConfig struct {
	ConnectionTimeout time.Duration
	BaseRetryInterval time.Duration
	MaxRetryInterval  time.Duration
	// максимальное кол-во попыток (0 - без ограничений)
	MaxRetryCount int

	AutoStartProcess bool
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
	StartReading(context.Context)
	SendMessage(context.Context, *fsv1.Message) error
	MessageChannel() <-chan *fsv1.Message
}

// Connector определяет интерфейс для различных типов соединений
type Connector interface {
	// connect устанавливает соединение с заданным endpoint
	Connect(ctx context.Context, endpoints nodestorage.Endpoint) (Connection, error)
	// suportProtocol проверяет, проддерживает ли данный протокол
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
	config       *ConnectionConfig
	activeConns  *sync.Map // map[string]Connection
	connAttempts *sync.Map // map[string]int
	connFactory  *ConnectionFactory
	ctx          context.Context
	cancel       context.CancelFunc

	msgProcessor *MessageProcessor
	processLock  sync.Mutex
	processMap   *sync.Map // nodeID -> bool (показывает, запущена ли обработка для данного соединения)
}

type NodeDB interface {
	ListNodes(context.Context, nodestorage.NodeFilter) ([]nodestorage.Node, error)
	GetNode(context.Context, string) (nodestorage.Node, error)
	GetNodeEndpoints(context.Context, string) ([]nodestorage.Endpoint, error)
	UpdateEndpointStats(ctx context.Context, endpointID int64, isSuccess bool) error
}
