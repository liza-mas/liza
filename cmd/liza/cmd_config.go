package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Read and update supported runtime configuration",
	Long: fmt.Sprintf(`Read and update runtime configuration using the same dotted keys as %s.
Currently supports config.post_worktree_cmd only.
The general state query remains available: %s config.post_worktree_cmd --json`, brand.Command("get"), brand.Command("get")),
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Read config.post_worktree_cmd",
	Long:  fmt.Sprintf("Read config.post_worktree_cmd. Equivalent to %s config.post_worktree_cmd; an unset value is null in JSON.", brand.Command("get")),
	RunE: configRun(func(cmd *cobra.Command, args []string) error {
		if err := validateConfigArgs(args, 1); err != nil {
			return err
		}
		return getCmd.RunE(cmd, args)
	}),
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <command>",
	Short: "Set config.post_worktree_cmd for subsequent worktree setup",
	Long: fmt.Sprintf(`Store a project-scoped setup command without executing it.
Only config.post_worktree_cmd is supported. Quote the command as one argument.
An identical value is a successful no-op. Replacing a different value requires
--replace and --reason. These flags prevent accidental replacement; they do not
authenticate a human caller. Existing provider sessions are not restarted.

Read the current value with %s config.post_worktree_cmd --json
or %s config.post_worktree_cmd --json.

With an agent ID (flag or environment), the role must allow
config-set-post-worktree-cmd and the current registration generation is required.
No default agent role has this capability. Without an ID this is an operator write.
Audit records are emitted to the process log (stderr), including in JSON mode;
they are not persisted in state.yaml or the activity log.

Example:
  %s config.post_worktree_cmd "make setup"`, brand.Command("config", "get"), brand.Command("get"), brand.Command("config", "set")),
	RunE: configRun(func(cmd *cobra.Command, args []string) error {
		if err := validateConfigArgs(args, 2); err != nil {
			return err
		}
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		replace, _ := cmd.Flags().GetBool("replace")
		reason, _ := cmd.Flags().GetString("reason")
		input := ops.SetPostWorktreeCmdInput{Command: args[1], Replace: replace, Reason: reason}
		flagID, _ := cmd.Flags().GetString("agent-id")
		agentID, err := identity.Resolve(identity.Config{FlagValue: flagID})
		if err != nil {
			return err
		}
		if agentID != "" {
			authority, err := requireAgentAuthorityForID(cmd, agentID)
			if err != nil {
				return err
			}
			resolver, err := loadResolverForRBAC(projectRoot)
			if err != nil {
				return err
			}
			if err := validateAllowedOperation(resolver, agentID, "config-set-post-worktree-cmd"); err != nil {
				return err
			}
			input.Authority = &authority
		}
		result, err := ops.SetPostWorktreeCmd(projectRoot, input)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return err
		}
		cmd.Printf("%s: %s\n", result.Key, result.Outcome)
		return nil
	}),
}

// configRun preserves a single JSON error envelope for admission errors too.
// Unlike commands that suppress logs in JSON mode, config set keeps its audit
// on stderr; stdout remains the machine-readable envelope only.
func configRun(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		if err != nil && isJSON(cmd) && !errors.Is(err, jsonout.ErrAlreadyWritten) {
			return jsonout.WriteResult(os.Stdout, nil, nil, err)
		}
		return err
	}
}

func validateConfigArgs(args []string, count int) error {
	if len(args) != count {
		return cliValidationError(fmt.Sprintf("expected %d arguments; use config.post_worktree_cmd as the key", count))
	}
	if args[0] != ops.PostWorktreeConfigKey {
		return cliValidationError("unsupported config key; supported key: " + ops.PostWorktreeConfigKey)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configGetCmd, configSetCmd)
	for _, cmd := range []*cobra.Command{configGetCmd, configSetCmd} {
		addJSONFlag(cmd)
		cmd.ValidArgsFunction = completeConfigKeys
	}
	configGetCmd.Flags().String("format", "value", "Output format: json, yaml, table, value")
	configSetCmd.Flags().Bool("replace", false, "Replace a different existing value (requires --reason)")
	configSetCmd.Flags().String("reason", "", "Reason for the configuration change")
	addAgentIDFlag(configSetCmd)
	registerCompletion(configSetCmd, "agent-id", completeAgentIDs)
}
