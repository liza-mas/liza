package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/liza-mas/liza/internal/brand"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/roles"
)

// Exit statuses of check-submission beside the shared 1 (failure), 2 (lock) and
// 3 (not found): the verdict is pass (0), refused (5) or incomplete (6). They
// stay clear of ExitDegraded (4), whose meaning outcome-checks.md owns.
const (
	exitCheckSubmissionRefused    = 5
	exitCheckSubmissionIncomplete = 6
)

var checkSubmissionCmd = &cobra.Command{
	Use:   "check-submission <task-id> [--agent-id <id>] [--max-wait <seconds>] [--json]",
	Short: "Run the submit gate's checks locally, changing no state",
	Long: fmt.Sprintf(`Evaluate the checks %[1]s applies, on the task worktree's HEAD, and return the
gate's verdict without submitting: the same check list, in the same order, with
the gate's configuration and runtime inputs. It changes no task, receipt,
anomaly, runtime input, branch or HEAD; its one write is the masked output of
a failing canonical command, kept as the gate keeps it (%[2]s).

Exit status: 0 pass, 5 refused (the first refusing check is shown, as the gate
words it), 6 incomplete, 1 failure, 2 lock timeout, 3 unknown task.

A refusal after the gate's preparation marker (published and canonical checks)
reads INVALID_INPUT / correct_input / effects none here, where %[1]s reports
STATE_CHANGED / requery / unknown; the reason is the same.

Incomplete is never a pass: some check could not run exactly as the gate does.
Its reason is shown for each check that was not run:
  rebase_pending            the branch is behind integration, or holds a merge
                            commit: rebase it, then rerun
  rebase_unverified:<why>   the rebase cannot be proven a no-op: tracked_changes
                            (commit or clean them), operation_in_progress
                            (finish or abort it), pre_rebase_hook (an executable
                            pre-rebase hook is present)
  single_use_input:<n>      canonical command <n> uses a single_use runtime
                            input only the gate spends; earlier commands ran
  admission_full            an admission class stayed full for --max-wait
Not applicable, as they have no candidate verdict: submit_hold,
lifecycle_request, preparation, index_refresh, publication_guards.

Agent sessions: the doer's own identity and authority are rechecked read-only.
An operator names the agent with --agent-id and needs no authority. Canonical
commands run in the task worktree as at the gate, behind the same admission
limits at the local tier (see %[3]s).`, brand.Command("submit-for-review"), brand.Command("tool-result", "read"), brand.Command("admit")),
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		err := runCheckSubmission(cmd, args[0])
		if err == nil {
			return nil
		}
		var verdict *checkSubmissionExit
		if errors.As(err, &verdict) {
			cmd.SilenceErrors = true
			return &toolResultExit{code: verdict.code}
		}
		cmd.SilenceErrors = true
		if isJSON(cmd) {
			_ = jsonout.WriteResult(os.Stdout, nil, nil, err)
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		return &toolResultExit{code: checkSubmissionFailureExit(err)}
	},
}

// checkSubmissionExit carries the status of a verdict that is not a pass.
type checkSubmissionExit struct{ code int }

func (e *checkSubmissionExit) Error() string { return "submission check did not pass" }

// checkSubmissionFailureExit maps a command failure to its shared status.
func checkSubmissionFailureExit(err error) int {
	var notFound *lizaerrors.NotFoundError
	switch {
	case errors.As(err, &notFound):
		return lizaerrors.ExitNotFound
	case filelock.IsLockErrorType(err, filelock.LockErrorTimeout):
		return lizaerrors.ExitLock
	}
	return lizaerrors.ExitError
}

// checkSubmissionOutput is the --json result: the local verdict, with the
// refusal rendered as submit-for-review's error envelope renders that error.
type checkSubmissionOutput struct {
	ops.CheckSubmissionResult
	Refusal *jsonout.ErrorDetail `json:"refusal,omitempty"`
}

func runCheckSubmission(cmd *cobra.Command, taskID string) error {
	maxWait, _ := cmd.Flags().GetInt("max-wait")
	flagID, _ := cmd.Flags().GetString("agent-id")
	projectRoot, err := requireProjectRoot()
	if err != nil {
		return err
	}
	input := ops.CheckSubmissionInput{TaskID: taskID, MaxWait: time.Duration(maxWait) * time.Second}
	sessionID, err := identity.Resolve(identity.Config{})
	if err != nil {
		return err
	}
	switch {
	case sessionID != "":
		if flagID != "" && flagID != sessionID {
			return cliValidationError("--agent-id names another agent than the session's; an operator runs without an agent identity")
		}
		authority, err := requireAgentAuthorityForID(cmd, sessionID)
		if err != nil {
			return err
		}
		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateRoleType(resolver, sessionID, roles.RoleTypeDoer); err != nil {
			return err
		}
		input.AgentID, input.Authority = sessionID, &authority
	case flagID != "":
		input.AgentID = flagID
	default:
		return cliValidationError("check-submission requires --agent-id when run outside an agent session")
	}

	result, err := ops.CheckSubmission(projectRoot, input)
	if err != nil {
		return err
	}
	output := checkSubmissionOutput{CheckSubmissionResult: *result}
	if result.Refusal != nil {
		code, message := jsonout.ClassifyError(result.Refusal)
		output.Refusal = &jsonout.ErrorDetail{Code: code, Message: message, Details: jsonout.ErrorDetails(result.Refusal)}
	}
	if isJSON(cmd) {
		if err := jsonout.WriteResult(os.Stdout, output, result.Warnings, nil); err != nil {
			return err
		}
	} else {
		printCheckSubmission(os.Stdout, output)
	}
	switch result.Verdict {
	case ops.SubmissionVerdictRefused:
		return &checkSubmissionExit{code: exitCheckSubmissionRefused}
	case ops.SubmissionVerdictIncomplete:
		return &checkSubmissionExit{code: exitCheckSubmissionIncomplete}
	}
	return nil
}

// printCheckSubmission writes one line per check, then the verdict.
func printCheckSubmission(w io.Writer, output checkSubmissionOutput) {
	for _, check := range output.Checks {
		line := fmt.Sprintf("%-34s %s", check.Name, check.Status)
		if check.Reason != "" {
			line += " (" + check.Reason + ")"
		}
		fmt.Fprintln(w, line)
	}
	if output.Refusal != nil {
		fmt.Fprintf(w, "refusal: %s\n", strings.TrimSpace(output.Refusal.Message))
	}
	for _, deferred := range output.DeferredCommands {
		fmt.Fprintf(w, "deferred: command %d (%s)\n", deferred.Index, deferred.Authority)
	}
	fmt.Fprintf(w, "verdict: %s\n", output.Verdict)
}

func init() {
	addAgentIDFlag(checkSubmissionCmd)
	addJSONFlag(checkSubmissionCmd)
	checkSubmissionCmd.Flags().Int("max-wait", 3600, fmt.Sprintf("Seconds to wait for an admission slot before reporting admission_full (0-%d)", int(ops.AdmitMaxWait/time.Second)))
	checkSubmissionCmd.ValidArgsFunction = completeTaskIDArgs(1)
	rootCmd.AddCommand(checkSubmissionCmd)
}
