package handlers

import (
	"fmt"
	"fs/node"
	"net"
)

// HandleEcho обрабатывает команду echo
func HandleEcho(message string, n *node.Node) {
	conn, err := net.Dial("tcp", "0.0.0.0:10002")
	if err != nil {
		// handle errorgo
	}
	peer := node.NewPeer(conn)
	n.SendName(peer)
	fmt.Printf("CLI отправляет сообщение: %s\n", message)
}
