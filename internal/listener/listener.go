package listener

import (
	"bufio"
	"context"
	"fmt"
	"fs/internal/node"
	"fs/internal/util/logger/sl"
	"log/slog"
	"net"
	"time"

	"github.com/pion/transport/v3"
	"github.com/pion/turn/v4"
)

func StartListener(ctx context.Context, node *node.Node, port int, log *slog.Logger) {
	const op = "listener.StartListener"
	log = log.With(slog.String("op", op))

	// Validate port range
	if port <= 0 || port > 65535 {
		port = 35035
	}

	service := fmt.Sprintf("0.0.0.0:%v", port)

	// Resolve TCP address
	tcpAddr, err := net.ResolveTCPAddr("tcp", service)
	if err != nil {
		log.Error("Failed to resolve TCP address", sl.Err(err))
		return
	}

	// Start listening on the resolved address
	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Error("Failed to start TCP listener", slog.String("address", tcpAddr.String()), sl.Err(err))
		return
	}
	defer listener.Close()

	log.Info("Service started",
		slog.String("TCPAddr", tcpAddr.String()),
	)

	for {
		select {
		case <-ctx.Done():
			log.Info("Shutting down listener")
			return
		default:
			listener.SetDeadline(time.Now().Add(1 * time.Second))
			conn, err := listener.Accept()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue // продолжаем слушать, если это ошибка таймаута
				}
				log.Warn("Error accepting connection", sl.Err(err))
				continue
			}

			go func() {
				handleConnection(ctx, conn, node, log)
			}()
		}
	}

	// for {
	// 	conn, err := listener.Accept()
	// 	if err != nil {
	// 		log.Warn("Error accepting connection", sl.Err(err))
	// 		continue
	// 	}
	// 	go onConection(conn, node, log)
	// }
}

type relayConnection interface {
	AcceptTCP() (net.PacketConn, error)
}

func StartListenerWithTURN(ctx context.Context, node *node.Node, client *turn.Client, log *slog.Logger) {
	const op = "listener.StartListenerWithTURN"
	log = log.With(slog.String("op", op))

	relayConn, err := client.AllocateTCP()
	if err != nil {
		log.Error("Не удалось выделить ретранслируемое соединение: %v", err)
	}
	defer func() {
		if closeErr := relayConn.Close(); closeErr != nil {
		}
	}()

	log.Info("Сервис запущен через ретрансляцию TURN",
		slog.String("RelayedAddr", relayConn.Addr().String()),
	)

	for {
		select {
		case <-ctx.Done():
			log.Info("Завершение работы слушателя через ретрансляцию")
			return
		default:
			conn, err := relayConn.AcceptTCP()
			if err != nil {
				log.Error("Failed to accept TCP connection: %s", err)
			}

			go func() {
				// Передаем relayConn как обычное соединение с пиром
				// handleConnection(ctx, conn, node, log)
				handleConnectionWithTURN(ctx, conn, node, log)
			}()
		}
	}
}

func handleConnectionWithTURN(ctx context.Context, conn transport.TCPConn, n *node.Node, log *slog.Logger) {
	const op = "listener.handleConnectionWithTURN"
	log = log.With(slog.String("op", op))

	defer func() {
		conn.Close()
		log.Info("Connection closed", slog.String("RemoteAddr", conn.RemoteAddr().String()))
	}()

	log.Info("New connection established",
		slog.String("RemoteAddr", conn.RemoteAddr().String()),
	)

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	readWriter := bufio.NewReadWriter(reader, writer)

	// Здесь также можно использовать Peek или другие методы работы с буфером
	peer := node.NewPeer(conn) // Используем conn как транспорт для node.Peer
	n.HandleNode(ctx, readWriter, peer)
}

func handleConnection(ctx context.Context, conn net.Conn, n *node.Node, log *slog.Logger) {
	const op = "listener.handleConnection"
	log = log.With(slog.String("op", op))

	defer func() {
		conn.Close()
		log.Info("Connection closed", slog.String("RemoteAddr", conn.RemoteAddr().String()))
	}()

	log.Info("New connection established",
		slog.String("RemoteAddr", conn.RemoteAddr().String()),
	)

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	readWriter := bufio.NewReadWriter(reader, writer)

	//	buf, err := readWriter.Peek(4)
	//	if err != nil {
	//		if err != io.EOF {
	//			log.Default().Printf("Read peak ERROR: %s", err)
	//		}
	//		return
	//	}
	peer := node.NewPeer(conn)
	n.HandleNode(ctx, readWriter, peer)
}
