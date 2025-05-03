package cliplugins

import (
	"context"
	"fmt"
	messagerouter "fs/internal/message_router"

	"github.com/spf13/cobra"
)

type SendlerCommand struct {
	cmd    *cobra.Command
	sender *messagerouter.MessageRouter
}

func NewSenderCommand(sender *messagerouter.MessageRouter) *SendlerCommand {
	return &SendlerCommand{sender: sender}
}

func (s *SendlerCommand) Meta() *cobra.Command {
	if s.cmd != nil {
		return s.cmd
	}
	s.cmd = &cobra.Command{
		Use:   "Send",
		Short: "Sends commands|info to the specified peers",
		Long:  "Sends commands|info to the specified peers...",
		Annotations: map[string]string{
			cobra.BashCompOneRequiredFlag: "true",
		},
	}
	s.cmd.Flags().StringP("node", "n", "", "node id for sending")
	s.cmd.Flags().BoolP("index", "i", false, "Send local indexes")
	return s.cmd
}

func (s *SendlerCommand) Execute(ctx context.Context, cmd *cobra.Command, args []string) error {
	node, err := cmd.Flags().GetString("node")
	if err != nil || node == "" {
		return fmt.Errorf("flag --node is required")
	}

	isIndex, err := cmd.Flags().GetBool("index")
	if err != nil {
		return fmt.Errorf("flag --isIndex failed")
	}
	if isIndex {
		if err := s.sender.SendIndexUpdate(ctx, node, true, 0); err != nil {
			return fmt.Errorf("failed sending indexes to the node: %s, err: %w", node, err)
		}
	}

	return nil
}
