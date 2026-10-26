package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/spf13/cobra"
)

var markBlockedCmd = &cobra.Command{
	Use:   "mark-blocked <task-id>",
	Short: "Mark a task as BLOCKED due to unresolvable blocker",
	Long: `Mark a task as BLOCKED when work cannot proceed.

Per the blocking protocol (specs/architecture/roles.md), use this when:
  - Spec ambiguity prevents implementation
  - Missing external dependency blocks progress
  - Design conflict discovered that requires rescoping

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must be in an executing status (e.g. IMPLEMENTING_CODE, CODE_PLANNING)
  - Only the assigned agent can mark a task as blocked
  - Requires a reason and 1-3 clarifying questions

Effects:
  - status = BLOCKED
  - blocked_reason = <reason>
  - blocked_questions = [<questions>]
  - repair_request = <structured orchestrator repair request> when --repair-* flags or --repair-request-file are provided
  - With --human-action, the block is human-owned: an AWAITING HUMAN alert names the ask
  - Clear assigned_to
  - Clear lease_expires
  - Add history entry with event "blocked"
  - Triggers orchestrator wake`,
	Args: cobra.ExactArgs(1),
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
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		reason, _ := cmd.Flags().GetString("reason")
		questions, _ := cmd.Flags().GetStringSlice("questions")
		opts, err := markBlockedOptionsFromFlags(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "mark-blocked"); err != nil {
			return err
		}

		opts.Request = requestOpts
		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.MarkBlockedWithAuthority(projectRoot, taskID, reason, questions, authority, opts)
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		return commands.MarkBlockedWithAuthorityCommand(projectRoot, taskID, reason, questions, authority, opts)
	},
}

var unblockTaskCmd = &cobra.Command{
	Use:   "unblock-task <task-id>",
	Short: "Restore a repaired BLOCKED task to its initial or executing state",
	Long: `Restore a BLOCKED task after the orchestrator has verified that the blocker is gone.

	This is for repair completion, not normal task claiming. By default the restore
	is a continuation: the task returns to the initial status for its role_pair, and
	the next claim by any doer resumes its preserved work without consuming an
	iteration. Pending dependencies keep that task dependency-held and
	not immediately claimable. With --new-iteration, the resume consumes an
	iteration. With --assign-to,
	it directly restores the executing status and assigns the requested doer agent.
	With --rebase-on, it rebases a preserved task worktree before unblocking, updates
	base_commit, and leaves rebase conflicts BLOCKED with fresh repair metadata.
	With --scope-exception-file, a coding task also receives a bounded in-task
	scope exception: one strict JSON object {"files": [...], "change": "...",
	"unchanged": "..."} of 1-8 repository-relative files and at most 4096 bytes.
	Outside its original scope the task may then change only those files, only as
	stated; acceptance, provider dependencies, review boundary and lifecycle gates
	stay unchanged. Use it when the blocked task can close the gap itself in test
	support, fixtures or its own constants, with production code and merged
	contracts unchanged and no other unmerged writer of those files.
	With --budget-exception <seconds> and --budget-ruling <decision-id>, a coding
	task's canonical admission batch budget becomes the larger of its reviewed
	timeout_seconds and the largest recorded exception (at most 14400). Commands,
	order, manifest, proofs and deferrals stay as reviewed. Use it when admission
	timed out and a human ruling authorizes the larger budget; the ruling is
	recorded as given, not looked up.`,
	Args: cobra.ExactArgs(1),
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
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]
		assignTo, _ := cmd.Flags().GetString("assign-to")
		if !cmd.Flags().Changed("assign-to") {
			assignTo = ""
		}
		reason, _ := cmd.Flags().GetString("reason")
		rebaseOn, _ := cmd.Flags().GetString("rebase-on")
		if !cmd.Flags().Changed("rebase-on") {
			rebaseOn = ""
		}
		allowDirty, _ := cmd.Flags().GetBool("allow-dirty")
		if !cmd.Flags().Changed("allow-dirty") {
			allowDirty = false
		}
		newIteration, _ := cmd.Flags().GetBool("new-iteration")
		if !cmd.Flags().Changed("new-iteration") {
			newIteration = false
		}
		var scopeException *ops.ScopeException
		if cmd.Flags().Changed("scope-exception-file") {
			path, _ := cmd.Flags().GetString("scope-exception-file")
			scopeException, err = readScopeException(path)
			if err != nil {
				return err
			}
		}
		budgetException, err := budgetExceptionFromFlags(cmd)
		if err != nil {
			return err
		}
		opts := ops.UnblockTaskOptions{
			Request:         requestOpts,
			AssignTo:        assignTo,
			RebaseOn:        rebaseOn,
			AllowDirty:      allowDirty,
			NewIteration:    newIteration,
			ScopeException:  scopeException,
			BudgetException: budgetException,
		}

		if operatorInvocation(cmd) {
			projectRoot, err := requireProjectRoot()
			if err != nil {
				return err
			}
			actor := resolveChangedBy(cmd)
			invocation.calledOps = true
			if isJSON(cmd) {
				result, err := ops.UnblockTaskAsOperator(projectRoot, taskID, reason, actor, opts)
				return jsonout.WriteResult(os.Stdout, result, nil, err)
			}
			return commands.UnblockTaskAsOperatorCommand(projectRoot, taskID, reason, actor, opts)
		}

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
		if err := validateAllowedOperation(resolver, authority.ID, "unblock-task"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.UnblockTaskWithAuthority(projectRoot, taskID, reason, authority, opts)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.UnblockTaskWithAuthorityCommand(projectRoot, taskID, reason, authority, opts)
	},
}

func readScopeException(path string) (*ops.ScopeException, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, cliValidationWrap("opening --scope-exception-file", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, ops.MaxScopeExceptionBytes+1))
	if err != nil {
		return nil, cliValidationWrap("reading --scope-exception-file", err)
	}
	exception, err := ops.ParseScopeException(data)
	if err != nil {
		return nil, cliValidationWrap("--scope-exception-file", err)
	}
	return exception, nil
}

func budgetExceptionFromFlags(cmd *cobra.Command) (*ops.BudgetException, error) {
	seconds, ruling := cmd.Flags().Changed("budget-exception"), cmd.Flags().Changed("budget-ruling")
	if !seconds && !ruling {
		return nil, nil
	}
	if !seconds || !ruling {
		return nil, cliValidationError("--budget-exception and --budget-ruling must be given together")
	}
	exception := &ops.BudgetException{}
	exception.TimeoutSeconds, _ = cmd.Flags().GetInt("budget-exception")
	exception.Ruling, _ = cmd.Flags().GetString("budget-ruling")
	if err := exception.Validate(); err != nil {
		return nil, cliValidationWrap("--budget-exception", err)
	}
	return exception, nil
}

func markBlockedOptionsFromFlags(cmd *cobra.Command) (ops.MarkBlockedOptions, error) {
	dependsOn, _ := cmd.Flags().GetStringSlice("depends-on")
	repairRequest, err := repairRequestFromFlags(cmd)
	if err != nil {
		return ops.MarkBlockedOptions{}, err
	}
	humanAction, err := humanActionFromFlag(cmd)
	if err != nil {
		return ops.MarkBlockedOptions{}, err
	}
	stateLockTimeout, _ := cmd.Flags().GetBool("state-lock-timeout")
	return ops.MarkBlockedOptions{DependsOn: dependsOn, RepairRequest: repairRequest, HumanAction: humanAction, StateLockTimeout: stateLockTimeout}, nil
}

// humanActionFromFlag reads --human-action; given but empty is an input error
// rather than a silently agent-owned block.
func humanActionFromFlag(cmd *cobra.Command) (string, error) {
	humanAction, _ := cmd.Flags().GetString("human-action")
	if cmd.Flags().Changed("human-action") && strings.TrimSpace(humanAction) == "" {
		return "", cliValidationError("--human-action must not be empty")
	}
	return humanAction, nil
}

func repairRequestFromFlags(cmd *cobra.Command) (*models.RepairRequest, error) {
	repairRequestFile, _ := cmd.Flags().GetString("repair-request-file")
	operation, _ := cmd.Flags().GetString("repair-operation")
	target, _ := cmd.Flags().GetString("repair-target")
	command, _ := cmd.Flags().GetString("repair-command")
	evidence, _ := cmd.Flags().GetStringArray("repair-evidence")
	validation, _ := cmd.Flags().GetStringArray("repair-validation")

	if cmd.Flags().Changed("repair-request-file") {
		if strings.TrimSpace(repairRequestFile) == "" {
			return nil, cliValidationError("--repair-request-file requires a path")
		}
		for _, name := range []string{"repair-operation", "repair-target", "repair-command", "repair-evidence", "repair-validation"} {
			if cmd.Flags().Changed(name) {
				return nil, cliValidationError("--repair-request-file cannot be combined with --repair-* fields")
			}
		}

		data, err := os.ReadFile(repairRequestFile)
		if err != nil {
			return nil, cliValidationWrap("reading repair request file", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return nil, cliValidationError("repair request file is empty")
		}
		var request models.RepairRequest
		if err := json.Unmarshal(data, &request); err != nil {
			return nil, cliValidationWrap("parsing repair request file", err)
		}
		return &request, nil
	}

	hasRepairRequest := false
	for _, name := range []string{"repair-operation", "repair-target", "repair-command", "repair-evidence", "repair-validation"} {
		hasRepairRequest = hasRepairRequest || cmd.Flags().Changed(name)
	}
	if !hasRepairRequest {
		return nil, nil
	}
	if strings.TrimSpace(operation) == "" {
		return nil, cliValidationError("--repair-operation is required when repair request fields are provided")
	}
	if strings.TrimSpace(target) == "" {
		return nil, cliValidationError("--repair-target is required when repair request fields are provided")
	}
	if strings.TrimSpace(command) == "" {
		return nil, cliValidationError("--repair-command is required when repair request fields are provided")
	}
	if !hasNonEmptyValue(evidence) {
		return nil, cliValidationError("--repair-evidence is required when repair request fields are provided")
	}
	if !hasNonEmptyValue(validation) {
		return nil, cliValidationError("--repair-validation is required when repair request fields are provided")
	}
	return &models.RepairRequest{
		Operation:  operation,
		Target:     target,
		Command:    command,
		Evidence:   evidence,
		Validation: validation,
	}, nil
}

func hasNonEmptyValue(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

var assessBlockedCmd = &cobra.Command{
	Use:   "assess-blocked <task-id>",
	Short: "Record orchestrator assessment of a BLOCKED task",
	Long: `Record that the orchestrator has assessed a BLOCKED task.

This prevents the orchestrator re-wake loop where blocked tasks that have
already been triaged continue to trigger new orchestrator sessions.

After assessing, the task remains BLOCKED but won't trigger further wakes
unless new activity occurs (dependency changes, human notes, etc.).

With --awaits, the task waits for all of the named existing, unfinished
tasks: it wakes once they have all merged, or as soon as one fails, instead of
on every generated task under its dependencies or each named task settling.
Use it when the block waits on work a dependency edge cannot express. An
assessment without --awaits keeps the current set, minus merged tasks;
--clear-awaits drops it.

With --human-action, the block is human-owned: an AWAITING HUMAN alert names
the ask. An assessment without it keeps the current ask; --clear-human-action
drops it.

Requirements:
  - Agent ID must be provided (via --agent-id flag)
  - Task must be in BLOCKED status`,
	Args: cobra.ExactArgs(1),
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
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		note, _ := cmd.Flags().GetString("note")
		opts, err := assessBlockedOptionsFromFlags(cmd)
		if err != nil {
			return err
		}

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
		if err := validateRoleType(resolver, authority.ID, string(roles.RoleTypeOrchestrator)); err != nil {
			return err
		}

		opts.Request = requestOpts
		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.AssessBlockedWithAuthority(projectRoot, taskID, note, authority, opts)
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		return commands.AssessBlockedWithAuthorityCommand(projectRoot, taskID, note, authority, opts)
	},
}

func assessBlockedOptionsFromFlags(cmd *cobra.Command) (ops.AssessBlockedOptions, error) {
	reason, _ := cmd.Flags().GetString("reason")
	questions, _ := cmd.Flags().GetStringArray("question")
	awaited, _ := cmd.Flags().GetStringSlice("awaits")
	clearAwaits, _ := cmd.Flags().GetBool("clear-awaits")
	if clearAwaits && len(awaited) > 0 {
		return ops.AssessBlockedOptions{}, cliValidationError("--clear-awaits cannot be combined with --awaits")
	}
	humanAction, err := humanActionFromFlag(cmd)
	if err != nil {
		return ops.AssessBlockedOptions{}, err
	}
	clearHumanAction, _ := cmd.Flags().GetBool("clear-human-action")
	stateLockTimeout, _ := cmd.Flags().GetBool("state-lock-timeout")
	if clearHumanAction && humanAction != "" {
		return ops.AssessBlockedOptions{}, cliValidationError("--clear-human-action cannot be combined with --human-action")
	}
	repairRequest, err := repairRequestFromFlags(cmd)
	if err != nil {
		return ops.AssessBlockedOptions{}, err
	}
	reconcile := cmd.Flags().Changed("reason") || cmd.Flags().Changed("question") || repairRequest != nil
	if !reconcile {
		return ops.AssessBlockedOptions{AwaitedTasks: awaited, ClearAwaited: clearAwaits, HumanAction: humanAction, ClearHumanAction: clearHumanAction, StateLockTimeout: stateLockTimeout}, nil
	}
	if strings.TrimSpace(reason) == "" {
		return ops.AssessBlockedOptions{}, cliValidationError("--reason is required for canonical metadata reconciliation")
	}
	if len(questions) == 0 {
		return ops.AssessBlockedOptions{}, cliValidationError("--question is required for canonical metadata reconciliation")
	}
	if len(questions) > 3 {
		return ops.AssessBlockedOptions{}, cliValidationError("--question may be repeated at most 3 times")
	}
	for _, question := range questions {
		if strings.TrimSpace(question) == "" {
			return ops.AssessBlockedOptions{}, cliValidationError("--question values must not be empty")
		}
	}
	return ops.AssessBlockedOptions{Reason: reason, Questions: questions, RepairRequest: repairRequest, AwaitedTasks: awaited, ClearAwaited: clearAwaits, HumanAction: humanAction, ClearHumanAction: clearHumanAction, StateLockTimeout: stateLockTimeout}, nil
}

var assessHypothesisExhaustedCmd = &cobra.Command{
	Use:   "assess-hypothesis-exhausted <task-id>",
	Short: "Record orchestrator assessment of a hypothesis-exhausted task",
	Long: `Record that the orchestrator has assessed a hypothesis-exhausted task
(2+ coders failed on it).

This prevents the orchestrator re-wake loop where hypothesis-exhausted tasks
that have already been triaged continue to trigger new orchestrator sessions.

After assessing, the task keeps its current status but won't trigger further
wakes unless new activity occurs (new failures, human notes, etc.).

Requirements:
  - Agent ID must be provided (via --agent-id flag)
  - Task must have 2+ entries in failed_by and not be in terminal status`,
	Args: cobra.ExactArgs(1),
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
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		note, _ := cmd.Flags().GetString("note")

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
		if err := validateRoleType(resolver, authority.ID, string(roles.RoleTypeOrchestrator)); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.AssessHypothesisExhaustedWithAuthorityAndOptions(projectRoot, taskID, note, authority, requestOpts)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AssessHypothesisExhaustedWithAuthorityAndOptionsCommand(projectRoot, taskID, note, authority, requestOpts)
	},
}

func init() {
	for _, cmd := range []*cobra.Command{unblockTaskCmd, assessHypothesisExhaustedCmd, markBlockedCmd, assessBlockedCmd} {
		addLifecycleFlags(cmd)
	}
	rootCmd.AddCommand(markBlockedCmd)
	rootCmd.AddCommand(unblockTaskCmd)
	rootCmd.AddCommand(assessBlockedCmd)
	rootCmd.AddCommand(assessHypothesisExhaustedCmd)

	markBlockedCmd.ValidArgsFunction = completeTaskIDArgs(1)
	unblockTaskCmd.ValidArgsFunction = completeTaskIDArgs(1)
	assessBlockedCmd.ValidArgsFunction = completeTaskIDArgs(1)
	assessHypothesisExhaustedCmd.ValidArgsFunction = completeTaskIDArgs(1)

	addJSONFlag(markBlockedCmd)
	addJSONFlag(unblockTaskCmd)
	addJSONFlag(assessBlockedCmd)
	addJSONFlag(assessHypothesisExhaustedCmd)

	addChangedByFlag(unblockTaskCmd)
	unblockTaskCmd.Long += "\n\n" + operatorInvocationHelp

	// Mark-blocked command flags
	markBlockedCmd.Flags().String("reason", "", "reason why the task is blocked (required)")
	markBlockedCmd.Flags().StringSlice("questions", nil, "clarifying questions (1-3 required)")
	markBlockedCmd.Flags().String("repair-operation", "", "orchestrator-only repair operation requested for this blocker")
	markBlockedCmd.Flags().String("repair-target", "", "task or state object the requested repair should modify")
	markBlockedCmd.Flags().String("repair-command", "", "exact command the orchestrator should run or adapt")
	markBlockedCmd.Flags().StringArray("repair-evidence", nil, "evidence gathered before requesting orchestrator repair")
	markBlockedCmd.Flags().StringArray("repair-validation", nil, "validation already run or required after orchestrator repair")
	markBlockedCmd.Flags().String("repair-request-file", "", "path to a complete JSON repair request; mutually exclusive with --repair-* fields")
	markBlockedCmd.Flags().StringSlice("depends-on", nil, "task IDs blocking this task; also used as the orchestrator re-wake signal")
	markBlockedCmd.Flags().String("human-action", "", "one-line action only a human can take to clear this block, and how to signal it is done; raises AWAITING HUMAN")
	markBlockedCmd.Flags().Bool("state-lock-timeout", false, "tag an infrastructure-only state-lock timeout for automatic continuation after a later successful publication")
	markBlockedCmd.Flags().String("agent-id", "", "agent ID marking the task as blocked")
	markBlockedCmd.MarkFlagRequired("reason")
	markBlockedCmd.MarkFlagRequired("questions")
	registerCompletion(markBlockedCmd, "depends-on", completeTaskIDs)
	registerCompletion(markBlockedCmd, "agent-id", completeAgentIDs)

	// Unblock-task command flags
	unblockTaskCmd.Flags().String("agent-id", "", "orchestrator agent ID (overrides "+brand.EnvName("AGENT_ID")+"; without either, runs as operator)")
	unblockTaskCmd.Flags().String("assign-to", "", "doer agent ID to resume the task directly; omitted restores initial status and may remain dependency-held")
	unblockTaskCmd.Flags().String("reason", "", "reason the blocked task can resume (required)")
	unblockTaskCmd.Flags().String("rebase-on", "", "branch or commit to rebase the task worktree onto before unblocking")
	unblockTaskCmd.Flags().Bool("allow-dirty", false, "allow tracked worktree changes during --rebase-on by using git rebase --autostash")
	unblockTaskCmd.Flags().Bool("new-iteration", false, "make the resume consume an iteration; omitted, the next claim continues the current iteration")
	unblockTaskCmd.Flags().String("scope-exception-file", "", "JSON file granting a coding task a bounded in-task scope exception (files, change, unchanged)")
	unblockTaskCmd.Flags().Int("budget-exception", 0, "raise a coding task's canonical admission budget to this many seconds (requires --budget-ruling)")
	unblockTaskCmd.Flags().String("budget-ruling", "", "human decision that authorizes --budget-exception")
	unblockTaskCmd.MarkFlagRequired("reason")
	registerCompletion(unblockTaskCmd, "agent-id", completeAgentIDs)
	registerCompletion(unblockTaskCmd, "assign-to", completeAgentIDs)

	// Assess-blocked command flags
	assessBlockedCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
	assessBlockedCmd.Flags().String("note", "", "optional note about the assessment outcome")
	assessBlockedCmd.Flags().String("reason", "", "current canonical blocked reason; requires --question")
	assessBlockedCmd.Flags().StringArray("question", nil, "current canonical blocked question (repeat 1-3 times with --reason)")
	assessBlockedCmd.Flags().String("repair-operation", "", "orchestrator-only repair operation requested for the current blocker")
	assessBlockedCmd.Flags().String("repair-target", "", "task or state object the requested repair should modify")
	assessBlockedCmd.Flags().String("repair-command", "", "exact command the orchestrator should run or adapt")
	assessBlockedCmd.Flags().StringArray("repair-evidence", nil, "evidence gathered before requesting orchestrator repair")
	assessBlockedCmd.Flags().StringArray("repair-validation", nil, "validation already run or required after orchestrator repair")
	assessBlockedCmd.Flags().String("repair-request-file", "", "path to a complete JSON repair request; mutually exclusive with --repair-* fields")
	assessBlockedCmd.Flags().StringSlice("awaits", nil, "existing unfinished task IDs the block waits for, all of them (comma-separated or repeated); wakes when all merge or one fails")
	assessBlockedCmd.Flags().Bool("clear-awaits", false, "drop the awaited set instead of carrying it forward; not with --awaits")
	assessBlockedCmd.Flags().String("human-action", "", "one-line action only a human can take to clear this block; sets or replaces the ask and raises AWAITING HUMAN")
	assessBlockedCmd.Flags().Bool("clear-human-action", false, "drop the human ask once agents own the repair; not with --human-action")
	assessBlockedCmd.Flags().Bool("state-lock-timeout", false, "explicitly adopt an infrastructure-only state-lock hold for automatic continuation")
	registerCompletion(assessBlockedCmd, "agent-id", completeAgentIDs)

	// Assess-hypothesis-exhausted command flags
	assessHypothesisExhaustedCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
	assessHypothesisExhaustedCmd.Flags().String("note", "", "optional note about the assessment outcome")
	registerCompletion(assessHypothesisExhaustedCmd, "agent-id", completeAgentIDs)
}
