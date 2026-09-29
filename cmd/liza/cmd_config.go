package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

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
Supported keys: config.post_worktree_cmd, config.max_instances,
config.doer_max_wait and config.reviewer_max_wait.
The general state query remains available: %s config.post_worktree_cmd --json`, brand.Command("get"), brand.Command("get")),
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Read a supported config key",
	Long:  fmt.Sprintf("Read a supported config key. Equivalent to %s <key>, e.g. %s config.post_worktree_cmd; an unset post_worktree_cmd is null in JSON.", brand.Command("get"), brand.Command("get")),
	RunE: configRun(func(cmd *cobra.Command, args []string) error {
		if err := validateConfigArgs(args, 1); err != nil {
			return err
		}
		return getCmd.RunE(cmd, args)
	}),
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a supported config key",
	Long: fmt.Sprintf(`Store a supported project-scoped configuration value.

config.post_worktree_cmd stores a setup command without executing it.
Quote the command as one argument.
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

Agent-pool keys take a positive integer and are operator-only (an identified
agent is refused):
  config.max_instances      live agents per non-orchestrator role without its
                            own max-instances (minimum 1, default 3)
  config.doer_max_wait      seconds an idle doer waits before leaving the pool
  config.reviewer_max_wait  seconds an idle reviewer waits before leaving the
                            pool (minimum 300, default 600 for each wait)
Replacing a different value needs --replace and --reason, as above. A changed
wait applies to agents started afterwards; running ones keep theirs.

Examples:
  %s config.post_worktree_cmd "make setup"
  %s config.max_instances 5 --replace --reason "wider fan-out"`, brand.Command("config", "get"), brand.Command("get"), brand.Command("config", "set"), brand.Command("config", "set")),
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
		flagID, _ := cmd.Flags().GetString("agent-id")
		agentID, err := identity.Resolve(identity.Config{FlagValue: flagID})
		if err != nil {
			return err
		}
		if args[0] != ops.PostWorktreeConfigKey {
			return setPoolConfig(cmd, projectRoot, agentID, args, replace, reason)
		}
		input := ops.SetPostWorktreeCmdInput{Command: args[1], Replace: replace, Reason: reason}
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

// setPoolConfig writes an agent-pool key on the operator path only.
func setPoolConfig(cmd *cobra.Command, projectRoot, agentID string, args []string, replace bool, reason string) error {
	if agentID != "" {
		return cliValidationError(fmt.Sprintf("%s is operator-only; agent %s cannot set it", args[0], agentID))
	}
	value, err := strconv.Atoi(args[1])
	if err != nil {
		return cliValidationError(fmt.Sprintf("%s takes an integer value, got %q", args[0], args[1]))
	}
	result, err := ops.SetPoolConfig(projectRoot, ops.SetPoolConfigInput{Key: args[0], Value: value, Replace: replace, Reason: reason})
	if isJSON(cmd) {
		return jsonout.WriteResult(os.Stdout, result, nil, err)
	}
	if err != nil {
		return err
	}
	cmd.Printf("%s: %s\n", result.Key, result.Outcome)
	return nil
}

func supportedConfigKeys() []string {
	return append([]string{ops.PostWorktreeConfigKey}, ops.PoolConfigKeys...)
}

func validateConfigArgs(args []string, count int) error {
	keys := supportedConfigKeys()
	if len(args) != count {
		return cliValidationError(fmt.Sprintf("expected %d arguments; supported keys: %s", count, strings.Join(keys, ", ")))
	}
	if !slices.Contains(keys, args[0]) {
		return cliValidationError("unsupported config key; supported keys: " + strings.Join(keys, ", "))
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
