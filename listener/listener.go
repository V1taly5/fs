package listener

import (
	"bufio"
	"fmt"
	"fs/node"
	"log"
	"net"
	"os"
)

func StartListener(node *node.Node, port int) {
	if port <= 0 || port > 65535 {
		port = 35035
	}

	service := fmt.Sprintf("0.0.0.0:%v", port)

	tcpAddr, err := net.ResolveTCPAddr("tcp", service)
	if err != nil {
		log.Default().Printf("ResolveTCPAddr: %s", err.Error())
		os.Exit(1)
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Default().Printf("ListenTCP: %s", err.Error())
		os.Exit(1)
	}
	fmt.Printf("\n\tService start on %s\n\n", tcpAddr.String())
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go onConection(conn, node)
	}
}

func onConection(conn net.Conn, n *node.Node) {
	defer func() {
		conn.Close()
	}()

	log.Default().Printf("New connection from: %v", conn.RemoteAddr().String())

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
