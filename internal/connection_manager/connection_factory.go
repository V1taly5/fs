package connectionmanager

import (
	"context"
	"errors"
	"fmt"
	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
	"log/slog"
	"net"
	"time"
)

func NewConnectionFactory(node *node.Node, log *slog.Logger) *ConnectionFactory {
	factory := &ConnectionFactory{
		connectors: make(map[string]Connector),
		node:       node,
		log:        log,
	}

	// регистрируем коннекторы
	factory.RegisterConnector(NewTCPConnector(node, 10*time.Second, log))
	return factory
}

func (f *ConnectionFactory) RegisterConnector(connector Connector) {
	op := "connection_manager.RegisterConnector"
	log := f.log.With(slog.String("op", op))

	f.connectors[connector.Name()] = connector
	log.Info("Register connector", slog.String("connector", connector.Name()))
}

func (f *ConnectionFactory) GetConnectorForProtocol(protocol string) (Connector, error) {
	for _, connector := range f.connectors {
		if connector.SuportProtocol(protocol) {
			return connector, nil
		}

	}
	return nil, fmt.Errorf("no connector found for protocol: %s", protocol)
}

func (f *ConnectionFactory) CreateConnection(ctx context.Context, endpoint nodestorage.Endpoint) (Connection, error) {
	connector, err := f.GetConnectorForProtocol(string(endpoint.Protocol))
	if err != nil {
		return nil, err
	}
	return connector.Connect(ctx, endpoint)
}

// TCP Connector

type TCPConnector struct {
	node    *node.Node
	timeout time.Duration
	log     *slog.Logger
}

func NewTCPConnector(node *node.Node, timeout time.Duration, log *slog.Logger) *TCPConnector {
	return &TCPConnector{
		node:    node,
		timeout: timeout,
		log:     log,
	}
}

func (c *TCPConnector) Connect(ctx context.Context, endpoint nodestorage.Endpoint) (Connection, error) {
	op := "connection_manager.TCPConnector.Connect"
	log := c.log.With(slog.String("op", op))
	// addr := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
	addr := endpoint.Address

	dialer := net.Dialer{Timeout: c.timeout}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp connection failed: %w", err)
	}

	// место для добавления аутентификации и рукопожатия
	authenticated, err := c.performHandShake(ctx, conn, endpoint)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake failed: %w", err)
	}

	if !authenticated {
		conn.Close()
		return nil, errors.New("authentication failed")
	}
	log.Info("TCP Connection established",
		slog.String("node_id", endpoint.NodeID),
		slog.String("adress", addr),
	)
	return NewTCPConnection(conn, endpoint), nil

}

func (c *TCPConnector) performHandShake(ctx context.Context, conn net.Conn, endpoint nodestorage.Endpoint) (bool, error) {
	return true, nil
}

func (c *TCPConnector) SuportProtocol(protocol string) bool {
	return protocol == "tcp"
}

func (c *TCPConnector) Name() string {
	return "tcp"
}

// TCP Connection

type TCPConnection struct {
	conn     net.Conn
	endpoint nodestorage.Endpoint
	active   bool
}

func NewTCPConnection(conn net.Conn, endpoint nodestorage.Endpoint) *TCPConnection {
	return &TCPConnection{
		conn:     conn,
		endpoint: endpoint,
		active:   true,
	}
}

func (c *TCPConnection) NodeID() string {
	return c.endpoint.NodeID
}

func (c *TCPConnection) Address() string {
	// return fmt.Sprintf("%s:%d", c.endpoint.Address, c.endpoint.Port)
	return c.endpoint.Address
}

func (c *TCPConnection) Protocol() string {
	return string(c.endpoint.Protocol)
}

func (c *TCPConnection) Close() error {
	if !c.active {
		return nil
	}
	c.active = false
	return c.conn.Close()
}

func (c *TCPConnection) IsActive() bool {
	return c.active
}
