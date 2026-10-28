package models

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// RejectionRCASchemaVersion is the only accepted version of both rejection-RCA
// request payloads and of the stored record.
const RejectionRCASchemaVersion = 1

// RejectionCause classifies why a rejection happened. The set is open on read:
// unrecognized values are stored verbatim and reported under "unknown" by
// telemetry; they are never silently dropped.
type RejectionCause string

// Rejection cause vocabulary.
const (
	RejectionCauseProductDefect     RejectionCause = "product_defect"
	RejectionCauseCapabilityFailure RejectionCause = "capability_failure"
	RejectionCauseLifecycleRetry    RejectionCause = "lifecycle_retry"
	RejectionCauseUnknown           RejectionCause = "unknown"
)

// IsKnownRejectionCause reports whether a cause is one this vocabulary names.
func IsKnownRejectionCause(cause RejectionCause) bool {
	switch cause {
	case RejectionCauseProductDefect, RejectionCauseCapabilityFailure,
		RejectionCauseLifecycleRetry, RejectionCauseUnknown:
		return true
	}
	return false
}

// RecoveryPath names how a gated task is recovered. Unlike causes this is a
// closed enum: an unrecognized path cannot be mapped to a restore form and is
// invalid input.
type RecoveryPath string

// Recovery vocabulary.
const (
	RecoveryImplementationCorrection RecoveryPath = "implementation_correction"
	RecoveryCapabilityReroute        RecoveryPath = "capability_reroute"
	RecoveryLifecycleRepair          RecoveryPath = "lifecycle_repair"
	RecoveryRescope                  RecoveryPath = "rescope"
	RecoveryHumanOverride            RecoveryPath = "human_override"
)

// RestoreMode is the restore form a recorded disposition authorizes for
// unblock-task.
type RestoreMode string

// Restore modes a recorded disposition authorizes for unblock-task.
const (
	RestoreModeAssign    RestoreMode = "assign"
	RestoreModeClaimable RestoreMode = "claimable"
	RestoreModeNone      RestoreMode = "none"
)

// restoreModeByRecoveryPath backs both IsRecoveryPath and
// RejectionRCARestoreMode so the closed enum and the restore mapping cannot
// drift apart. "assign" is the non-incrementing restore, which is how
// capability and lifecycle causes avoid consuming a product iteration.
var restoreModeByRecoveryPath = map[RecoveryPath]RestoreMode{
	RecoveryCapabilityReroute:        RestoreModeAssign,
	RecoveryLifecycleRepair:          RestoreModeAssign,
	RecoveryImplementationCorrection: RestoreModeClaimable,
	RecoveryHumanOverride:            RestoreModeClaimable,
	RecoveryRescope:                  RestoreModeNone,
}

// IsRecoveryPath reports whether a recovery path is one this vocabulary names.
func IsRecoveryPath(path RecoveryPath) bool {
	_, ok := restoreModeByRecoveryPath[path]
	return ok
}

// RejectionRCARestoreMode returns the restore form a recovery path authorizes,
// or an empty mode for an unknown path.
func RejectionRCARestoreMode(path RecoveryPath) RestoreMode {
	return restoreModeByRecoveryPath[path]
}

// BlockedReasonRejectionRCARequired prefixes the prose blocked_reason of a
// gated task and names its blocked_class in history. Gate state is decided by
// RejectionRCAGateOpen, never by matching this string.
const BlockedReasonRejectionRCARequired = "rejection_rca_required"

// RejectionRCAContribution links one durable rejection to bounded evidence and
// to the causes it exhibited. Categories is a set: gh-161 classifies a
// contribution as one or more causes, so mixed cases need no other encoding.
type RejectionRCAContribution struct {
	RejectionIndex int              `yaml:"rejection_index" json:"rejection_index"`
	Categories     []RejectionCause `yaml:"categories" json:"categories"`
	Evidence       []string         `yaml:"evidence,omitempty" json:"evidence,omitempty"`
}

// RejectionRCARequest is the complete set of fields a caller of
// record-rejection-rca may send. Every other field of the stored record is
// seeded by the gate or derived inside the operation, so none can be spoofed.
type RejectionRCARequest struct {
	SchemaVersion int                        `yaml:"schema_version" json:"schema_version"`
	Summary       string                     `yaml:"summary" json:"summary"`
	Contributions []RejectionRCAContribution `yaml:"contributions" json:"contributions"`
}

// RejectionRCADispositionRequest is the complete set of fields a caller of
// resume-rejection-rca may send.
type RejectionRCADispositionRequest struct {
	SchemaVersion int          `yaml:"schema_version" json:"schema_version"`
	RecoveryPath  RecoveryPath `yaml:"recovery_path" json:"recovery_path"`
	Rationale     string       `yaml:"rationale,omitempty" json:"rationale,omitempty"`
}

// RejectionRCADisposition records the authorized decision that closed a gate.
// Only RecoveryPath and Rationale come from the caller; the rest is derived
// from the invocation and the transaction clock.
type RejectionRCADisposition struct {
	RecoveryPath     RecoveryPath `yaml:"recovery_path" json:"recovery_path"`
	RestoreMode      RestoreMode  `yaml:"restore_mode" json:"restore_mode"`
	Actor            string       `yaml:"actor" json:"actor"`
	LifecycleVersion uint64       `yaml:"lifecycle_version,omitempty" json:"lifecycle_version,omitempty"`
	DecidedAt        time.Time    `yaml:"decided_at" json:"decided_at"`
	Rationale        string       `yaml:"rationale,omitempty" json:"rationale,omitempty"`
	IterationExempt  bool         `yaml:"iteration_exempt,omitempty" json:"iteration_exempt,omitempty"`
}

// RejectionRCARecord is the durable classified root-cause analysis of one
// high-churn cycle. SchemaVersion, Threshold, RejectionCount, GatedAt and
// GatingCommit are seeded when the gate fires; the caller fields stay empty
// until an RCA is recorded, and Disposition until the gate is closed. The
// record always describes the current cycle — earlier cycles survive in task
// history, which is what telemetry reads.
type RejectionRCARecord struct {
	SchemaVersion  int                        `yaml:"schema_version" json:"schema_version"`
	Threshold      int                        `yaml:"threshold" json:"threshold"`
	RejectionCount int                        `yaml:"rejection_count" json:"rejection_count"`
	GatedAt        time.Time                  `yaml:"gated_at" json:"gated_at"`
	GatingCommit   string                     `yaml:"gating_commit,omitempty" json:"gating_commit,omitempty"`
	Fingerprint    string                     `yaml:"fingerprint,omitempty" json:"fingerprint,omitempty"`
	RecordedAt     *time.Time                 `yaml:"recorded_at,omitempty" json:"recorded_at,omitempty"`
	RecordedBy     string                     `yaml:"recorded_by,omitempty" json:"recorded_by,omitempty"`
	Summary        string                     `yaml:"summary,omitempty" json:"summary,omitempty"`
	Contributions  []RejectionRCAContribution `yaml:"contributions,omitempty" json:"contributions,omitempty"`
	Disposition    *RejectionRCADisposition   `yaml:"disposition,omitempty" json:"disposition,omitempty"`
}

// Request projects the caller-supplied fields of a stored record back onto the
// request payload that produced them, leaving every seeded field out. It is
// what makes a stored record checkable by the same structural validator, and
// its fingerprint reproducible from the request file alone.
func (r RejectionRCARecord) Request() RejectionRCARequest {
	return RejectionRCARequest{
		SchemaVersion: r.SchemaVersion,
		Summary:       r.Summary,
		Contributions: r.Contributions,
	}
}

// NormalizeRejectionRCARequest returns the structurally canonical form of a
// request: trimmed text with collapsed internal whitespace, category sets
// sorted and deduplicated, and contributions ordered by rejection index. The
// input is not modified.
func NormalizeRejectionRCARequest(request RejectionRCARequest) RejectionRCARequest {
	normalized := RejectionRCARequest{
		SchemaVersion: request.SchemaVersion,
		Summary:       collapseRejectionRCAText(request.Summary),
	}
	if len(request.Contributions) == 0 {
		return normalized
	}
	normalized.Contributions = make([]RejectionRCAContribution, 0, len(request.Contributions))
	for _, contribution := range request.Contributions {
		normalized.Contributions = append(normalized.Contributions, RejectionRCAContribution{
			RejectionIndex: contribution.RejectionIndex,
			Categories:     normalizeRejectionRCASet(contribution.Categories),
			Evidence:       normalizeRejectionRCAList(contribution.Evidence),
		})
	}
	slices.SortStableFunc(normalized.Contributions, func(a, b RejectionRCAContribution) int {
		return cmp.Compare(a.RejectionIndex, b.RejectionIndex)
	})
	return normalized
}

// RejectionRCAFingerprint is the structural identity of a caller request: a
// sha256 over the normalized schema version, summary and contributions and
// nothing else. No seeded field, timestamp or disposition enters it, so a
// resubmitted request file reproduces the stored digest exactly.
func RejectionRCAFingerprint(request RejectionRCARequest) string {
	// Fixed scalars and strings only; no unmarshalable payloads.
	data, _ := json.Marshal(NormalizeRejectionRCARequest(request))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// collapseRejectionRCAText trims and collapses internal whitespace so prose
// volume alone never creates a new RCA identity.
func collapseRejectionRCAText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// normalizeRejectionRCAList collapses each entry and drops empty ones,
// preserving order.
func normalizeRejectionRCAList[T ~string](values []T) []T {
	var normalized []T
	for _, value := range values {
		if collapsed := collapseRejectionRCAText(string(value)); collapsed != "" {
			normalized = append(normalized, T(collapsed))
		}
	}
	return normalized
}

// normalizeRejectionRCASet additionally sorts and deduplicates, because a
// category list is a set: neither its order nor a repetition carries meaning.
func normalizeRejectionRCASet(values []RejectionCause) []RejectionCause {
	normalized := normalizeRejectionRCAList(values)
	if len(normalized) == 0 {
		return nil
	}
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

// RejectionRCAGateOpen reports whether a task is gated on a classified RCA and
// its disposition. It is the carrier every consumer uses to distinguish a
// gated blocker from an ordinary one; no consumer parses blocked_reason.
func (t *Task) RejectionRCAGateOpen() bool {
	return t != nil && t.RejectionRCA != nil && t.RejectionRCA.Disposition == nil
}

// RejectionRCAGateDue reports whether a rejection crossing the threshold must
// gate the task. The predicate is cycle-relative: an open gate never re-fires,
// and a resumed task needs a further threshold rejections beyond the count its
// record captured, because ReviewCyclesTotal never resets. A non-positive
// threshold disables the gate rather than firing on every rejection.
func (t *Task) RejectionRCAGateDue(threshold int) bool {
	if t == nil || threshold <= 0 || t.RejectionRCAGateOpen() {
		return false
	}
	baseline := 0
	if t.RejectionRCA != nil {
		baseline = t.RejectionRCA.RejectionCount
	}
	return t.DurableRejectionCount() >= baseline+threshold
}

// DurableRejectionCount counts rejections that survive an attempt reset:
// ReviewCyclesTotal, falling back when it is zero to the rejection events in
// history. The fallback reproduces the rule analysis.checkPlanningReviewChurn
// applies, so the gate and that backstop cannot disagree.
func (t *Task) DurableRejectionCount() int {
	if t == nil {
		return 0
	}
	if t.ReviewCyclesTotal > 0 {
		return t.ReviewCyclesTotal
	}
	count := 0
	for _, entry := range t.History {
		if entry.Time.IsZero() {
			continue
		}
		if entry.Event == TaskEventRejected || entry.Event == TaskEventReviewVerdictRejected {
			count++
		}
	}
	return count
}
