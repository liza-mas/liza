package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
)

// resolveOrchestratorID resolves the orchestrator agent ID from params or state.
// When agent_id is absent from params, auto-resolves from the registered orchestrator.
// When agent_id is present but empty or non-string, returns a validation error
// to prevent silent identity assumption on malformed input.
func (s *Server) resolveOrchestratorID(params map[string]any) (string, error) {
	raw, present := params["agent_id"]
	if present {
		agentID, ok := raw.(string)
		if !ok || agentID == "" {
			return "", fmt.Errorf("agent_id must be a non-empty string")
		}
		return agentID, nil
	}
	statePath := paths.New(s.projectRoot).StatePath()
	resolved, err := ops.ResolveOrchestratorFromState(statePath, s.resolver)
	if err != nil {
		return "", fmt.Errorf("agent_id not provided and auto-resolution failed: %w", err)
	}
	return resolved, nil
}

// handleAddTasks implements the liza_add_tasks tool (batch endpoint).
func (s *Server) handleAddTasks(params map[string]any) (any, error) {
	agentID, err := s.resolveOrchestratorID(params)
	if err != nil {
		return nil, err
	}

	if err := isOperationAllowed(s.resolver, s.pipelineLoadErr, agentID, "liza_add_tasks"); err != nil {
		return nil, err
	}

	rawTasks, ok := params["tasks"].([]any)
	if !ok {
		return nil, fmt.Errorf("tasks parameter must be an array")
	}
	if len(rawTasks) == 0 {
		return nil, fmt.Errorf("tasks array must not be empty")
	}

	tasks, err := extractTaskInputs(rawTasks)
	if err != nil {
		return nil, err
	}

	input := &ops.AddTasksInput{Tasks: tasks, OrchestratorID: agentID}
	statePath := paths.New(s.projectRoot).StatePath()
	result, err := ops.AddTasks(statePath, s.logPath, input)
	if err != nil {
		return nil, fmt.Errorf("add tasks failed: %w", err)
	}

	return textResult(formatAddTasksResult(result))
}

// handleAddTaskCompat is a deprecated compatibility wrapper for liza_add_task.
// It wraps the single-task params into a batch call to handleAddTasks.
func (s *Server) handleAddTaskCompat(params map[string]any) (any, error) {
	agentID, _ := params["agent_id"].(string)
	task := make(map[string]any, len(params))
	for k, v := range params {
		if k != "agent_id" {
			task[k] = v
		}
	}
	return s.handleAddTasks(map[string]any{
		"tasks":    []any{task},
		"agent_id": agentID,
	})
}

// handleClaimTask implements the liza_claim_task tool
// Maps to: liza claim-task
func (s *Server) handleClaimTask(params map[string]any) (any, error) {
	taskID, agentID, err := requireTaskAndAgent(params)
	if err != nil {
		return nil, err
	}

	result, err := ops.ClaimTask(s.projectRoot, taskID, agentID)
	if err != nil {
		return nil, fmt.Errorf("claim task failed: %w", err)
	}

	return textResult(appendWarnings(
		fmt.Sprintf("Task %s claimed by %s (from %s), worktree: %s",
			result.TaskID, result.AgentID, result.SourceStatus, result.WorktreeRel),
		result.Warnings))
}

// handleSubmitForReview implements the liza_submit_for_review tool
// Maps to: liza submit-for-review
func (s *Server) handleSubmitForReview(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	commitSHA, err := requireString(params, "commit_sha")
	if err != nil {
		return nil, err
	}

	agentID, err := requireString(params, "agent_id")
	if err != nil {
		return nil, err
	}

	result, err := ops.SubmitForReview(s.projectRoot, taskID, commitSHA, agentID)
	if err != nil {
		return nil, fmt.Errorf("submit for review failed: %w", err)
	}

	return textResult(fmt.Sprintf("Task %s submitted for review (commit: %s)", result.TaskID, result.ReviewCommit))
}

// handleAwaitVerdict implements the liza_await_verdict tool.
// Blocks until a review verdict arrives for a submitted task.
func (s *Server) handleAwaitVerdict(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	agentID, err := requireString(params, "agent_id")
	if err != nil {
		return nil, err
	}

	// Optional timeout with default 1500s (25 min, within Claude Code's 30 min MCP_TIMEOUT)
	timeoutSeconds := 1500
	if v, ok := params["timeout_seconds"].(float64); ok && v > 0 {
		timeoutSeconds = int(v)
	}
	timeout := time.Duration(timeoutSeconds) * time.Second

	result, err := ops.AwaitVerdict(context.Background(), s.projectRoot, taskID, agentID, timeout)
	if err != nil {
		if errors.Is(err, ops.ErrBudgetExhausted) {
			return textResult("Budget exhausted: iteration or review-cycle limit reached. Exit normally.")
		}
		return nil, fmt.Errorf("await verdict failed: %w", err)
	}

	msg := fmt.Sprintf("Verdict: %s\nStatus: %s\nReason: %s\nReviewer: %s",
		result.Verdict, result.TaskStatus, result.Reason, result.ReviewerAgent)
	if result.Guidance != "" {
		msg += fmt.Sprintf("\n\n%s", result.Guidance)
	}

	return textResult(msg)
}

// handleAwaitResubmission implements the liza_await_resubmission tool.
// Blocks until a doer resubmits after a rejection, preserving review ownership.
//
//lint:ignore U1000 Registered by registerMutationTools in server_registration.go (sibling task)
func (s *Server) handleAwaitResubmission(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	agentID, err := requireString(params, "agent_id")
	if err != nil {
		return nil, err
	}

	// Optional timeout with default 1500s (25 min, within Claude Code's 30 min MCP_TIMEOUT)
	timeoutSeconds := 1500
	if v, ok := params["timeout_seconds"].(float64); ok && v > 0 {
		timeoutSeconds = int(v)
	}
	timeout := time.Duration(timeoutSeconds) * time.Second

	result, err := ops.AwaitResubmission(context.Background(), s.projectRoot, taskID, agentID, timeout)
	if err != nil {
		return nil, fmt.Errorf("await resubmission failed: %w", err)
	}

	msg := fmt.Sprintf("Verdict: %s\nStatus: %s", result.Verdict, result.TaskStatus)
	if result.Verdict == ops.ResubmissionResubmitted {
		msg += fmt.Sprintf("\nNew submission received. Review the changes at commit %s. Review cycle %d.",
			result.ReviewCommit, result.ReviewCycle)
	}
	if result.Reason != "" {
		msg += fmt.Sprintf("\nReason: %s", result.Reason)
	}

	return textResult(msg)
}

// handleHandoff implements the liza_handoff tool
// Maps to: liza handoff
func (s *Server) handleHandoff(params map[string]any) (any, error) {
	taskID, agentID, err := requireTaskAndAgent(params)
	if err != nil {
		return nil, err
	}

	summary, err := requireString(params, "summary")
	if err != nil {
		return nil, err
	}

	nextAction, err := requireString(params, "next_action")
	if err != nil {
		return nil, err
	}

	input := &ops.HandoffInput{
		ProjectRoot: s.projectRoot,
		TaskID:      taskID,
		Summary:     summary,
		NextAction:  nextAction,
		AgentID:     agentID,
		Succeeded:   extractStringSlice(params, "succeeded"),
		Failed:      extractStringSlice(params, "failed"),
		KeyFiles:    extractStringSlice(params, "key_files"),
		DeadEnds:    extractStringSlice(params, "dead_ends"),
	}
	if h, ok := params["hypothesis"].(string); ok {
		input.Hypothesis = h
	}

	result, err := ops.Handoff(input)
	if err != nil {
		return nil, fmt.Errorf("handoff failed: %w", err)
	}

	return textResult(fmt.Sprintf("Handoff initiated for task %s by %s", result.TaskID, result.AgentID))
}

// handleSubmitVerdict implements the liza_submit_verdict tool
// Maps to: liza submit-verdict
func (s *Server) handleSubmitVerdict(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	verdict, err := requireString(params, "verdict")
	if err != nil {
		return nil, err
	}

	agentID, err := requireString(params, "agent_id")
	if err != nil {
		return nil, err
	}

	reason, _ := params["reason"].(string)
	impact, _ := params["impact"].(string)

	result, err := ops.SubmitVerdict(s.projectRoot, taskID, verdict, reason, agentID, impact)
	if err != nil {
		return nil, fmt.Errorf("submit verdict failed: %w", err)
	}

	return textResult(fmt.Sprintf("Verdict %s submitted for task %s", result.Verdict, result.TaskID))
}

// handleMarkBlocked implements the liza_mark_blocked tool
// Maps to: liza mark-blocked
func (s *Server) handleMarkBlocked(params map[string]any) (any, error) {
	taskID, agentID, err := requireTaskAndAgent(params)
	if err != nil {
		return nil, err
	}

	reason, err := requireString(params, "reason")
	if err != nil {
		return nil, err
	}

	questions := extractStringSlice(params, "questions")
	if len(questions) == 0 {
		return nil, fmt.Errorf("questions parameter required (1-3 questions)")
	}

	_, err = ops.MarkBlocked(s.projectRoot, taskID, reason, questions, agentID)
	if err != nil {
		return nil, fmt.Errorf("mark blocked failed: %w", err)
	}

	return textResult(fmt.Sprintf("Task %s marked as BLOCKED", taskID))
}

// handleReleaseClaim implements the liza_release_claim tool
// Maps to: liza release-claim
func (s *Server) handleReleaseClaim(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	role, err := requireString(params, "role")
	if err != nil {
		return nil, err
	}

	agentID, err := requireString(params, "agent_id")
	if err != nil {
		return nil, err
	}

	if err := authorizeClaimRelease(agentID, role, s.resolver); err != nil {
		return nil, err
	}

	reason, _ := params["reason"].(string)
	force, _ := params["force"].(bool)

	result, err := ops.ReleaseClaim(s.projectRoot, taskID, role, force, reason, agentID)
	if err != nil {
		return nil, fmt.Errorf("release claim failed: %w", err)
	}

	var msg string
	switch {
	case result.ReleasedReviewer && result.ReleasedDoer:
		msg = "Released reviewer and doer claims for task " + result.TaskID
	case result.ReleasedReviewer:
		msg = "Released reviewer claim for task " + result.TaskID
	case result.ReleasedDoer:
		msg = "Released doer claim for task " + result.TaskID
	default:
		msg = "Claim released for task " + result.TaskID
	}
	return textResult(msg)
}

// handleSupersede implements the liza_supersede_task tool
// Maps to: liza supersede-task
func (s *Server) handleSupersede(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	agentID, err := s.resolveOrchestratorID(params)
	if err != nil {
		return nil, err
	}

	if err := isOperationAllowed(s.resolver, s.pipelineLoadErr, agentID, "liza_supersede_task"); err != nil {
		return nil, err
	}

	reason, err := requireString(params, "reason")
	if err != nil {
		return nil, err
	}

	replacementIDs := extractStringSlice(params, "replacement_ids")

	result, err := ops.SupersedeTask(s.projectRoot, taskID, replacementIDs, reason, agentID)
	if err != nil {
		return nil, fmt.Errorf("supersede task failed: %w", err)
	}

	return textResult(appendWarnings(
		fmt.Sprintf("Task %s superseded (was %s)", result.TaskID, result.OriginalStatus),
		result.Warnings,
	))
}

// handleCancelTask implements the liza_cancel_task tool
// Maps to: liza cancel-task
func (s *Server) handleCancelTask(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	agentID, err := s.resolveOrchestratorID(params)
	if err != nil {
		return nil, err
	}

	if err := isOperationAllowed(s.resolver, s.pipelineLoadErr, agentID, "liza_cancel_task"); err != nil {
		return nil, err
	}

	reason, err := requireString(params, "reason")
	if err != nil {
		return nil, err
	}

	result, err := ops.CancelTask(s.projectRoot, taskID, reason, agentID)
	if err != nil {
		return nil, fmt.Errorf("cancel task failed: %w", err)
	}

	return textResult(appendWarnings(
		fmt.Sprintf("Task %s cancelled (was %s)", result.TaskID, result.OriginalStatus),
		result.Warnings,
	))
}

// handleAssessBlocked implements the liza_assess_blocked tool
// Maps to: liza assess-blocked
func (s *Server) handleAssessBlocked(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	agentID, err := s.resolveOrchestratorID(params)
	if err != nil {
		return nil, err
	}

	note, _ := params["note"].(string)

	result, err := ops.AssessBlocked(s.projectRoot, taskID, note, agentID)
	if err != nil {
		return nil, fmt.Errorf("assess blocked failed: %w", err)
	}

	return textResult(fmt.Sprintf("Task %s assessed by orchestrator", result.TaskID))
}

// handleAssessHypothesisExhausted implements the liza_assess_hypothesis_exhausted tool
// Maps to: liza assess-hypothesis-exhausted
func (s *Server) handleAssessHypothesisExhausted(params map[string]any) (any, error) {
	taskID, err := requireString(params, "task_id")
	if err != nil {
		return nil, err
	}

	agentID, err := s.resolveOrchestratorID(params)
	if err != nil {
		return nil, err
	}

	note, _ := params["note"].(string)

	result, err := ops.AssessHypothesisExhausted(s.projectRoot, taskID, note, agentID)
	if err != nil {
		return nil, fmt.Errorf("assess hypothesis-exhausted failed: %w", err)
	}

	return textResult(fmt.Sprintf("Task %s assessed by orchestrator (hypothesis-exhausted)", result.TaskID))
}

// handleDeleteAgent implements the liza_delete_agent tool
// Maps to: liza delete agent
func (s *Server) handleDeleteAgent(params map[string]any) (any, error) {
	targetAgentID, err := requireString(params, "target_agent_id")
	if err != nil {
		return nil, err
	}

	reason, err := requireString(params, "reason")
	if err != nil {
		return nil, err
	}

	force, _ := params["force"].(bool)

	result, err := ops.DeleteAgent(s.projectRoot, targetAgentID, force, false, reason)
	if err != nil {
		return nil, fmt.Errorf("delete agent failed: %w", err)
	}

	return textResult(fmt.Sprintf("Agent %s deleted", result.AgentID))
}

// handleSetDiscoveryDisposition implements the liza_set_discovery_disposition tool.
func (s *Server) handleSetDiscoveryDisposition(params map[string]any) (any, error) {
	discoveryID, err := requireString(params, "discovery_id")
	if err != nil {
		return nil, err
	}

	disposition, err := requireString(params, "disposition")
	if err != nil {
		return nil, err
	}

	if err := ops.SetDiscoveryDisposition(s.projectRoot, discoveryID, disposition); err != nil {
		return nil, fmt.Errorf("set discovery disposition failed: %w", err)
	}

	return textResult(fmt.Sprintf("Discovery %s disposition set to %q", discoveryID, disposition))
}
