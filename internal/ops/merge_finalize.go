package ops

import (
	"fmt"
	"maps"
	"strings"
	"time"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/projectdetect"
)

// indexRefreshTriggerMerge names the merge as the cause of a repo-root index
// refresh.
const indexRefreshTriggerMerge = "merge"

// postMergeSetup is the post_worktree_cmd detection a merge may use to fill an
// unset configuration value.
type postMergeSetup struct {
	cmd         string
	nodeSubdirs []string
	ambiguous   bool
}

// detectPostMergeSetup runs only while post_worktree_cmd is unset. The merge
// transaction rechecks the value before filling it.
func detectPostMergeSetup(projectRoot string, configured *string) postMergeSetup {
	var setup postMergeSetup
	if configured != nil {
		return setup
	}
	setup.cmd = projectdetect.DetectPostWorktreeCmd(projectRoot)
	if setup.cmd == "" {
		setup.nodeSubdirs = projectdetect.DetectNodeSubdirs(projectRoot)
		setup.ambiguous = len(setup.nodeSubdirs) > 1
	}
	return setup
}

// restoreCheckout restores the working tree when the checked-out branch differs
// from the integration branch. Failure is a warning.
func (m *mergeInvocation) restoreCheckout() {
	if m.restoreRef == "" {
		return
	}
	syncErr := withIntegrationMutationLock(m.projectRoot, integrationLockRestore+m.taskID, func() error {
		return m.git.RestoreSyncedFiles(m.outcome.preMergeHEAD, m.mergeCommit, m.restoreRef)
	})
	if syncErr != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("failed to restore working tree: %v", syncErr))
	}
}

// commitMerged moves the task to MERGED. State commits before worktree cleanup:
// if the write fails the worktree still exists for investigation, whereas the
// reverse order would lose the worktree while state still says APPROVED.
func (m *mergeInvocation) commitMerged() error {
	if mergeFinalStateTestHook != nil {
		mergeFinalStateTestHook()
	}
	err := modifyLifecycleState(m.bb, m.authority, func(s *models.State) error {
		t := s.FindTask(m.taskID)
		if t == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: m.taskID}
		}
		if err := ValidateLifecyclePreparation(t, m.request); err != nil {
			return err
		}
		// Re-validate status under lock to prevent concurrent transition
		if !models.IsApprovedForMerge(t, m.pb.pr) {
			return fmt.Errorf("task %s status changed concurrently (now %s)", m.taskID, t.Status)
		}
		if err := pipelineTransition(t, models.TaskStatusMerged, m.pb); err != nil {
			return err
		}
		m.fillDetectedPostWorktreeCmd(s)
		mergeCommit := m.mergeCommit
		t.Worktree = nil
		t.MergeCommit = &mergeCommit
		t.IntegrationFailure = nil
		// Deferred commands enter the goal ledger here, before post-merge
		// archival can move the receipt out of live state (ADR-0204).
		recordMergedDeferrals(s, t, mergeCommit, time.Now().UTC())
		m.releaseMergedAgent(s, t)
		recordMergeCompletion(t, m.agentID, mergeCommit, m.historyExtra())

		var completeErr error
		m.lifecycle, completeErr = CompleteLifecycleRequest(t, m.request, models.LifecycleProjection{ReviewCommit: m.expectedCommit, MergeCommit: mergeCommit}, m.state.Agents)
		return completeErr
	})
	if err != nil {
		return fmt.Errorf("failed to update state to MERGED: %w", err)
	}
	m.effects = LifecycleEffectsCommitted
	return nil
}

// fillDetectedPostWorktreeCmd fills post_worktree_cmd only while it is still
// unset inside the MERGED transaction, so detection never overwrites an
// explicit configuration.
func (m *mergeInvocation) fillDetectedPostWorktreeCmd(s *models.State) {
	if s.Config.PostWorktreeCmd == nil && m.setup.cmd != "" {
		cmd := m.setup.cmd
		s.Config.PostWorktreeCmd = &cmd
		m.autoConfiguredSetup = true
	}
}

// releaseMergedAgent releases the assigned agent — only if still working on this
// task. After submission the coder's CurrentTask is cleared; if the coder has
// since claimed another task we must not blow them to IDLE.
func (m *mergeInvocation) releaseMergedAgent(s *models.State, t *models.Task) {
	if t.AssignedTo == nil {
		return
	}
	if a, ok := s.Agents[*t.AssignedTo]; ok {
		if a.CurrentTask != nil && *a.CurrentTask == m.taskID {
			s.ReleaseAgent(*t.AssignedTo)
		}
	}
}

// historyExtra is the merge-gate detail of the MERGED history entry.
func (m *mergeInvocation) historyExtra() map[string]any {
	extra := map[string]any{"tests_ran": m.tests.ran}
	if len(m.mergeExtra) > 0 && m.mergeExtra[0] != nil {
		maps.Copy(extra, m.mergeExtra[0])
	}
	return extra
}

// recordMergeCompletion appends the merge's two persisted local-time records:
// the completion handoff event (audit trail for sprint analysis) and the MERGED
// history entry. These are the C5 timestamp sites (Scope 16); both stay here, in
// one function.
func recordMergeCompletion(t *models.Task, agentID, mergeCommit string, extra map[string]any) {
	t.HandoffEvents = append(t.HandoffEvents, models.HandoffEvent{
		Timestamp: time.Now(),
		Agent:     agentID,
		Trigger:   models.HandoffTriggerCompletion,
	})
	t.History = append(t.History, models.TaskHistoryEntry{
		Time:   time.Now(),
		Event:  models.TaskEventMerged,
		Agent:  &agentID,
		Commit: &mergeCommit,
		Extra:  extra,
	})
}

// runPostCommitEffects performs the follow-up work after MERGED is committed.
// Every failure is a warning in the order the steps run: state is already
// final, so none may refuse the merge.
func (m *mergeInvocation) runPostCommitEffects() {
	if m.setup.ambiguous && !m.autoConfiguredSetup {
		m.warnings = append(m.warnings, fmt.Sprintf(
			"post_worktree_cmd remains unset: detected Node projects in %s; configure manually",
			strings.Join(m.setup.nodeSubdirs, ", "),
		))
	}

	m.warnings = append(m.warnings, retireMergedTask(m.bb, m.git, m.taskID)...)

	// Update sprint metrics — non-fatal
	if _, err := UpdateSprintMetrics(m.projectRoot); err != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("failed to update sprint metrics: %v", err))
	}

	// Report obligation drift this merge introduced — non-fatal, and after the
	// state commit on purpose. A merge is the only way the content an approved
	// obligation rests on can move, and surfacing it must never be able to
	// refuse the merge that surfaced it.
	if drifted, err := RecordObligationContentDrift(m.bb, m.projectRoot, m.mergeCommit, m.agentID); err != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("failed to check obligation drift: %v", err))
	} else {
		for _, section := range drifted {
			m.warnings = append(m.warnings, fmt.Sprintf(
				"obligation content drifted since approval: %s — recorded for review", section))
		}
	}

	// Refresh repo-root indexes for the merged content — non-fatal. wt-merge
	// moves the integration branch with update-ref, which fires no Git hook.
	if err := startIndexRefresh(m.projectRoot, indexRefreshTriggerMerge); err != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("failed to start repo-root index refresh: %v", err))
	}

	// Start a periodic deferred-validation batch when one is due — non-fatal;
	// the next merge or integration reconciliation reconsiders (ADR-0204).
	if _, err := StartDeferredValidationIfDue(m.projectRoot); err != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("failed to start deferred validation: %v", err))
	}

	m.archiveTerminalRecords()
}

// archiveTerminalRecords shrinks live state by archiving terminal acceptance
// receipts and tasks — non-fatal. Only a newly committed merge gets here (a
// replay returns earlier), and the write carries the caller's authority so the
// generation fence still holds.
func (m *mergeInvocation) archiveTerminalRecords() {
	if m.lifecycle.Outcome != models.LifecycleCompleted {
		return
	}
	if postMergeArchiveTestHook != nil {
		postMergeArchiveTestHook()
	}
	if _, err := archiveTerminalAcceptanceReceipts(m.projectRoot, m.authority, DefaultArchiveLimit, m.opts); err != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("failed to archive terminal acceptance receipts: %v", err))
	}
	if _, err := archiveTerminalTasks(m.projectRoot, m.authority, DefaultArchiveLimit, m.opts); err != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("failed to archive terminal tasks: %v", err))
	}
}

// result is the successful merge's outcome.
func (m *mergeInvocation) result() *MergeResult {
	return &MergeResult{
		LifecycleOutcome:  m.lifecycle,
		TaskID:            m.taskID,
		MergeCommit:       m.mergeCommit,
		FastForward:       m.outcome.fastForward,
		TestsRan:          m.tests.ran,
		NoTestScriptFound: m.tests.notFound,
		TestOutput:        m.tests.output,
		Warnings:          m.warnings,
	}
}
