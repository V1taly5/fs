package listener

import (
	"bufio"
	"context"
	"fmt"
	"fs/internal/node"
	"fs/internal/peers"
	"fs/internal/util/logger/sl"
	"log/slog"
	"net"
	"sync"
	"time"
)

const (
	defaultPort       = 35035
	maxConnections    = 100 // Максимальное число одновременных соединений
	connectionTimeout = 5 * time.Second
)

func StartListener(ctx context.Context, node *node.Node, port int, log *slog.Logger) {
	const op = "listener.StartListener"
	log = log.With(slog.String("op", op))

	// validate port range
	if port <= 0 || port > 65535 {
		port = defaultPort
	}

	service := fmt.Sprintf("0.0.0.0:%v", port)

	tcpAddr, err := net.ResolveTCPAddr("tcp", service)
	if err != nil {
		log.Error("Failed to resolve TCP address", sl.Err(err))
		return
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Error("Failed to start TCP listener", slog.String("address", tcpAddr.String()), sl.Err(err))
		return
	}
	defer listener.Close()

	log.Info("Service started", slog.String("TCPAddr", tcpAddr.String()))

	var wg sync.WaitGroup
	connLimiter := make(chan struct{}, maxConnections)

	for {
		select {
		case <-ctx.Done():
			log.Info("Shutting down listener")
			wg.Wait()
			return
		default:
			// listener.SetDeadline(time.Now().Add(1 * time.Second))
			if err := listener.SetDeadline(time.Now().Add(1 * time.Second)); err != nil {
				log.Warn("Failed to set deadline", sl.Err(err))
			}

			conn, err := listener.Accept()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue // продолжаем слушать, если это ошибка таймаута
				}
				log.Warn("Error accepting connection", sl.Err(err))
				continue
			}
			select {
			case connLimiter <- struct{}{}:
				wg.Add(1)
				go func() {
					handleConnection(ctx, conn, node, log)
					<-connLimiter
					wg.Done()
				}()
			default:
				log.Warn("Too many connections, rejecting new connection", slog.String("RemoteAddr", conn.RemoteAddr().String()))
				conn.Close()
			}

		}
	}
}

func handleConnection(ctx context.Context, conn net.Conn, n *node.Node, log *slog.Logger) {
	const op = "listener.handleConnection"
	log = log.With(slog.String("op", op), slog.String("RemoteAddr", conn.RemoteAddr().String()))

	defer func() {
		conn.Close()
		log.Info("Connection closed")
	}()

	log.Info("New connection established")

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	readWriter := bufio.NewReadWriter(reader, writer)

	peer := peers.NewPeer(conn)
	n.HandleNode(ctx, readWriter, peer)

	if err := writer.Flush(); err != nil {
		log.Warn("Failed to flush writer", sl.Err(err))
	}
}
