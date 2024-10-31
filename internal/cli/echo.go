package cli

import (
	"fmt"
	handlers "fs/internal/cliHandlers"

	"github.com/spf13/cobra"
)

// createEchoCommand создает команду echo и передает метод для вызова в handler
func createEchoCommand(cmdContext *AppContext) *cobra.Command {
	var message string

	var echoCmd = &cobra.Command{
		Use:   "echo",
		Short: "Отправляет сообщение через CLI и вызывает переданный метод",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Сообщение:", message)
			// Вызов обработчика, передаем метод
			handlers.HandleEcho(message, cmdContext.Node)
		},
	}

	echoCmd.Flags().StringVarP(&message, "message", "m", "", "Сообщение для отправки")
	echoCmd.MarkFlagRequired("message")

	return echoCmd
}
