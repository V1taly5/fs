package cli

import (
	"fmt"
	handlers "fs/internal/cliHandlers"

	"github.com/spf13/cobra"
)

func createFileSendingCommand(cmdContext *AppContext) *cobra.Command {
	var path string

	var echoCmd = &cobra.Command{
		Use:   "sendFile",
		Short: "Отправляет файл через CLI",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Путь:", path)
			// Вызов обработчика, передаем метод
			handlers.HandleSendFile(path, cmdContext.Node)
		},
	}

	echoCmd.Flags().StringVarP(&path, "Path", "p", "", "Путь к файлу для отправки")
	echoCmd.MarkFlagRequired("Path")

	return echoCmd
}
