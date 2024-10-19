package cli

import (
	"bufio"
	"fmt"
	"fs/node"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// AppContext хранит зависимости, которые будут использоваться в командах CLI
type AppContext struct {
	Node *node.Node
}

func NewAppContext(node *node.Node) *AppContext {
	return &AppContext{
		Node: node,
	}
}

// AttachCommand добавляет команды к корневой команде
func AttachCommand(cmd *cobra.Command) {
	rootCmd.AddCommand(cmd)
}

func CliStart(args []string, ctx *AppContext) {
	AttachCommand(createEchoCommand(ctx))

	if len(args) > 1 {
		if err := Execute(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	} else {
		reader := bufio.NewReader(os.Stdin)
		fmt.Println("Введите команды для взаимодействия:")
		for {
			fmt.Print("> ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			if input == "" {
				continue
			}

			// Разделяем команду и аргументы
			parts := strings.Split(input, " ")
			os.Args = append([]string{os.Args[0]}, parts...)
			if err := Execute(); err != nil {
				fmt.Println(err)
			}
		}
	}
}

var rootCmd = &cobra.Command{
	Use:   "service-cli",
	Short: "CLI для взаимодействия c сервисом",
	Long:  `Это CLI-интерфейс для взаимодействия с сервисом через команды.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Введите команду для взаимодействия с основным сервисом.")
	},
}

// Execute запускает корневую команду
func Execute() error {
	return rootCmd.Execute()
}
