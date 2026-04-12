package ops

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// VerdictResult contains the outcome of a successful verdict submission.
type VerdictResult struct {
	TaskID              string `json:"task_id"`
	Verdict             string `json:"verdict"` // "APPROVED" or "REJECTED"
	AgentID             string `json:"agent_id"`
	Reason              string `json:"reason"` // non-empty for rejections
	EscalatedToBlocked  bool   `json:"escalated_to_blocked"`
	BlockedReason       string `json:"blocked_reason"`
	NewAttemptTriggered bool   `json:"new_attempt_triggered"`
}

// impactOrder defines the ordering for impact levels.
// Higher index = higher impact. Used by ResolveEffectiveImpact.
var impactOrder = map[string]int{
	"standard":     0,
	"significant":  1,
	"architecture": 2,
}

// IsValidImpact returns whether v is a recognized impact classification.
// Empty string is valid (means "not specified").
func IsValidImpact(v string) bool {
	if v == "" {
		return true
	}
	_, ok := impactOrder[v]
	return ok
}

// ResolveEffectiveImpact scans checkpoint and verdict history entries since the
// last rejection, returning the maximum impact found.
// Ordering: standard < significant < architecture; default: "standard".
func ResolveEffectiveImpact(history []models.TaskHistoryEntry) string {
	maxImpact := "standard"
	maxRank := 0

	// Iterate in reverse; stop at the last rejection boundary.
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]

		if entry.Event == models.TaskEventRejected {
			break // rejection resets the cycle
		}

		// Only checkpoint and verdict entries contribute impact.
		if entry.Event != models.TaskEventPreExecutionCheckpoint && entry.Event != models.TaskEventApproved {
			continue
		}

		if v, ok := entry.Extra["impact"].(string); ok && v != "" {
			if rank, known := impactOrder[v]; known && rank > maxRank {
				maxRank = rank
				maxImpact = v
			}
		}
	}

	return maxImpact
}

// SubmitVerdict atomically applies a review verdict: APPROVED transitions to
// APPROVED or PARTIALLY_APPROVED status (based on quorum), REJECTED increments
// review cycles and requires a reason. The optional impact parameter records
// the reviewer's impact classification; it cannot downgrade the effective impact.
// No terminal I/O.
func SubmitVerdict(projectRoot, taskID, verdict, reason, agentID, impact string) (*VerdictResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if verdict == "" {
		return nil, &PreconditionError{Reason: "verdict is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "LIZA_AGENT_ID is required"}
	}

	verdict = strings.ToUpper(verdict)
	if verdict != "APPROVED" && verdict != "REJECTED" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("verdict must be APPROVED or REJECTED, got: %s", verdict)}
	}

	if verdict == "REJECTED" && reason == "" {
		return nil, &PreconditionError{Reason: "rejection reason is required for REJECTED verdict"}
	}

	if !IsValidImpact(impact) {
		return nil, &PreconditionError{Reason: fmt.Sprintf("invalid impact value: %s (must be standard, significant, or architecture)", impact)}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	if _, err := identity.ExtractRole(agentID); err != nil {
		return nil, fmt.Errorf("invalid agent ID %s: %w", agentID, err)
	}

	// Phase 1: Read state and validate preconditions
	_, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	// Resolve expected statuses from pipeline config
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", resolverErr)
	}
	if task.RolePair == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", taskID)}
	}
	expectedReviewingStatus, err := resolver.ReviewingStatus(task.RolePair)
	if err != nil {
		return nil, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	// Also accept reviewing_2 (second review in quorum flow).
	expectedReviewing2Status, _ := resolver.Reviewing2Status(task.RolePair)
	approvedStatus, err := resolver.ApprovedStatus(task.RolePair)
	if err != nil {
		return nil, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	rejectedStatus, err := resolver.RejectedStatus(task.RolePair)
	if err != nil {
		return nil, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}

	// Resolve quorum states (optional — may not exist if quorum is always 1)
	partiallyApprovedStatus, _ := resolver.PartiallyApprovedStatus(task.RolePair)

	// Resolve clean state (optional — only integration-pair declares this)
	cleanStatus, _ := resolver.CleanStatus(task.RolePair)

	pipelineTransitions := BuildPipelineTransitions(resolver)

	// Fast-fail before git operations; re-checked authoritatively inside Modify.
	isReviewing := task.Status == expectedReviewingStatus ||
		(expectedReviewing2Status != "" && task.Status == expectedReviewing2Status)
	if !isReviewing {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s is not in a reviewing state (current status: %s)", taskID, task.Status)}
	}

	// Resolve effective impact from history and enforce escalation.
	effectiveImpact := ResolveEffectiveImpact(task.History)
	if impact != "" {
		// Enforce: verdict impact must be >= resolved effective impact (never downgrade)
		if impactOrder[impact] < impactOrder[effectiveImpact] {
			return nil, &PreconditionError{Reason: fmt.Sprintf("cannot downgrade impact from %q to %q — impact can only escalate", effectiveImpact, impact)}
		}
		effectiveImpact = impact
	}

	// Phase 2: Validate ReviewCommit exists and matches worktree HEAD
	if task.ReviewCommit == nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no review_commit — cannot submit verdict", taskID)}
	}

	g := git.New(projectRoot)
	wtPath := g.GetWorktreePath(taskID)
	if _, statErr := os.Stat(wtPath); os.IsNotExist(statErr) {
		// Worktree absent on disk (e.g. tests without real worktrees) — skip check.
	} else if statErr != nil {
		return nil, fmt.Errorf("failed to stat worktree %s: %w", wtPath, statErr)
	} else {
		wtHEAD, headErr := g.GetWorktreeHEAD(taskID)
		if headErr != nil {
			return nil, fmt.Errorf("failed to get worktree HEAD: %w", headErr)
		}
		if *task.ReviewCommit != wtHEAD {
			return nil, &PreconditionError{Reason: fmt.Sprintf("review_commit %s does not match worktree HEAD %s — worktree was modified after submission", *task.ReviewCommit, wtHEAD)}
		}
	}

	// Phase 3: Atomic state update
	now := time.Now().UTC()
	escalatedToBlocked := false
	blockedReasonOut := ""
	newAttemptNeeded := false
	newAttemptReason := ""

	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		isReviewingAuth := task.Status == expectedReviewingStatus ||
			(expectedReviewing2Status != "" && task.Status == expectedReviewing2Status)
		if !isReviewingAuth {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not in a reviewing state (current status: %s)", taskID, task.Status)}
		}

		transitionTask := func(to models.TaskStatus) error {
			return task.TransitionWith(to, pipelineTransitions)
		}

		if verdict == "APPROVED" {
			// Build approval from agent registry and append to approvals list
			provider := ""
			if agent, ok := state.Agents[agentID]; ok {
				provider = agent.Provider
			}
			task.Approvals = append(task.Approvals, models.Approval{
				Agent:     agentID,
				Provider:  provider,
				Timestamp: now,
			})

			// Build history entry with optional impact in Extra
			historyEntry := models.TaskHistoryEntry{
				Time:  now,
				Event: models.TaskEventApproved,
				Agent: &agentID,
			}
			if impact != "" {
				historyEntry.Extra = map[string]any{"impact": impact}
			}
			task.History = append(task.History, historyEntry)

			// Evaluate quorum: determine if more approvals are needed.
			effectiveQuorum, qErr := resolver.EffectiveQuorum(task.RolePair, effectiveImpact)
			if qErr != nil {
				return fmt.Errorf("failed to resolve quorum: %w", qErr)
			}

			if task.ApprovalCount() < effectiveQuorum {
				// Quorum not met — need partially_approved state to continue
				if partiallyApprovedStatus == "" {
					return fmt.Errorf("quorum %d requires partially-approved state but none declared for %q", effectiveQuorum, task.RolePair)
				}
				if err := transitionTask(partiallyApprovedStatus); err != nil {
					return err
				}
			} else {
				// Quorum met — route to clean or approved
				targetStatus := approvedStatus
				if cleanStatus != "" && len(task.Output) == 0 {
					targetStatus = cleanStatus
				}
				if err := transitionTask(targetStatus); err != nil {
					return err
				}
			}

			// Derived field for backward compatibility
			task.ApprovedBy = &agentID
			task.RejectionReason = nil
		} else {
			if err := transitionTask(rejectedStatus); err != nil {
				return err
			}

			// Rejection at any stage clears all approvals (spec: both reviewers re-review)
			task.ClearApprovals()
			task.ApprovedBy = nil

			task.RejectionReason = &reason
			task.ReviewCyclesCurrent++
			task.ReviewCyclesTotal++

			task.History = append(task.History, models.TaskHistoryEntry{
				Time:   now,
				Event:  models.TaskEventRejected,
				Agent:  &agentID,
				Reason: &reason,
			})

			// Refresh lease — coder needs time to address rejection.
			// If escalation triggers below, lease is cleared along with assignment.
			renewLease(state, task)

			reviewLimit := effectiveReviewCycleLimit(state.Config)
			iterationLimit := effectiveCoderIterationLimit(task, state.Config)

			escalation, shouldEscalate := classifyLimitEscalation(
				task.ReviewCyclesCurrent,
				reviewLimit,
				task.Iteration,
				iterationLimit,
				task.EffectiveAttempt(),
			)
			if shouldEscalate {
				switch escalation.action {
				case LimitActionBlocked:
					if err := transitionTask(models.TaskStatusBlocked); err != nil {
						return err
					}

					blockedReason := escalation.reason
					task.BlockedReason = &blockedReason
					task.BlockedQuestions = escalation.questions
					task.LeaseExpires = nil
					escalatedToBlocked = true
					blockedReasonOut = blockedReason

					if task.AssignedTo != nil {
						assignedCoder := *task.AssignedTo
						if assignedCoder != agentID {
							if a, ok := state.Agents[assignedCoder]; ok {
								if a.CurrentTask != nil && *a.CurrentTask == taskID {
									state.ReleaseAgent(assignedCoder)
								}
							}
						}
					}
					task.AssignedTo = nil

					task.History = append(task.History, models.TaskHistoryEntry{
						Time:   now,
						Event:  models.TaskEventBlocked,
						Agent:  &agentID,
						Reason: &blockedReason,
					})
				case LimitActionNewAttempt:
					// Capture for post-Modify call — cannot nest bb.Modify
					newAttemptNeeded = true
					newAttemptReason = escalation.reason
				}
			}
		}

		task.ReviewingBy = nil
		task.ReviewLeaseExpires = nil
		state.ReleaseAgent(agentID)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to submit verdict: %w", err)
	}

	if newAttemptNeeded {
		_, taErr := TransitionToNewAttempt(projectRoot, taskID, newAttemptReason)
		if taErr != nil {
			return nil, fmt.Errorf("submit_verdict %s: rejection committed but attempt transition failed: %w", taskID, taErr)
		}
	}

	return &VerdictResult{
		TaskID:              taskID,
		Verdict:             verdict,
		AgentID:             agentID,
		Reason:              reason,
		EscalatedToBlocked:  escalatedToBlocked,
		BlockedReason:       blockedReasonOut,
		NewAttemptTriggered: !escalatedToBlocked && newAttemptNeeded,
	}, nil
}
