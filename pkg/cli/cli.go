package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type CommandPlugin interface {
	Meta() *cobra.Command
	Execute(cmd *cobra.Command, args []string) error
}

type CLI struct {
	rootCmd *cobra.Command
	plugins []CommandPlugin
}

func NewCLI() *CLI {
	return &CLI{
		rootCmd: &cobra.Command{
			Use:   "cli",
			Short: "CLI tool",
		},
		plugins: make([]CommandPlugin, 0, 10),
	}
}

func (c *CLI) RegisterPlugin(p CommandPlugin) {
	c.plugins = append(c.plugins, p)
	cmd := p.Meta()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		for _, plugin := range c.plugins {
			if plugin.Meta() == cmd {
				return plugin.Execute(cmd, args)
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
