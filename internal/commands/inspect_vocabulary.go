package commands

import "slices"

// inspectFormat is an output format of the inspect (get) family. The empty
// value selects each renderer's own default.
type inspectFormat string

const (
	inspectFormatJSON  inspectFormat = "json"
	inspectFormatYAML  inspectFormat = "yaml"
	inspectFormatTable inspectFormat = "table"
	inspectFormatValue inspectFormat = "value"
)

// inspectFormats lists the formats an inspect query accepts, plus the empty
// default.
var inspectFormats = []inspectFormat{inspectFormatJSON, inspectFormatYAML, inspectFormatTable, inspectFormatValue, ""}

// inspectTarget is an entity an inspect query can name as its first argument.
type inspectTarget string

const (
	inspectTargetConfig              inspectTarget = "config"
	inspectTargetSprint              inspectTarget = "sprint"
	inspectTargetTasks               inspectTarget = "tasks"
	inspectTargetAgents              inspectTarget = "agents"
	inspectTargetMetrics             inspectTarget = "metrics"
	inspectTargetAnomalies           inspectTarget = "anomalies"
	inspectTargetQuarantinedVerdicts inspectTarget = "quarantined_verdicts"
	inspectTargetProofReaffirmations inspectTarget = "proof_reaffirmations"
	inspectTargetHumanNotes          inspectTarget = "human_notes"
)

// inspectTargets lists every entity target, in the order the CLI documents them.
var inspectTargets = []inspectTarget{
	inspectTargetConfig, inspectTargetSprint, inspectTargetTasks, inspectTargetAgents,
	inspectTargetMetrics, inspectTargetAnomalies, inspectTargetQuarantinedVerdicts,
	inspectTargetProofReaffirmations, inspectTargetHumanNotes,
}

// Spellings cmd/liza passes to InspectOptions and completes. They derive from
// the typed constants so the CLI and the renderers cannot drift.
const (
	InspectFormatJSON      = string(inspectFormatJSON)
	InspectFormatTable     = string(inspectFormatTable)
	InspectFormatValue     = string(inspectFormatValue)
	InspectTargetTasks     = string(inspectTargetTasks)
	InspectTargetAgents    = string(inspectTargetAgents)
	InspectTargetMetrics   = string(inspectTargetMetrics)
	InspectTargetAnomalies = string(inspectTargetAnomalies)
)

// InspectFormatNames lists the output formats an inspect query names, in the
// order help and completion document them.
func InspectFormatNames() []string {
	return []string{
		string(inspectFormatJSON), string(inspectFormatYAML),
		string(inspectFormatTable), string(inspectFormatValue),
	}
}

// inspectQueryRoot is the leading segment of a dotted field or ID query. The
// plural roots are the entity targets; the singular roots address one entity.
type inspectQueryRoot string

const (
	inspectRootAgents                   = inspectQueryRoot(inspectTargetAgents)
	inspectRootTasks                    = inspectQueryRoot(inspectTargetTasks)
	inspectRootSprint                   = inspectQueryRoot(inspectTargetSprint)
	inspectRootAgent   inspectQueryRoot = "agent"
	inspectRootTask    inspectQueryRoot = "task"
	inspectRootVersion inspectQueryRoot = "version"
)

// computedField names a value a query computes from state instead of reading a
// stored field, by the entity it is computed for.
type computedField string

const (
	computedFieldActiveCount        computedField = "active_count"
	computedFieldUtilization        computedField = "utilization"
	computedFieldCompletionRate     computedField = "completion_rate"
	computedFieldAvgIterationCount  computedField = "avg_iteration_count"
	computedFieldElapsed            computedField = "elapsed"
	computedFieldRemaining          computedField = "remaining"
	computedFieldProgressPercent    computedField = "progress_percent"
	computedFieldTimeSinceHeartbeat computedField = "time_since_heartbeat"
	computedFieldTimeOnTask         computedField = "time_on_task"
	computedFieldTransitionID       computedField = "transition_id"
	computedFieldAge                computedField = "age"
	computedFieldTimeInStatus       computedField = "time_in_status"
)

// taskComputedFields are the computed fields a task query can address.
var taskComputedFields = []computedField{computedFieldTransitionID, computedFieldAge, computedFieldTimeInStatus}

func isTaskComputedField(field string) bool {
	return slices.Contains(taskComputedFields, computedField(field))
}

// Reserved state and computed paths keep precedence over task IDs.
const (
	tasksComputedQueryPrefix  = string(inspectTargetTasks) + "."
	taskQueryPrefix           = string(inspectRootTask) + "."
	queryPathSeparator        = "."
	sprintTimelinePath        = "sprint.timeline"
	taskFieldGenerationDigest = "generation_digest"
	inspectTagKey             = "inspect"
	yamlTagKey                = "yaml"
	tagOmitted                = "-"
	zeroDurationText          = "0s"
	unknownRoleLabel          = "unknown"
	percentScale              = 100
	emptyCell                 = "-"
)

// isReservedTasksComputedQuery reports whether the query names an aggregate task
// metric, which wins over a task ID that happens to start with "tasks.".
func isReservedTasksComputedQuery(query string) bool {
	return query == tasksComputedQueryPrefix+string(computedFieldCompletionRate) ||
		query == tasksComputedQueryPrefix+string(computedFieldAvgIterationCount)
}

// isStructured reports whether the format is machine-readable output.
func (f inspectFormat) isStructured() bool {
	return f == inspectFormatJSON || f == inspectFormatYAML
}

// Table and description limits shared by the task tables.
const (
	maxTableDescriptionLen = 50
	descriptionEllipsis    = "..."
)

// truncateTableDescription shortens a description to maxTableDescriptionLen
// bytes, ellipsis included.
func truncateTableDescription(desc string) string {
	if len(desc) > maxTableDescriptionLen {
		return desc[:maxTableDescriptionLen-len(descriptionEllipsis)] + descriptionEllipsis
	}
	return desc
}

func isInspectTarget(query string) bool {
	return slices.Contains(inspectTargets, inspectTarget(query))
}
