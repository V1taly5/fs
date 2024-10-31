package clihandlers

import (
	"fmt"
	"fs/internal/node"
)

// HandleEcho обрабатывает команду echo
func HandleEcho(message string, n *node.Node) {
	peers := n.Peers.Gets()
	for key := range peers {
		fmt.Println([]byte(key))
		peer, found := n.Peers.Get(key)
		if found == true {
			n.SendMessage(peer, message)
			fmt.Printf("CLI отправляет сообщение: %s\n", message)
		} else {
			fmt.Println("Пир для отправки сообщения не найден")
		}
	}
}

// HandleEcho обрабатывает команду echo
func HandleSendFile(path string, n *node.Node) {
	// ctx := context.Background()
	peers := n.Peers.Gets()
	for key := range peers {
		fmt.Println([]byte(key))
		peer, found := n.Peers.Get(key)
		if found == true {
			n.SendFile(peer, path)
			fmt.Printf("CLI отправляет файл: %s\n", path)
		} else {
			fmt.Println("Пир для отправки файла не найден")
		}
	}
}
