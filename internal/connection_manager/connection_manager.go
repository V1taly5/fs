package connectionmanager

import (
	"context"
	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
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
		activeConns:  &sync.Map{},
		connAttempts: &sync.Map{},
		connFactory:  NewConnectionFactory(node, log),
		ctx:          ctx,
		cancel:       cancel,
	}

	return manager
}

func (m *ConnectionManager) ConnectToEndpoint(
	ctx context.Context,
	endpoint nodestorage.Endpoint,
	retryOptions *RetryOptions,
) (Connection, error) {
	op := "connecton_manager.ConnectToEndpoint"
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
	// UpdateEndpointStats(endpoint.ID, true)

	// обновление timestamp последнего взаимодействия с узлом
	// UpdateNodeLastSeen(endpoint.NodeID)

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
