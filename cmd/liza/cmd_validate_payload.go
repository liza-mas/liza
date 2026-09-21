package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

// validatePayloadCmd is deliberately ungated: it reads no state, takes no lock
// and commits nothing, so it needs no agent identity and no role capability.
var validatePayloadCmd = &cobra.Command{
	Use:   "validate-payload <operation> --payload <file>",
	Short: "Check a command's payload structurally before invoking it",
	Long: `Validate one operation's payload against its registered schema.

The preflight uses the same validator as the operation's own command, so a
payload it accepts cannot be rejected for structural reasons later. It reads no
state, acquires no lock and changes nothing, and needs no agent identity.

Pass the operation's canonical JSON object. For a command whose payload is one
file, pass that same file: its content is lifted to the canonical object.

An invalid payload exits nonzero and reports result.diagnostics — one entry per
rejected field, naming the schema version, the field path, the violated
constraint and the class of the rejected value, never the value itself. Read
those instead of parsing the error message.

Examples:
  ` + brand.Command("validate-payload", "--list", "--json") + `
  ` + brand.Command("validate-payload", "set-task-output", "--payload", "outputs.json", "--json"),
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		if listSchemas, _ := cmd.Flags().GetBool("list"); listSchemas {
			if len(args) > 0 {
				return cliValidationError("--list reports every registered schema and takes no operation argument")
			}
			listed := commands.ValidatePayloadSchemas()
			if isJSON(cmd) {
				return jsonout.WriteResult(os.Stdout, listed, nil, nil)
			}
			commands.WriteValidatePayloadSchemas(os.Stdout, listed)
			return nil
		}

		if len(args) != 1 {
			return cliValidationError("<operation> is required; run --list to see the registered operations")
		}
		payloadFile, _ := cmd.Flags().GetString("payload")
		if payloadFile == "" {
			return cliValidationError("--payload is required")
		}

		result, err := commands.ValidatePayloadFile(args[0], payloadFile)
		recordValidatePayloadOutcome(cmd, result, err)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return err
		}
		commands.WriteValidatePayloadResult(os.Stdout, *result)
		return nil
	},
}

// recordValidatePayloadOutcome separates preflight rejections from state
// conflicts on the lifecycle counters. The verdict is already decided: this is
// one best-effort observation and never changes it. Counters are scoped to the
// project's sprint, so this is also the only state read on the path — after
// the verdict, and skipped entirely outside a project.
func recordValidatePayloadOutcome(cmd *cobra.Command, result *commands.ValidatePayloadResult, operationErr error) {
	projectRoot, inProject := completionProjectRoot(cmd)
	if !inProject {
		return
	}
	var outcome models.LifecycleOutcome
	if result != nil {
		outcome = result.LifecycleOutcome
	}
	if warning := ops.NewLifecycleInvocation(projectRoot).Finish(commands.ValidatePayloadOperation, outcome, operationErr); warning != nil {
		fmt.Fprintf(os.Stderr, "warning: lifecycle metrics unavailable: %v\n", warning)
	}
}

func init() {
	rootCmd.AddCommand(validatePayloadCmd)
	validatePayloadCmd.Flags().String("payload", "", "path to the operation's canonical JSON payload")
	validatePayloadCmd.Flags().Bool("list", false, "list the registered operations and their schema versions")
	addJSONFlag(validatePayloadCmd)
}
