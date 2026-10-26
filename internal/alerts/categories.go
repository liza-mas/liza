package alerts

// Category names a kind of alert the watcher emits or matches. Alert.Category
// stays a string: it is parsed back from log lines, whose categories (those of
// lifecycle writers in other packages included) no constant here needs to name.
// Convert at construction (Category.String) and at comparison (Category(s)).
type Category string

// Categories the watch checks and pool repair emit or match. The spellings are
// the log format: an alerts.log line carries them verbatim.
const (
	CategoryApproachingLimit       Category = "APPROACHING LIMIT"
	CategoryAttempt                Category = "ATTEMPT"
	CategoryAutoRepairFailed       Category = "AUTO REPAIR FAILED"
	CategoryAutoRepairUnservable   Category = "AUTO REPAIR UNSERVABLE"
	CategoryAwaitingHuman          Category = "AWAITING HUMAN"
	CategoryBlocked                Category = "BLOCKED"
	CategoryCircuitBreaker         Category = "CIRCUIT BREAKER"
	CategoryDeadAgentProcess       Category = "DEAD AGENT PROCESS"
	CategoryHypothesisExhaustion   Category = "HYPOTHESIS EXHAUSTION"
	CategoryImmediateDiscovery     Category = "IMMEDIATE DISCOVERY"
	CategoryIntegrationFailed      Category = "INTEGRATION FAILED"
	CategoryInvalidAgentOwnership  Category = "INVALID AGENT OWNERSHIP"
	CategoryInvalidState           Category = "INVALID STATE"
	CategoryLeaseExpired           Category = "LEASE EXPIRED"
	CategoryMissingRole            Category = "MISSING ROLE"
	CategoryNoUsableProvider       Category = "NO USABLE PROVIDER"
	CategoryOrchestratorMissing    Category = "ORCHESTRATOR MISSING"
	CategoryOrphanedRejected       Category = "ORPHANED REJECTED"
	CategoryPipelineConfig         Category = "PIPELINE CONFIG"
	CategoryRegisteredAgentProcess Category = "REGISTERED AGENT PROCESS"
	CategoryReviewLeaseExpired     Category = "REVIEW LEASE EXPIRED"
	CategoryReviewLoop             Category = "REVIEW LOOP"
	CategorySprintStalled          Category = "SPRINT STALLED"
	CategoryStaleDraft             Category = "STALE DRAFT"
	CategoryStaleSentinel          Category = "STALE SENTINEL"
	CategoryStalled                Category = "STALLED"
)

// String returns the category as an Alert.Category value.
func (c Category) String() string { return string(c) }
