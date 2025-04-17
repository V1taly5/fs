package cliplugins

import (
	"context"
	"fmt"
	connectionmanager "fs/internal/connection_manager"
	fsv1 "fs/proto/gen/go"
	"time"

	"github.com/spf13/cobra"
)

type ConnectionCommand struct {
	cmd     *cobra.Command
	connMng *connectionmanager.ConnectionManager
}

func NewConnectionCommand(connMng *connectionmanager.ConnectionManager) *ConnectionCommand {
	return &ConnectionCommand{
		connMng: connMng,
	}
}

func (c *ConnectionCommand) Meta() *cobra.Command {
	if c.cmd != nil {
		return c.cmd
	}
	c.cmd = &cobra.Command{
		Use:   "Connection",
		Short: "Interacts with the connection manager",
		Long:  "Interacts with the connection manager to initiate...",
		Annotations: map[string]string{
			cobra.BashCompOneRequiredFlag: "true",
		},
	}
	c.cmd.Flags().StringP("idnode", "i", "", "Node ID for conneciton")
	c.cmd.RegisterFlagCompletionFunc("idnode", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		nodeIDs := make([]string, 0, 5)
		nodeIDs = append(nodeIDs, "d9c8327530a1527c5eb755f0c92bfa693012b8f7a7737594f92f647e4dd28427")
		// TODO: получить nodeIDs из бд

		// Endpoint := c.connMng.GetEndpoint(id)
		// for _, connection := range connections {
		// 	nodeIDs = append(nodeIDs, connection.NodeID())
		// }
		return nodeIDs, cobra.ShellCompDirectiveNoFileComp
	})
	return c.cmd
}

func (c *ConnectionCommand) Execute(ctx context.Context, cmd *cobra.Command, args []string) error {
	nodeID, err := cmd.Flags().GetString("idnode")
	if err != nil || nodeID == "" {
		return fmt.Errorf("flag --idnode is required")
	}
	endpoints, err := c.connMng.GetNodeEndpoints(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for nodeID: %s", nodeID)
	}

	retryOptions := &connectionmanager.RetryOptions{
		MaxAttempts:    3,
		UseExponential: true,
		InitialDelay:   time.Second,
		MaxDelay:       time.Second * 10,
	}
	if len(endpoints) == 0 {
		return nil
	}
	_, err = c.connMng.ConnectToEndpointWithAutoStartProcess(ctx, endpoints[0], retryOptions)
	// _, err = c.connMng.ConnectToEndpoint(ctx, endpoints[0], retryOptions)
	if err != nil {
		return fmt.Errorf("failed to create Conneciton to node endpoint")
	}

	ping := &fsv1.PingMSG{
		SeqNum: 54,
	}
	// Создаем Message с payload типа PingMSG
	message := &fsv1.Message{
		MassageId:  uint64(time.Now().UnixNano()),                                      // Уникальный ID сообщения
		Timestamp:  uint64(time.Now().Unix()),                                          // Текущее время
		SenderId:   "50e209477ff4553500fbe1c7eb5742c09d1ea66b5eefe23ea4504a65028621d5", // ID отправителя
		ReceiverId: "efb7604e4419997e37e64a90385b88419858e003c66c82f8f5a7e62a618672b8", // ID получателя
		Payload:    &fsv1.Message_Ping{Ping: ping},                                     // Устанавливаем payload
	}

	err = c.connMng.SendMessage(ctx, nodeID, message)
	if err != nil {
		return fmt.Errorf("failed to send msg")
	}
	return nil
}
