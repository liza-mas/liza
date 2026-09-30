package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var provisionCmd = &cobra.Command{
	Use:   "provision --record --task <task-id> --input <input-id> --file <envelope>",
	Short: "Record an operator-produced runtime input instance",
	Long: fmt.Sprintf(`Register one hand-produced runtime input in the consumption ledger.

The envelope is an operator-owned KEY=VALUE file outside the repository and its
worktrees. It must define exactly the variables the task's runtime_inputs
declaration names, with no surrounding whitespace, inline " #" comments or
empty values; secret values need at least 8 bytes. Variables listed under the
declaration's files hold absolute paths to file artifacts, which must also live
outside the repository. The ledger stores the envelope path, variable names and
a keyed identity digest, never values. The key is created on first use at
<home>/<global dir>/runtime-input.key; back it up with your secret sources.

The identity covers the delivered values and file-artifact contents, not the
envelope's formatting or the files' locations. Recording a materialization
already in the ledger returns it unchanged: a consumed or invalidated one stays
spent. A reusable input may be recorded for several tasks (repeat --task).

Operator-only: identified agent sessions are refused. As with other operator
commands, absence of an agent identity is not authentication of a human.

After recording, restore a task blocked for a missing input with %s.`, brand.Command("unblock-task", "<task-id>")),
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return cliValidationError("provision is operator-only; agent sessions cannot record runtime inputs")
		}
		if record, _ := cmd.Flags().GetBool("record"); !record {
			return cliValidationError("provision requires --record: recipe execution is not available in this version")
		}
		tasks, _ := cmd.Flags().GetStringArray("task")
		inputID, _ := cmd.Flags().GetString("input")
		envelope, _ := cmd.Flags().GetString("file")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		result, err := ops.RecordRuntimeInput(projectRoot, ops.RecordRuntimeInputInput{Tasks: tasks, InputID: inputID, Envelope: envelope})
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Runtime input %s (%s) %s: instance %s is %s for %s.\n",
			result.InputID, result.Recipe, result.Outcome, result.InstanceID[:16], result.State, strings.Join(result.Tasks, ", "))
		return nil
	},
}

func init() {
	provisionCmd.Flags().Bool("record", false, "Record an operator-produced instance")
	provisionCmd.Flags().StringArray("task", nil, "Task the instance serves (repeatable for reusable inputs)")
	provisionCmd.Flags().String("input", "", "Runtime input id declared by the task(s)")
	provisionCmd.Flags().String("file", "", "Absolute path of the KEY=VALUE envelope")
	addJSONFlag(provisionCmd)
	rootCmd.AddCommand(provisionCmd)
}
