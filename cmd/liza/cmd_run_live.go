package main

import (
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var runLiveCmd = &cobra.Command{
	Use:   "run-live --task <task-id> [--timeout <seconds>] -- <command> [args...]",
	Short: "Run a local live command with the task's reusable runtime inputs",
	Long: fmt.Sprintf(`Run one command, without a shell, in the current directory with the task's
reusable runtime inputs delivered as environment variables. Runtime-input
variables never reach agent sessions; this is how a doer iterates on a live
subset, and how a reviewer re-runs a non-consumptive live command.

Every runtime-input variable name is removed from the environment first, then
only this task's reusable inputs are added: single_use inputs are delivered to
the submission gate alone, so no local run can spend them. Running a canonical
command that uses a single_use input is refused before launch; reviewers rely
on the gate receipt for it.

Output is captured (at most 1 MiB), masked, including every declared secret
value, and printed after the command exits; the exit code is the command's.
Identified agents must be the task's assigned doer or claimed reviewer.
A missing input is reported with the operator action (%s).`, brand.Command("provision", "--record")),
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		if taskID == "" {
			return cliValidationError("run-live requires --task")
		}
		timeout, _ := cmd.Flags().GetInt("timeout")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		input := ops.RunLiveInput{TaskID: taskID, Argv: args, TimeoutSeconds: timeout}
		input.Dir, err = os.Getwd()
		if err != nil {
			return err
		}
		agentID, err := identity.Resolve(identity.Config{})
		if err != nil {
			return err
		}
		if agentID != "" {
			authority, err := requireAgentAuthorityForID(cmd, agentID)
			if err != nil {
				return err
			}
			input.Authority = &authority
		}
		result, err := ops.RunLive(projectRoot, input)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), result.Output)
		if result.ExitCode != 0 {
			cmd.SilenceErrors = true
			return &toolResultExit{code: max(result.ExitCode, 1)}
		}
		return nil
	},
}

func init() {
	runLiveCmd.Flags().String("task", "", "Task whose reusable runtime inputs are delivered")
	runLiveCmd.Flags().Int("timeout", 600, "Seconds before the command is stopped (1-3600)")
	rootCmd.AddCommand(runLiveCmd)
}
