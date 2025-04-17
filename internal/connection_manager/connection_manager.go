package connectionmanager

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
	"fs/internal/util/logger/sl"
	fsv1 "fs/proto/gen/go"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

func NewConnectionManager(
	ctx context.Context,
	node *node.Node,
	db NodeDB,
	config *ConnectionConfig,
	log *slog.Logger,
	port int,
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
	manager.RegisterMessageHandler("ping", handlePing)
	service := fmt.Sprintf("0.0.0.0:%v", port)
	manager.StartTCPListenrer(ctx, service)

	return manager
}

// InitMessageProcessor инициализирует обработчик сообщений
func (m *ConnectionManager) InitMessageProcessor() {
	m.processLock.Lock()
	defer m.processLock.Unlock()

	if m.msgProcessor == nil {
		m.msgProcessor = NewMessageProcessor(m.ctx, m.node, m.log)
	}
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
		m.StartProcessingForConnection(conn)
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

	// TODY dirty
	node, err := m.db.GetNode(ctx, endpoint.NodeID)
	if err != nil {
		return nil, err
	}

	conn, err := m.connFactory.CreateConnection(connCtx, endpoint, node.PublicKey)
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

func (m *ConnectionManager) GetNodeEndpoints(ctx context.Context, nodeID string) ([]nodestorage.Endpoint, error) {
	endpoints, err := m.db.GetNodeEndpoints(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return endpoints, nil
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

// func (m *ConnectionManager) InitMessageProcessor() {
// 	m.processLock.Lock()
// 	defer m.processLock.Unlock()

// 	if m.msgProcessor == nil {
// 		m.msgProcessor = NewMessageProcessor(m.ctx, m.node, m.log)
// 	}
// }

// !!
func (m *ConnectionManager) RegisterMessageHandler(msgType string, handler MsgHandler) {
	m.processLock.Lock()
	defer m.processLock.Unlock()

	if m.msgProcessor == nil {
		m.msgProcessor = NewMessageProcessor(m.ctx, m.node, m.log)
	}

	m.msgProcessor.RegisterHandler(msgType, handler)
}

// Starts message processing for all connections
// !!
func (m *ConnectionManager) StartMessageProcessing() {
	m.processLock.Lock()
	defer m.processLock.Unlock()

	if m.msgProcessor == nil {
		m.msgProcessor = NewMessageProcessor(m.ctx, m.node, m.log)
	}

	// starting processing for all active connections
	connections := m.GetActiveConnection()
	for _, conn := range connections {
		m.StartProcessingForConnection(conn)
	}

	m.log.Info("Message processing started for all active connections",
		slog.Int("connection_count", len(connections)))
}

// Starts message processing for one connection
// !!
func (m *ConnectionManager) StartProcessingForConnection(conn Connection) {
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

// !!
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

func (c *ConnectionManager) StartTCPListenrer(ctx context.Context, listenAddr string) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		c.log.Error("failed start tcp listener")
		return err
	}

	c.log.Info("listening for imcoming connections", slog.String("at the Addres", listenAddr))

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					c.log.Error("connection acceptance error ")
					continue
				}
			}
			go c.handleIncomingTCPConnection(ctx, conn)
		}
	}()
	return nil
}

func (c *ConnectionManager) handleIncomingTCPConnection(ctx context.Context, conn net.Conn) {

	remotePublicKey := make([]byte, ed25519.PublicKeySize)
	_, err := io.ReadFull(conn, remotePublicKey)
	if err != nil {
		c.log.Error("Ошибка при чтении публичного ключа", sl.Err(err))
		conn.Close()
		return
	}

	// Преобразование байтов в ed25519.PublicKey
	if len(remotePublicKey) != ed25519.PublicKeySize {
		c.log.Error("Неверный размер публичного ключа")
		conn.Close()
		return
	}
	remotePubKey := ed25519.PublicKey(remotePublicKey)

	// read client's ephemeral public key and signature
	clientEphemeralPub := make([]byte, 32)
	clientSignature := make([]byte, ed25519.SignatureSize)
	if _, err := io.ReadFull(conn, clientEphemeralPub); err != nil {
		c.log.Error("failed to read client ephemeral key", sl.Err(err))
		conn.Close()
		return
	}
	if _, err := io.ReadFull(conn, clientSignature); err != nil {
		c.log.Error("failed to read client signature", sl.Err(err))
		conn.Close()
		return
	}

	// verify client's signature
	if !ed25519.Verify(remotePublicKey, clientEphemeralPub, clientSignature) { // Нужно получить публичный ключ клиента из БД
		c.log.Error("client signature verification failed")
		conn.Close()
		return
	}

	// generate server ephemeral key pair
	var serverEphemeralPriv [32]byte
	if _, err := io.ReadFull(rand.Reader, serverEphemeralPriv[:]); err != nil {
		c.log.Error("failed to generate ephemeral private key", sl.Err(err))
		conn.Close()
		return
	}

	var serverEphemeralPub [32]byte
	curve25519.ScalarBaseMult(&serverEphemeralPub, &serverEphemeralPriv)
	// serverEphemeralPub, serverEphemeralPriv, err := x25519.GenerateKey(rand.Reader)
	if err != nil {
		c.log.Error("failed to generate server ephemeral key", sl.Err(err))
		conn.Close()
		return
	}

	// sign and send server's ephemeral key
	signature := ed25519.Sign(c.node.PrivKey, serverEphemeralPub[:])
	if _, err := conn.Write(append(serverEphemeralPub[:], signature...)); err != nil {
		c.log.Error("Failed to send server handshake data", sl.Err(err))
		conn.Close()
		return
	}

	// compute shared secret
	var clientEphemeralPubKey [32]byte
	copy(clientEphemeralPubKey[:], clientEphemeralPub)

	sharedSecret, err := curve25519.X25519(serverEphemeralPriv[:], clientEphemeralPubKey[:])
	// sharedSecret, err := serverEphemeralPriv.SharedKey(clientEphemeralPub)
	if err != nil {
		c.log.Error("Failed to compute shared secret", sl.Err(err))
		conn.Close()
		return
	}

	// derive session keys
	hkdf := hkdf.New(sha256.New, sharedSecret, nil, []byte("p2p_session_keys_v1"))
	clientKey := make([]byte, 32)
	serverKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdf, clientKey); err != nil {
		c.log.Error("Key derivation failed", sl.Err(err))
		conn.Close()
		return
	}
	if _, err := io.ReadFull(hkdf, serverKey); err != nil {
		c.log.Error("Key derivation failed", sl.Err(err))
		conn.Close()
		return
	}

	// initialize connection
	// connInstance := NewTCPConnection(conn, remotePubKey)
	// connInstance.SetSessionKeys(serverKey, clientKey) // Обратный порядок
	// c.activeConns.Store(connInstance.NodeID(), connInstance)

	connInstance, err := c.NewTCPConnectionFromIncoming(conn, remotePubKey)
	if err != nil {
		c.log.Error("Не удалось создать экземпляр Connection", sl.Err(err))
		conn.Close()
		return
	}
	connInstance.SetSessionKeys(serverKey, clientKey)

	// Добавляем активное соединение в мапу
	c.activeConns.Store(connInstance.NodeID(), connInstance)
	c.StartProcessingForConnection(connInstance)
	c.log.Info("Соединение установлено и зарегистрировано: ", slog.String("Addr", connInstance.Address()))
}

func (c *ConnectionManager) NewTCPConnectionFromIncoming(conn net.Conn, remotePubKey ed25519.PublicKey) (Connection, error) {
	remoteAddr := conn.RemoteAddr().String()
	_, portStr, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return nil, err
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	// Формирование структуры Endpoint. Поле NodeID заполним позже, после handshake.
	endpoint := nodestorage.Endpoint{
		NodeID:   string(remotePubKey),
		Address:  remoteAddr,
		Port:     port,
		Protocol: nodestorage.ProtocolTCP,
		Source:   "local",
		// Остальные поля можно оставить с значениями по умолчанию или задать здесь
		Priority: 1,
	}

	// Создаём TCPConnection с использованием фабричного метода.
	tcpConn := NewTCPConnection(conn, endpoint)
	return tcpConn, nil
}
