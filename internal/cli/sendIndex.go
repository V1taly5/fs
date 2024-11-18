package cli

import (
	handlers "fs/internal/cliHandlers"

	"github.com/spf13/cobra"
)

func createIndexSendingCommand(cmdContext *AppContext) *cobra.Command {
	// var path string

	var sendIndexCmd = &cobra.Command{
		Use:   "sendIndex",
		Short: "Отправляет индексы через CLI",
		Run: func(cmd *cobra.Command, args []string) {
			// fmt.Println("Путь:", path)
			// Вызов обработчика, передаем метод
			handlers.HandleSendIndex(cmdContext.Node)
		},
	}

	// echoCmd.Flags().StringVarP(&path, "Path", "p", "", "Путь к файлу для отправки")
	// echoCmd.MarkFlagRequired("Path")

	return sendIndexCmd
}
