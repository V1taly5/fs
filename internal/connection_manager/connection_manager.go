package connectionmanager

import (
	"context"
	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
	fsv1 "fs/proto/gen/go"
	"log/slog"
	"sync"
	"time"
)

func NewConnectionManager(
	ctx context.Context,
	node *node.Node,
	db NodeDB,
	config *ConnectionConfig,
	log *slog.Logger,
) *ConnectionManager {
	ctx, cancel := context.WithCancel(ctx)

	manager := &ConnectionManager{
		node:         node,
		db:           db,
		log:          log,
		config:       config,
		activeConns:  &sync.Map{}, // nodeID -> Connection
		connAttempts: &sync.Map{}, // nodeID -> int
		connFactory:  NewConnectionFactory(node, log),
		ctx:          ctx,
		cancel:       cancel,

		processMap: &sync.Map{},
	}
	manager.msgProcessor = NewMessageProcessor(ctx, node, log)
	return manager
}

func (m *ConnectionManager) ConnectToEndpointWithAutoStartProcess(
	ctx context.Context,
	endpoint nodestorage.Endpoint,
	retryOptions *RetryOptions,
) (Connection, error) {
	conn, err := m.ConnectToEndpoint(ctx, endpoint, retryOptions)
	if err != nil {
		return nil, err
	}

	if m.config.AutoStartProcess {
		m.startProcessingForConnection(conn)
	}

	return conn, nil
}

func (m *ConnectionManager) ConnectToEndpoint(
	ctx context.Context,
	endpoint nodestorage.Endpoint,
	retryOptions *RetryOptions,
) (Connection, error) {
	op := "connection_manager.ConnectToEndpoint"
	log := m.log.With(slog.String("op", op))

	if conn, exists := m.GetActiveConnectionForNode(endpoint.NodeID); exists {
		log.Info("Found existing connection",
			slog.String("node_id", endpoint.NodeID),
			slog.String("protocol", string(endpoint.Protocol)),
			slog.String("adderss", conn.Address()),
		)
		return conn, nil
	}

	// создаем контекст с таймаутом, если он не задан в исходном
	connCtx := m.ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		connCtx, cancel = context.WithTimeout(ctx, m.config.ConnectionTimeout)
		defer cancel()
	}

	log.Info("Connecting to endpoint",
		slog.String("node_id", endpoint.NodeID),
		slog.String("protocol", string(endpoint.Protocol)),
		slog.String("adderss", endpoint.Address),
	)

	conn, err := m.connFactory.CreateConnection(connCtx, endpoint)
	if err != nil {
		// обнавляем статистиук в базе данных (неудачная попытка)
		// UpdateEndpointStats(endpoint.ID, false)
		m.db.UpdateEndpointStats(ctx, endpoint.ID, false)

		log.Error("Failed to connect to endpoint",
			slog.String("node_id", endpoint.NodeID),
			slog.String("protocol", string(endpoint.Protocol)),
			slog.String("address", endpoint.Address),
			slog.String("error", err.Error()))

		// если указаны опции повторных попыток, планируем повторное подключение
		if retryOptions != nil && retryOptions.MaxAttempts > 0 {
			attemptCountIface, _ := m.connAttempts.LoadOrStore(endpoint.NodeID, 0)
			attemptCount := attemptCountIface.(int)

			if attemptCount < retryOptions.MaxAttempts {
				m.connAttempts.Store(endpoint.NodeID, attemptCount+1)

				// расчет задержки
				delay := retryOptions.InitialDelay
				if retryOptions.UseExponential {
					delay = calculateExponentialBackoff(retryOptions.InitialDelay, attemptCount, retryOptions.MaxDelay)
				}

				log.Info("Scheduling retry",
					slog.String("node_id", endpoint.NodeID),
					slog.Float64("delay_seconds", delay.Seconds()),
					slog.Int("attempt", attemptCount+1),
					slog.Int("max_attempts", retryOptions.MaxAttempts),
				)

				// запускаем повторную попытку в горутине
				go m.scheduleRetryConnection(ctx, endpoint, delay, retryOptions)
			} else {
				// сбрасываем счетчик попыток
				m.connAttempts.Delete(endpoint.NodeID)
				log.Info("Max retry attempts reached",
					slog.String("node_id", endpoint.NodeID),
					slog.Int("max_attempts", retryOptions.MaxAttempts),
				)
			}
		}
		return nil, err
	}
	// соединение успешно
	// обновляем статистику в бд (успешная попытка)
	m.db.UpdateEndpointStats(ctx, endpoint.ID, true)

	// сбарсываем счетчик попыток
	m.connAttempts.Delete(endpoint.NodeID)

	m.activeConns.Store(endpoint.NodeID, conn)

	log.Info("Successfully connected to endpoint",
		slog.String("node_id", endpoint.NodeID),
		slog.String("protocol", string(endpoint.Protocol)),
		slog.String("address", endpoint.Address),
	)
	return conn, nil
}

// GetActiveConnectionForNode возвращает активное соединение с узлом
func (m *ConnectionManager) GetActiveConnectionForNode(nodeID string) (Connection, bool) {
	connIface, exists := m.activeConns.Load(nodeID)
	if !exists {
		return nil, false
	}

	conn := connIface.(Connection)

	// проверка соединения на активность
	if !conn.IsActive() {
		m.activeConns.Delete(nodeID)
		return nil, false
	}

	return conn, true
}

// GetActiveConnection возвращает slice всех активных соединений
func (m *ConnectionManager) GetActiveConnection() []Connection {
	connections := []Connection{}

	m.activeConns.Range(func(key, value interface{}) bool {
		conn := value.(Connection)
		if conn.IsActive() {
			connections = append(connections, conn)
		} else {
			// удаляем неактивные
			m.activeConns.Delete(key)
		}
		return true
	})
	return connections
}

func (m *ConnectionManager) scheduleRetryConnection(
	ctx context.Context,
	endpoint nodestorage.Endpoint,
	delay time.Duration,
	retryOpt *RetryOptions,
) {
	// создаем новый контекст тк передаваемый может быть отменен
	newCtx, cancel := context.WithTimeout(m.ctx, m.config.ConnectionTimeout)
	defer cancel()

	// ждем
	select {
	case <-time.After(delay):
		// продолжаем
	case <-ctx.Done():
		// исходный контекст был отменен
		return
	case <-newCtx.Done():
		// контекст менеджера был отменен
		return
	}

	newRetryOptions := &RetryOptions{
		MaxAttempts:    retryOpt.MaxAttempts - 1,
		UseExponential: retryOpt.UseExponential,
		InitialDelay:   retryOpt.InitialDelay,
		MaxDelay:       retryOpt.MaxDelay,
	}

	_, _ = m.ConnectToEndpoint(newCtx, endpoint, newRetryOptions)
}

// calculateExponentialBackoff вычисляет время задержки по exp алгоритму
func calculateExponentialBackoff(baseDelay time.Duration, attemt int, maxDelay time.Duration) time.Duration {
	multiplier := 1 << uint(attemt)
	delay := baseDelay * time.Duration(multiplier)

	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

func (m *ConnectionManager) InitMessageProcessor() {
	m.processLock.Lock()
	defer m.processLock.Unlock()

	if m.msgProcessor == nil {
		m.msgProcessor = NewMessageProcessor(m.ctx, m.node, m.log)
	}
}

func (m *ConnectionManager) RegisterMessageHandler(msgType string, handler MsgHandler) {
	m.processLock.Lock()
	defer m.processLock.Unlock()

	if m.msgProcessor == nil {
		m.msgProcessor = NewMessageProcessor(m.ctx, m.node, m.log)
	}

	m.msgProcessor.RegisterHandler(msgType, handler)
}

// Starts message processing for all connections
func (m *ConnectionManager) StartMessageProcessing() {
	m.processLock.Lock()
	defer m.processLock.Unlock()

	if m.msgProcessor == nil {
		m.msgProcessor = NewMessageProcessor(m.ctx, m.node, m.log)
	}

	// starting processing for all active connections
	connections := m.GetActiveConnection()
	for _, conn := range connections {
		m.startProcessingForConnection(conn)
	}

	m.log.Info("Message processing started for all active connections",
		slog.Int("connection_count", len(connections)))
}

// Starts message processing for one connection
func (m *ConnectionManager) startProcessingForConnection(conn Connection) {
	nodeID := conn.NodeID()

	if started, exists := m.processMap.LoadOrStore(nodeID, true); exists && started.(bool) {
		return
	}

	conn.StartReading(m.msgProcessor.ctx)

	go func() {
		m.msgProcessor.ProcessConnectionMessages(conn)
		m.processMap.Store(nodeID, false)
	}()

	m.log.Info("Started message processing for connection",
		slog.String("node_id", nodeID),
		slog.String("address", conn.Address()))
}

func (m *ConnectionManager) StopMessageProcessing() {
	m.processLock.Lock()
	defer m.processLock.Unlock()

	if m.msgProcessor != nil {
		m.msgProcessor.Stop()
		m.processMap = &sync.Map{}
		m.log.Info("Message processing stopped for all connections")
	}
}

func (m *ConnectionManager) Shutdown() {
	m.log.Info("Shutting down connection manager")

	m.StopMessageProcessing()

	m.activeConns.Range(func(key, value interface{}) bool {
		conn := value.(Connection)
		nodeID := key.(string)

		if err := conn.Close(); err != nil {
			m.log.Error("Error closing connection",
				slog.String("node_id", nodeID),
				slog.String("address", conn.Address()),
				slog.String("error", err.Error()))
		} else {
			m.log.Info("Connection closed",
				slog.String("node_id", nodeID),
				slog.String("address", conn.Address()))
		}

		m.activeConns.Delete(nodeID)
		return true
	})

	m.cancel()

	m.log.Info("Connection manager shutdown complete")
}

func (m *ConnectionManager) SendMessage(ctx context.Context, nodeID string, msg *fsv1.Message) error {
	connIface, exists := m.activeConns.Load(nodeID)
	if !exists {
		return ErrNoActiveConnection
	}

	conn := connIface.(Connection)
	if !conn.IsActive() {
		m.activeConns.Delete(nodeID)
		return ErrConnectionInactive
	}

	return conn.SendMessage(ctx, msg)
}
