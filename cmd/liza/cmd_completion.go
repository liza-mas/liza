package main

import (
	"fmt"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:       "completion [bash|zsh|fish|powershell]",
	Short:     "Generate shell completion script",
	Long:      fmt.Sprintf("Generate shell completion script for %s.", brand.BinaryName),
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletion(cmd.OutOrStdout())
		default:
			return cliValidationError("unsupported shell: " + args[0])
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
