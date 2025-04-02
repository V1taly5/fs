package connectionmanager

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
	"fs/internal/util/logger/sl"
	fsv1 "fs/proto/gen/go"
	"io"
	"log/slog"
	"net"
	"os"
	"time"

	"google.golang.org/protobuf/proto"
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

// TODO:
func (c *TCPConnector) performHandShake(ctx context.Context, conn net.Conn, endpoint nodestorage.Endpoint) (bool, error) {
	return true, nil
}

func (c *TCPConnector) SuportProtocol(protocol string) bool {
	return protocol == "tcp"
}

func (c *TCPConnector) Name() string {
	return "tcp"
}

var bufferSize = 5

// TCP Connection
type TCPConnection struct {
	conn        net.Conn
	endpoint    nodestorage.Endpoint
	active      bool
	messageChan chan *fsv1.Message
}

func NewTCPConnection(conn net.Conn, endpoint nodestorage.Endpoint) *TCPConnection {
	return &TCPConnection{
		conn:        conn,
		endpoint:    endpoint,
		active:      true,
		messageChan: make(chan *fsv1.Message, bufferSize),
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
	close(c.messageChan)
	return c.conn.Close()
}

func (c *TCPConnection) IsActive() bool {
	return c.active
}

func (c *TCPConnection) MessageChannel() <-chan *fsv1.Message {
	return c.messageChan
}

func (c *TCPConnection) SendMessage(ctx context.Context, msg *fsv1.Message) error {
	if !c.active {
		return errors.New("connection is not active")
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// добавить размер сообщение как префикс
	messageSize := uint32(len(data))
	sizeBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(sizeBuf, messageSize)

	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("failed to set write deadline: %w", err)
		}
		defer c.conn.SetWriteDeadline(time.Time{})
	}

	if _, err := c.conn.Write(sizeBuf); err != nil {
		c.active = false
		c.Close()
		return fmt.Errorf("failed to send message size: %w", err)
	}

	if _, err := c.conn.Write(data); err != nil {
		c.Close()
		return fmt.Errorf("failed to send message size: %w", err)
	}
	return nil
}

// io.ReadFull блокируется и если не поймать закрытие ctx в момент прохода этой проверки,
//
//	то функции не закончит свое выполнение, для того, что бы выйти из блокировки io.ReadFull надо закрыть
//
// net.Conn -> тогда произойдет разблокировка
func (c *TCPConnection) StartReading(ctx context.Context) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	go func() {
		defer c.Close()

		sizeBuf := make([]byte, 4)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !c.IsActive() {
				return
			}
			if _, err := io.ReadFull(c.conn, sizeBuf); err != nil {
				log.Error("failed to read message size", slog.String("from", c.Address()), sl.Err(err))
				return
			}

			messageSize := binary.BigEndian.Uint32(sizeBuf)
			if messageSize > 10*1024*1024 {
				log.Error("message size too large", slog.String("from", c.Address()), slog.Uint64("size", uint64(messageSize)))
				return
			}

			messageBuff := make([]byte, messageSize)
			if n, err := io.ReadFull(c.conn, messageBuff); err != nil {
				localLog := log.With(slog.String("from", c.Address()))
				if errors.Is(err, io.EOF) {
					localLog.Info("client has closed connection (EOF)")
					return
				} else if errors.Is(err, io.ErrUnexpectedEOF) {
					localLog.Info("fewer bytes were read than expected client may have closed the connection", slog.Int("bites read", n))
					return
				}
				localLog.Error("failed to read message data", sl.Err(err))
				return
			}

			msg := &fsv1.Message{}
			if err := proto.Unmarshal(messageBuff, msg); err != nil {
				log.Error("failed to unmarshal message", slog.String("from", c.Address()), sl.Err(err))
				continue
			}

			select {
			case c.messageChan <- msg:
			case <-time.After(DefaultMessageProcessTimeout):
				log.Warn("message channel full, dropping message", slog.String("from", c.Address()))
			}
		}
	}()
}
