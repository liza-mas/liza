package models

// TransitionCardinality is how many target tasks a pipeline transition creates
// per source task. The set is closed; pipeline config validation refuses any
// other value.
type TransitionCardinality string

// Transition cardinalities accepted by pipeline transition definitions.
const (
	CardinalityPerSubtask TransitionCardinality = "per-subtask"
	CardinalityOneToOne   TransitionCardinality = "one-to-one"
	CardinalityManyToOne  TransitionCardinality = "many-to-one"
)

// TransitionTrigger says whether a pipeline transition runs on operator
// command or automatically. The set is closed.
type TransitionTrigger string

// Transition triggers accepted by pipeline transition definitions.
const (
	TransitionTriggerManual TransitionTrigger = "manual"
	TransitionTriggerAuto   TransitionTrigger = "auto"
)

// AllocationRoute names the exclusive architecture route a pipeline
// transition belongs to. The empty route is the legacy, unconditional
// transition.
type AllocationRoute string

// Allocation routes accepted by pipeline transition definitions.
const (
	AllocationRouteNone               AllocationRoute = ""
	AllocationRouteScopeDecomposition AllocationRoute = "scope-decomposition"
	AllocationRouteCodingAllocation   AllocationRoute = "coding-allocation"
)
