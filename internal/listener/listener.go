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
	"time"
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
	peer := peers.NewPeer(conn)
	n.HandleNode(ctx, readWriter, peer)
}
