package listener

import (
	"bufio"
	"fmt"
	"fs/internal/util/logger/sl"
	"fs/node"
	"log/slog"
	"net"
	"os"
)

func StartListener(node *node.Node, port int, log *slog.Logger) {
	const op = "listener.StartListener"
	log = log.With(slog.String("op", op))

	if port <= 0 || port > 65535 {
		port = 35035
	}

	service := fmt.Sprintf("0.0.0.0:%v", port)

	tcpAddr, err := net.ResolveTCPAddr("tcp", service)
	if err != nil {
		log.Info("ResolveTCPAddr", sl.Err(err))
		os.Exit(1)
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Info("ListenTCP", sl.Err(err))
		os.Exit(1)
	}
	log.Info("Service start on",
		slog.String("TCPAddr", tcpAddr.String()),
	)
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go onConection(conn, node, log)
	}
}

func onConection(conn net.Conn, n *node.Node, log *slog.Logger) {
	const op = "listener.onConnection"
	log = log.With(slog.String("op", op))

	defer func() {
		conn.Close()
	}()
	log.Info("New connection from",
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
	n.HandleNode(readWriter, peer)
}
