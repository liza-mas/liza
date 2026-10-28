package commands

import "github.com/liza-mas/liza/internal/prompts"

// statusFormat is an output format of the status command. Any other value,
// including the empty string, renders the dashboard.
type statusFormat string

const (
	statusFormatJSON statusFormat = "json"
	statusFormatYAML statusFormat = "yaml"
	// statusFormatDashboard is the default rendering; any value other than json
	// and yaml selects it.
	statusFormatDashboard statusFormat = "dashboard"
)

// StatusFormatNames lists the formats the status command names, in the order
// help and completion document them.
func StatusFormatNames() []string {
	return []string{string(statusFormatJSON), string(statusFormatYAML), string(statusFormatDashboard)}
}

// phaseHandoffState is the hand-off state reported in the status phase-handoff
// section.
type phaseHandoffState string

const (
	phaseHandoffReady               phaseHandoffState = "READY"
	phaseHandoffPartialReady        phaseHandoffState = "PARTIAL_READY"
	phaseHandoffCheckpointed        phaseHandoffState = "CHECKPOINTED"
	phaseHandoffCompleted           phaseHandoffState = "COMPLETED"
	phaseHandoffDispositionRequired phaseHandoffState = "DISPOSITION_REQUIRED"
	phaseHandoffRepairRequired      phaseHandoffState = "REPAIR_REQUIRED"
	phaseHandoffMergeRequired       phaseHandoffState = "MERGE_REQUIRED"
)

// wakeTriggerIntegrationFailed is a trigger name the status reason table still
// words, although agent no longer declares or emits it.
const wakeTriggerIntegrationFailed prompts.WakeTrigger = "INTEGRATION_FAILED"

// Circuit-breaker states the status and watch views read. The owner (A3 ops
// mode change and the models history check) keeps them unexported.
const (
	circuitBreakerStatusOK        = "OK"
	circuitBreakerStatusTriggered = "TRIGGERED"
)

const (
	// phaseHandoffMissingTaskStatus marks a planned task ID absent from state.
	phaseHandoffMissingTaskStatus = "MISSING"
	// processStatusRunning is the display status of a live, matching agent process.
	processStatusRunning = "running"
)
