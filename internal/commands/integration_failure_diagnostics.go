package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/models"
)

// diagnosticUnknown is the value shown for a diagnostic field that is absent or
// empty, and the fallback operation's name.
const diagnosticUnknown = "unknown"

// Keys of an integration-failure diagnostic. The ops writer persists the
// structured diagnostic; the fallback below builds the same shape for a legacy
// state, and the alert text reads three of the keys.
const (
	diagnosticKeyOperation          = "operation"
	diagnosticKeyReason             = "reason"
	diagnosticKeyStatus             = "status"
	diagnosticKeyRecoveryHint       = "recovery_hint"
	diagnosticKeyWorktreeState      = "worktree_state"
	diagnosticKeyWorktreeRecorded   = "worktree_recorded"
	diagnosticKeyWorktree           = "worktree"
	diagnosticKeyWorktreeMissing    = "worktree_missing"
	diagnosticKeyWorktreeCheckError = "worktree_check_error"
	diagnosticKeyFailedBy           = "failed_by"
	// historyExtraDiagnostic and historyExtraPRURL are keys of the history entry's
	// Extra map that the ops writer fills.
	historyExtraDiagnostic = "diagnostic"
	historyExtraPRURL      = "pr_url"
)

// worktreeState is what the fallback diagnostic reports about the task's worktree.
type worktreeState string

const (
	worktreeStateNotRecorded       worktreeState = "not_recorded"
	worktreeStateRecordedUnchecked worktreeState = "recorded_unchecked"
	worktreeStatePresent           worktreeState = "present"
	worktreeStateMissing           worktreeState = "missing"
	worktreeStateUnknown           worktreeState = diagnosticUnknown
)

const (
	integrationFailureFallbackOperation = diagnosticUnknown
	integrationFailureFallbackReason    = "legacy INTEGRATION_FAILED state has no structured diagnostic"
	integrationFailureFallbackHint      = "inspect task history; if a PR was merged externally, run reconcile-merged with --merge-commit and --pr-url; otherwise use supersede-task/cancel-task or recreate the worktree before retrying"
)

func latestIntegrationFailureDiagnostic(task *models.Task, projectRoot string) map[string]any {
	if len(task.IntegrationFailure) > 0 {
		return cloneStringAnyMap(task.IntegrationFailure)
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventIntegrationFailed || entry.Extra == nil {
			continue
		}
		diagnostic, ok := entry.Extra[historyExtraDiagnostic].(map[string]any)
		if ok {
			return cloneStringAnyMap(diagnostic)
		}
	}
	if task.Status != models.TaskStatusIntegrationFailed {
		return nil
	}
	return fallbackIntegrationFailureDiagnostic(task, projectRoot)
}

func fallbackIntegrationFailureDiagnostic(task *models.Task, projectRoot string) map[string]any {
	diagnostic := map[string]any{
		diagnosticKeyOperation:    integrationFailureFallbackOperation,
		diagnosticKeyReason:       integrationFailureFallbackReason,
		diagnosticKeyStatus:       string(task.Status),
		diagnosticKeyRecoveryHint: integrationFailureFallbackHint,
	}
	if task.Worktree == nil || *task.Worktree == "" {
		diagnostic[diagnosticKeyWorktreeState] = string(worktreeStateNotRecorded)
		diagnostic[diagnosticKeyWorktreeRecorded] = false
	} else {
		diagnostic[diagnosticKeyWorktree] = *task.Worktree
		diagnostic[diagnosticKeyWorktreeRecorded] = true
		addWorktreePresence(projectRoot, *task.Worktree, diagnostic)
	}
	if len(task.FailedBy) > 0 {
		diagnostic[diagnosticKeyFailedBy] = append([]string(nil), task.FailedBy...)
	}
	return diagnostic
}

func addWorktreePresence(projectRoot, worktree string, diagnostic map[string]any) {
	if projectRoot == "" {
		diagnostic[diagnosticKeyWorktreeState] = string(worktreeStateRecordedUnchecked)
		return
	}
	worktreePath := filepath.Join(projectRoot, worktree)
	if _, err := os.Stat(worktreePath); err == nil {
		diagnostic[diagnosticKeyWorktreeState] = string(worktreeStatePresent)
		diagnostic[diagnosticKeyWorktreeMissing] = false
	} else if os.IsNotExist(err) {
		diagnostic[diagnosticKeyWorktreeState] = string(worktreeStateMissing)
		diagnostic[diagnosticKeyWorktreeMissing] = true
	} else {
		diagnostic[diagnosticKeyWorktreeState] = string(worktreeStateUnknown)
		diagnostic[diagnosticKeyWorktreeCheckError] = err.Error()
	}
}

func latestIntegrationPRURL(task *models.Task) *string {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Extra == nil {
			continue
		}
		switch entry.Event {
		case models.TaskEventIntegrationFailed, models.TaskEventMerged:
		default:
			continue
		}
		value, ok := entry.Extra[historyExtraPRURL].(string)
		if !ok || value == "" {
			continue
		}
		return &value
	}
	return nil
}

func integrationFailureAlertMessage(task *models.Task, projectRoot string) string {
	diagnostic := latestIntegrationFailureDiagnostic(task, projectRoot)
	if diagnostic == nil {
		return task.ID
	}
	return fmt.Sprintf("%s — operation=%s reason=%s recovery_hint=%s",
		task.ID,
		stringDiagnosticValue(diagnostic, diagnosticKeyOperation),
		stringDiagnosticValue(diagnostic, diagnosticKeyReason),
		stringDiagnosticValue(diagnostic, diagnosticKeyRecoveryHint))
}

func stringDiagnosticValue(diagnostic map[string]any, key string) string {
	value, ok := diagnostic[key]
	if !ok || value == nil {
		return diagnosticUnknown
	}
	if s, ok := value.(string); ok && s != "" {
		return s
	}
	return fmt.Sprint(value)
}

func cloneStringAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
