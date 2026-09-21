package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/spf13/cobra"
)

var replaceTaskCmd = &cobra.Command{
	Use:   "replace-task --replacement-file <file> --request-id <id> --expected-transition <token>",
	Short: "Atomically replace a task and retarget its consumers",
	Long: `Replace a source task with one new task in a single transaction.

The JSON file declares source_task_id, reason, replacement (the add-task
object), consumers (expected and desired dependency lists), and optionally
preserved_base (both base_commit and an existing matching worktree).

Requires an orchestrator with the replace-task capability. Both --request-id
and --expected-transition are required; preserve the original pair and payload
across retries. Read result.outcome and result.safe_action before proceeding.

Example:
  ` + brand.Command("replace-task", "--replacement-file", "replacement.json", "--request-id", "replace-1", "--expected-transition", "<token>", "--json"),
	Args: cobra.NoArgs,
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
		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		request, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}
		input, err := readReplacementInput(cmd)
		if err != nil {
			return err
		}
		invocation.taskID = input.SourceTaskID
		authority, err := resolveOrchestratorAuthority(cmd)
		if err != nil {
			return err
		}
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, authority.ID, "replace-task"); err != nil {
			return err
		}
		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.ReplaceTaskWithAuthorityAndOptions(projectRoot, input, authority, request)
			var warnings []string
			if result != nil {
				warnings = result.Warnings
			}
			return jsonout.WriteResult(os.Stdout, result, warnings, err)
		}
		return commands.ReplaceTaskWithAuthorityAndOptionsCommand(projectRoot, input, authority, request)
	},
}

// Validate the original file before typed decoding can discard field shape.
func readReplacementInput(cmd *cobra.Command) (ops.ReplaceTaskInput, error) {
	var input ops.ReplaceTaskInput
	path, _ := cmd.Flags().GetString("replacement-file")
	if strings.TrimSpace(path) == "" {
		return input, cliValidationError("--replacement-file is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return input, cliValidationWrap("reading --replacement-file", err)
	}
	_, diagnostics, err := payloadschema.Validate("replace-task", json.RawMessage(data))
	if err != nil {
		return input, err
	}
	if len(diagnostics) > 0 {
		return input, ops.NewLifecycleInvalidInputError("replace-task", nil, diagnostics, fmt.Errorf("replacement payload rejected by schema"))
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return input, cliValidationError("replacement payload cannot be decoded")
	}
	return input, nil
}

func init() {
	rootCmd.AddCommand(replaceTaskCmd)
	addAgentIDFlag(replaceTaskCmd)
	addJSONFlag(replaceTaskCmd)
	addLifecycleFlags(replaceTaskCmd)
	// Check in RunE so missing input receives the lifecycle JSON envelope.
	replaceTaskCmd.Flags().String("replacement-file", "", "path to the JSON replacement transaction")
}
