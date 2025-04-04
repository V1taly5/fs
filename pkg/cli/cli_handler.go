package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type GreetCommand struct {
	cmd *cobra.Command
}

func (g *GreetCommand) Meta() *cobra.Command {
	if g.cmd == nil {
		g.cmd = &cobra.Command{
			Use:   "greet",
			Short: "Print greeting message",
			Long:  `Just say hello to someone by name.`,
			Annotations: map[string]string{
				cobra.BashCompOneRequiredFlag: "true",
			},
		}
		g.cmd.Flags().StringP("name", "n", "", "Name to greet (required)")
		g.cmd.RegisterFlagCompletionFunc("name", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			names := []string{"anton", "john", "mary", "bob", "teo"}
			return names, cobra.ShellCompDirectiveNoFileComp
		})
	}
	return g.cmd
}

func (g *GreetCommand) Execute(cmd *cobra.Command, args []string) error {
	name, err := cmd.Flags().GetString("name")
	if err != nil || name == "" {
		return fmt.Errorf("flag --name is required")
	}
	fmt.Printf("Hello, dear %s!\n", name)
	return nil
}
