package commands

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// RecordRejectionRCACommand records a classified rejection RCA on a gated task
// and prints the lifecycle result. The payload is the request file's canonical
// JSON object.
func RecordRejectionRCACommand(projectRoot, taskID string, payload any, authority models.AgentAuthority, opts ops.RejectionRCAOptions) error {
	result, err := ops.RecordRejectionRCAWithAuthority(projectRoot, taskID, payload, authority, opts)
	return printRecordRejectionRCAResult(taskID, result, err)
}

// ResumeRejectionRCACommand closes a task's open rejection-RCA gate with an
// authorized disposition and prints the lifecycle result. The task stays
// BLOCKED; unblock-task restores it in the form the restore mode allows.
func ResumeRejectionRCACommand(projectRoot, taskID string, payload any, authority models.AgentAuthority, opts ops.RejectionRCAOptions) error {
	result, err := ops.ResumeRejectionRCAWithAuthority(projectRoot, taskID, payload, authority, opts)
	return printResumeRejectionRCAResult(taskID, result, err)
}

func printRecordRejectionRCAResult(taskID string, result *ops.RejectionRCAResult, err error) error {
	if printRejectionRCAOutcome(result, err) {
		return wrapRejectionRCAError("record rejection rca", err)
	}
	fmt.Printf("Recorded rejection RCA on %s (fingerprint %s)\n", taskID, result.RejectionRCA.Fingerprint)
	printRejectionRCAWarnings(result)
	return nil
}

func printResumeRejectionRCAResult(taskID string, result *ops.RejectionRCAResult, err error) error {
	if printRejectionRCAOutcome(result, err) {
		return wrapRejectionRCAError("resume rejection rca", err)
	}
	disposition := result.RejectionRCA.Disposition
	fmt.Printf("Resumed rejection RCA on %s: recovery_path %s, restore_mode %s\n", taskID, disposition.RecoveryPath, disposition.RestoreMode)
	fmt.Printf("Task stays BLOCKED; restore it with unblock-task in the form restore_mode %s allows.\n", disposition.RestoreMode)
	printRejectionRCAWarnings(result)
	return nil
}

// printRejectionRCAOutcome renders either operation's outcome, a failure's
// structured outcome included, and reports whether rendering is complete. Only
// a fresh COMPLETED result leaves a success message to the caller.
func printRejectionRCAOutcome(result *ops.RejectionRCAResult, err error) bool {
	if err != nil {
		var failure *ops.LifecycleError
		if errors.As(err, &failure) {
			printLifecycleResult(failure.Outcome)
		}
		return true
	}
	return printLifecycleResult(result.LifecycleOutcome)
}

func wrapRejectionRCAError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

func printRejectionRCAWarnings(result *ops.RejectionRCAResult) {
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
}
