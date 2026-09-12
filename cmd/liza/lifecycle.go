package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/commands"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

func addLifecycleFlags(cmd *cobra.Command) {
	cmd.Flags().String("request-id", "", "logical request ID; preserve with its original --expected-transition across retries")
	cmd.Flags().String("expected-transition", "", "current task transition_id captured before the request; requires --request-id")
	validateArgs := cmd.Args
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if validateArgs != nil {
			if err := validateArgs(cmd, args); err != nil {
				return lifecycleInputFailure(cmd, args, cliValidationWrap("command arguments", err))
			}
		}
		return nil
	}
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return lifecycleInputFailure(cmd, nil, cliValidationWrap("command flags", err))
	})
}

func lifecycleRequestOptions(cmd *cobra.Command) (ops.LifecycleRequestOptions, error) {
	request, _ := cmd.Flags().GetString("request-id")
	expected, _ := cmd.Flags().GetString("expected-transition")
	opts := ops.LifecycleRequestOptions{RequestID: request, ExpectedTransition: expected}
	if (cmd.Flags().Changed("request-id") || cmd.Flags().Changed("expected-transition")) && (request == "" || expected == "") {
		return opts, cliValidationError("request-id and expected-transition must both be non-empty when supplied")
	}
	if err := ops.ValidateLifecycleRequestOptions(opts); err != nil {
		return opts, cliValidationWrap("lifecycle request", err)
	}
	return opts, nil
}

// This invocation owns only CLI admission failures. Once calledOps is set, the
// ops entrypoint owns telemetry, including failures and exact receipt replays.
type lifecycleCLIInvocation struct {
	cmd       *cobra.Command
	taskID    string
	telemetry *ops.LifecycleInvocation
	calledOps bool
}

func beginLifecycleCLI(cmd *cobra.Command, args []string) *lifecycleCLIInvocation {
	call := &lifecycleCLIInvocation{cmd: cmd}
	if len(args) > 0 && cmd.Name() != "recover-agent" {
		call.taskID = args[0]
	}
	if root, ok := completionProjectRoot(cmd); ok {
		call.telemetry = ops.NewLifecycleInvocation(root)
	}
	return call
}

func (call *lifecycleCLIInvocation) finish(retErr *error) {
	if *retErr == nil || errors.Is(*retErr, jsonout.ErrAlreadyWritten) {
		return
	}
	if !call.calledOps {
		outcome, action := models.LifecycleStateChanged, "requery"
		var input *lizaerrors.CLIInputError
		var precondition *ops.PreconditionError
		var permission *lizaerrors.PermissionError
		if errors.As(*retErr, &input) || errors.As(*retErr, &precondition) {
			outcome, action = models.LifecycleInvalidInput, "correct_input"
		} else if errors.As(*retErr, &permission) {
			outcome, action = models.LifecycleForbidden, "stop"
		}
		*retErr = ops.WrapLifecycleError(call.cmd.Name(), nil, *retErr, outcome, action, "none")
	}
	var failure *ops.LifecycleError
	if !errors.As(*retErr, &failure) {
		return
	}
	if !call.calledOps {
		failure.Outcome.TaskID = call.taskID
		// Invalid identifiers must not become reflected diagnostic payloads.
		if opts, err := lifecycleRequestOptions(call.cmd); err == nil {
			failure.Outcome.RequestID = opts.RequestID
		}
		if call.telemetry != nil {
			if err := call.telemetry.Finish(call.cmd.Name(), failure.Outcome, *retErr); err != nil {
				fmt.Fprintf(os.Stderr, "warning: lifecycle metrics unavailable: %v\n", err)
			}
		}
	}
	if !isJSON(call.cmd) {
		commands.WriteLifecycleOutcome(os.Stderr, failure.Outcome)
	}
}

func lifecycleInputFailure(cmd *cobra.Command, args []string, err error) error {
	beginLifecycleCLI(cmd, args).finish(&err)
	if isJSON(cmd) {
		return jsonout.WriteResult(os.Stdout, nil, nil, err)
	}
	return err
}
