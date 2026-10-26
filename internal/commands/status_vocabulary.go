package commands

import "github.com/liza-mas/liza/internal/agent"

// statusFormat is an output format of the status command. Any other value,
// including the empty string, renders the dashboard.
type statusFormat string

const (
	statusFormatJSON statusFormat = "json"
	statusFormatYAML statusFormat = "yaml"
)

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
const wakeTriggerIntegrationFailed agent.OrchestratorWakeTrigger = "INTEGRATION_FAILED"

const (
	// phaseHandoffMissingTaskStatus marks a planned task ID absent from state.
	phaseHandoffMissingTaskStatus = "MISSING"
	// processStatusRunning is the display status of a live, matching agent process.
	processStatusRunning = "running"
)
