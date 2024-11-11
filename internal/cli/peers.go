package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func createListPeersCommand(cmdContext *AppContext) *cobra.Command {
	var listPeersCmd = &cobra.Command{
		Use:   "list-peers",
		Short: "Отображает список обнаруженных пиров",
		Run: func(cmd *cobra.Command, args []string) {
			peers := cmdContext.Discoverer.GetDiscoveredPeers()
			if len(peers) == 0 {
				fmt.Println("Обнаруженных пиров нет.")
			} else {
				fmt.Println("Обнаруженные пиры:")
				for _, peer := range peers {
					fmt.Println(peer)
				}
			}
		},
	}
	return listPeersCmd
}

func createConnectPeerCommand(cmdContext *AppContext) *cobra.Command {
	var peerAddress string

	var connectPeerCmd = &cobra.Command{
		Use:   "connect-peer",
		Short: "Подключается к выбранному пиру",
		Run: func(cmd *cobra.Command, args []string) {
			if peerAddress == "" {
				fmt.Println("Укажите адрес пира с помощью флага --address.")
				return
			}
			ctx := context.Background()
			go cmdContext.Discoverer.ConnectToPeer(ctx, cmdContext.Node, peerAddress)
			fmt.Printf("Попытка подключения к пиру: %s\n", peerAddress)
		},
	}

	connectPeerCmd.Flags().StringVarP(&peerAddress, "address", "a", "", "Адрес пира для подключения")
	connectPeerCmd.MarkFlagRequired("address")

	return connectPeerCmd
}
