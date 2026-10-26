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
