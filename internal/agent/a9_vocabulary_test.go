package agent

import (
	"os"
	"testing"
	"time"
)

// The K7 sweep of A9 named literals that already existed. These tests pin each
// spelling and value, so a rename or retyping cannot change what is written to
// alerts.log, read from provider output or passed to a provider.

func TestA9NamedVocabularyKeepsStringValues(t *testing.T) {
	tests := map[string][2]string{
		"alertLevelCritical":                   {alertLevelCritical, "🚨"},
		"alertLevelWarning":                    {alertLevelWarning, "⚠️"},
		"alertCategoryProgressTimeout":         {alertCategoryProgressTimeout, "EXECUTION PROGRESS TIMEOUT"},
		"alertCategoryProviderAuditDegraded":   {alertCategoryProviderAuditDegraded, "PROVIDER AUDIT DEGRADED"},
		"alertCategoryBlockedTasksUnresolved":  {alertCategoryBlockedTasksUnresolved, "BLOCKED_TASKS UNRESOLVED"},
		"alertCategoryAbandonedBackgroundJob":  {alertCategoryAbandonedBackgroundJob, "ABANDONED BACKGROUND JOB"},
		"alertCategoryAbandonedBackgroundLoop": {alertCategoryAbandonedBackgroundLoop, "ABANDONED BACKGROUND JOB LOOP"},
		"alertCategoryCrashRestartLoop":        {alertCategoryCrashRestartLoop, "CRASH RESTART LOOP"},
		"alertCategorySpinning":                {alertCategorySpinning, "SPINNING"},
		"alertCategoryOrchestratorSpinning":    {alertCategoryOrchestratorSpinning, "ORCHESTRATOR_SPINNING"},
		"alertCategoryToolFailureRetryLoop":    {alertCategoryToolFailureRetryLoop, "TOOL FAILURE RETRY LOOP"},
		"alertCategoryIndexHooksUnavailable":   {alertCategoryIndexHooksUnavailable, "INDEX HOOKS UNAVAILABLE"},
		"alertCategoryCheckpointSummaryFailed": {alertCategoryCheckpointSummaryFailed, "CHECKPOINT SUMMARY FAILED"},
		"streamJSONObjectPrefix":               {streamJSONObjectPrefix, "{"},
		"streamTaskNotification":               {streamTaskNotification, "task_notification"},
		"streamToolResult":                     {streamToolResult, "tool_result"},
		"acpSessionUpdateKey":                  {acpSessionUpdateKey, "sessionUpdate"},
		"acpSessionUpdateMessageChunk":         {acpSessionUpdateMessageChunk, "agent_message_chunk"},
		"submitVerdictFailedCode":              {submitVerdictFailedCode, "submit_verdict_failed"},
		"submitVerdictCommand":                 {submitVerdictCommand, "submit-verdict"},
		"codexVersionProgram":                  {codexVersionProgram, "codex-cli"},
	}
	for name, v := range tests {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", name, v[0], v[1])
		}
	}
}

func TestA9NamedVocabularyKeepsNumericValues(t *testing.T) {
	tests := map[string][2]int64{
		"pausePollInterval":                   {int64(pausePollInterval), int64(5 * time.Second)},
		"progressPollDivisor":                 {progressPollDivisor, 4},
		"minProgressPollInterval":             {int64(minProgressPollInterval), int64(100 * time.Millisecond)},
		"maxProgressPollInterval":             {int64(maxProgressPollInterval), int64(15 * time.Second)},
		"supervisorRunIDBytes":                {supervisorRunIDBytes, 16},
		"entryPointValueParts":                {entryPointValueParts, 2},
		"minOrderableCandidates":              {minOrderableCandidates, 2},
		"hypothesisExhaustionMinFailures":     {hypothesisExhaustionMinFailures, 2},
		"claudeToolResultMinMajor":            {claudeToolResultMinMajor, 2},
		"claudeToolResultMinMinor":            {claudeToolResultMinMinor, 1},
		"claudeToolResultMinPatch":            {claudeToolResultMinPatch, 267},
		"claudeToolResultHookTimeoutSeconds":  {claudeToolResultHookTimeoutSeconds, 60},
		"claudeVersionMatchLen":               {claudeVersionMatchLen, 4},
		"toolResultVersionProbeTimeout":       {int64(toolResultVersionProbeTimeout), int64(5 * time.Second)},
		"codexVersionFields":                  {codexVersionFields, 2},
		"codexVersionComponents":              {codexVersionComponents, 3},
		"codexToolResultMinMinor":             {codexToolResultMinMinor, 154},
		"runtimeDirMode":                      {int64(runtimeDirMode), int64(os.FileMode(0o755))},
		"runtimeFileMode":                     {int64(runtimeFileMode), int64(os.FileMode(0o644))},
		"toolResultScriptMode":                {int64(toolResultScriptMode), int64(os.FileMode(0o700))},
		"exitCodeGracefulAbort (quota check)": {exitCodeGracefulAbort, 42},
	}
	for name, v := range tests {
		if v[0] != v[1] {
			t.Errorf("%s = %d, want %d", name, v[0], v[1])
		}
	}
}

// The progress watchdog polls a quarter of its timeout, clamped to the bounds.
func TestA9ProgressPollIntervalClamps(t *testing.T) {
	tests := []struct {
		timeout time.Duration
		want    time.Duration
	}{
		{time.Millisecond, 100 * time.Millisecond},
		{time.Minute, 15 * time.Second},
		{40 * time.Second, 10 * time.Second},
	}
	for _, tt := range tests {
		if got := progressPollInterval(tt.timeout); got != tt.want {
			t.Errorf("progressPollInterval(%v) = %v, want %v", tt.timeout, got, tt.want)
		}
	}
}
