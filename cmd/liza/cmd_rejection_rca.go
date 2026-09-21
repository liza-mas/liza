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
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/spf13/cobra"
)

// rejectionRCACommand binds one rejection-RCA operation to its payload file flag
// and its authority-fenced ops and text entrypoints.
type rejectionRCACommand struct {
	operation string
	fileFlag  string
	run       func(projectRoot, taskID string, payload any, authority models.AgentAuthority, opts ops.RejectionRCAOptions) (*ops.RejectionRCAResult, error)
	print     func(projectRoot, taskID string, payload any, authority models.AgentAuthority, opts ops.RejectionRCAOptions) error
}

var recordRejectionRCACmd = newRejectionRCACmd(rejectionRCACommand{
	operation: payloadschema.RecordRejectionRCAOperation,
	fileFlag:  "rca-file",
	run:       ops.RecordRejectionRCAWithAuthority,
	print:     commands.RecordRejectionRCACommand,
}, "Record a classified rejection RCA on a task held by the rejection-RCA gate",
	`Record the durable root-cause analysis a high-churn rejection gate requires.

The --rca-file content is exactly the request object: schema_version, summary
and contributions, each contribution naming a rejection_index, one or more
categories (product_defect, capability_failure, lifecycle_retry, unknown) and
bounded evidence. The gate's threshold, rejection count and timing are seeded by
the gate and cannot be sent. Resubmitting an equivalent request is NO_CHANGE.

Requires a registered orchestrator with the record-rejection-rca capability.
Preflight the file with `+"`"+brand.Command("validate-payload", "record-rejection-rca", "--payload", "<file>")+"`"+`
and read result.diagnostics on INVALID_INPUT.`)

var resumeRejectionRCACmd = newRejectionRCACmd(rejectionRCACommand{
	operation: payloadschema.ResumeRejectionRCAOperation,
	fileFlag:  "disposition-file",
	run:       ops.ResumeRejectionRCAWithAuthority,
	print:     commands.ResumeRejectionRCACommand,
}, "Close a recorded rejection-RCA gate with an authorized recovery disposition",
	`Record the recovery disposition that closes a task's rejection-RCA gate.

The --disposition-file content is exactly the request object: schema_version,
recovery_path (implementation_correction, capability_reroute, lifecycle_repair,
rescope, human_override) and rationale, required for human_override. The restore
mode, actor, lifecycle version and decision time are derived, not sent.

The task stays BLOCKED: unblock-task then restores it in the form the derived
restore mode allows. Requires a recorded RCA and a registered orchestrator with
the resume-rejection-rca capability.`)

func newRejectionRCACmd(spec rejectionRCACommand, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:   spec.operation + " <task-id> --" + spec.fileFlag + " <file>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
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

			// The invocation owns admission failures only. Once ops runs it
			// records telemetry and the text adapter prints the outcome, so the
			// shared stderr rendering would repeat it.
			invocation := beginLifecycleCLI(cmd, args)
			defer func() {
				if !invocation.calledOps {
					invocation.finish(&retErr)
				}
			}()
			requestOpts, err := lifecycleRequestOptions(cmd)
			if err != nil {
				return err
			}
			payload, err := readRejectionRCAPayload(cmd, spec.fileFlag)
			if err != nil {
				return err
			}
			authority, err := requireAgentAuthority(cmd)
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
			if err := validateAllowedOperation(resolver, authority.ID, spec.operation); err != nil {
				return err
			}
			if err := validateRoleType(resolver, authority.ID, "orchestrator"); err != nil {
				return err
			}

			opts := ops.RejectionRCAOptions{Request: requestOpts}
			invocation.calledOps = true
			if isJSON(cmd) {
				result, err := spec.run(projectRoot, args[0], payload, authority, opts)
				return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
			}
			return spec.print(projectRoot, args[0], payload, authority, opts)
		},
	}
}

// readRejectionRCAPayload decodes the request file into its canonical JSON
// object. Structural checks belong to the operation's schema; only a missing,
// unreadable or non-JSON file is refused here.
func readRejectionRCAPayload(cmd *cobra.Command, fileFlag string) (any, error) {
	path, _ := cmd.Flags().GetString(fileFlag)
	if strings.TrimSpace(path) == "" {
		return nil, cliValidationError(fmt.Sprintf("--%s is required", fileFlag))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, cliValidationWrap("reading --"+fileFlag, err)
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		// The decoder's message can quote the rejected bytes; report the
		// offset only.
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, cliValidationError(fmt.Sprintf("--%s is not valid JSON at byte offset %d", fileFlag, syntax.Offset))
		}
		return nil, cliValidationError(fmt.Sprintf("--%s is not valid JSON", fileFlag))
	}
	return payload, nil
}

func init() {
	for _, command := range []struct {
		cmd      *cobra.Command
		fileFlag string
		usage    string
	}{
		{recordRejectionRCACmd, "rca-file", "path to the JSON rejection RCA request"},
		{resumeRejectionRCACmd, "disposition-file", "path to the JSON recovery disposition request"},
	} {
		rootCmd.AddCommand(command.cmd)
		addAgentIDFlag(command.cmd)
		addJSONFlag(command.cmd)
		addLifecycleFlags(command.cmd)
		// Checked in RunE so a missing file keeps the JSON error envelope;
		// Cobra's required-flag check runs before RunE.
		command.cmd.Flags().String(command.fileFlag, "", command.usage)
		command.cmd.ValidArgsFunction = completeTaskIDArgs(1)
	}
}
