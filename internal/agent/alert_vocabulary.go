package agent

import "github.com/liza-mas/liza/internal/alerts"

// Alert levels and categories the supervisor runtime writes to alerts.log
// through providersignal.LogAlert, spelled as they appear there. Provider-signal
// alerts (quota, unavailable) are named by providersignal.
const (
	alertLevelCritical = string(alerts.AlertLevelCritical)
	alertLevelWarning  = string(alerts.AlertLevelWarning)

	alertCategoryProgressTimeout         = "EXECUTION PROGRESS TIMEOUT"
	alertCategoryProviderAuditDegraded   = "PROVIDER AUDIT DEGRADED"
	alertCategoryBlockedTasksUnresolved  = "BLOCKED_TASKS UNRESOLVED"
	alertCategoryAbandonedBackgroundJob  = "ABANDONED BACKGROUND JOB"
	alertCategoryAbandonedBackgroundLoop = "ABANDONED BACKGROUND JOB LOOP"
	alertCategoryCrashRestartLoop        = "CRASH RESTART LOOP"
	alertCategorySpinning                = "SPINNING"
	alertCategoryOrchestratorSpinning    = "ORCHESTRATOR_SPINNING"
	alertCategoryToolFailureRetryLoop    = "TOOL FAILURE RETRY LOOP"
	alertCategoryIndexHooksUnavailable   = "INDEX HOOKS UNAVAILABLE"
	alertCategoryCheckpointSummaryFailed = "CHECKPOINT SUMMARY FAILED"
)
