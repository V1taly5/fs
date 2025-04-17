package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/shlex"
	"github.com/spf13/cobra"
)

type CommandPlugin interface {
	Meta() *cobra.Command
	Execute(ctx context.Context, cmd *cobra.Command, args []string) error
}

type CLI struct {
	rootCmd *cobra.Command
	plugins []CommandPlugin
	ctx     context.Context
}

func NewCLI(ctx context.Context) *CLI {
	return &CLI{
		rootCmd: &cobra.Command{
			Use:   "cli",
			Short: "CLI tool",
		},
		plugins: make([]CommandPlugin, 0, 10),
		ctx:     ctx,
	}
}

func (c *CLI) RunCLI() error {
	c.initCompletion()

	inputCh := make(chan string)

	// Запускаем обработчик ввода
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Println("Entering interactive mode. Type 'exit' to quit.")
		for {
			fmt.Print("mycli> ")
			if !scanner.Scan() {
				close(inputCh)
				return
			}
			inputCh <- scanner.Text()
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			fmt.Println("\nContext cancelled, shutting down CLI...")
			return nil
		case line, ok := <-inputCh:
			if !ok {
				return nil
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if line == "exit" {
				return nil
			}

			args, err := shlex.Split(line)
			if err != nil {
				fmt.Printf("Error parsing command: %v\n", err)
				continue
			}

			cmd := &cobra.Command{
				Use:          "mycli",
				SilenceUsage: true,
			}
			cmd.AddCommand(c.rootCmd.Commands()...)

			cmd.SetArgs(args)
			cmd.SetContext(c.ctx) // Передаем контекст в команду

			if err := cmd.Execute(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		}
	}
}

func (c *CLI) RegisterPlugin(p CommandPlugin) {
	c.plugins = append(c.plugins, p)
	cmd := p.Meta()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		for _, plugin := range c.plugins {
			if plugin.Meta() == cmd {
				return plugin.Execute(cmd.Context(), cmd, args)
			}
		}
		return fmt.Errorf("unknown command")
	}
	c.rootCmd.AddCommand(cmd)
}

func (c *CLI) initCompletion() {
	c.rootCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		names := make([]string, 0, 5)
		for _, plugin := range c.plugins {
			names = append(names, plugin.Meta().Name())
		}
		return names, cobra.ShellCompDirectiveNoFileComp

	}
	completionCmd := &cobra.Command{
		Use:   "completion",
		Short: "Generation completion script",
		Long:  "Generation comletion sctipt for bash, zsh, fish, powershell",
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := "bash"
			switch shell {
			case "bash":
				return c.rootCmd.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return c.rootCmd.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return c.rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return c.rootCmd.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell: %s", shell)
			}
		},
	}
	// source <(./mycli completion zsh)
	c.rootCmd.AddCommand(completionCmd)
}

func (c *CLI) Run() error {
	c.initCompletion()
	return c.rootCmd.Execute()
}
