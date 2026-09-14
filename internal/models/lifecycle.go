package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	LifecycleCompleted            = "COMPLETED"
	LifecycleAlreadyCompleted     = "ALREADY_COMPLETED"
	LifecycleAlreadyTransitioned  = "ALREADY_TRANSITIONED"
	LifecycleStaleCaller          = "STALE_CALLER"
	LifecycleStateChanged         = "STATE_CHANGED"
	LifecycleRetryable            = "RETRYABLE"
	LifecycleInvalidInput         = "INVALID_INPUT"
	LifecycleForbidden            = "FORBIDDEN"
	LifecycleReceiptsPerOperation = 4
	LifecycleReceiptsPerTask      = 16
	LifecycleProjectionMaxBytes   = 1024
	LifecycleIdentifierMaxBytes   = 128
)

// LifecycleOutcome describes one operation's observed result, not permission
// to repeat its effects. TransitionID always describes the current task.
type LifecycleOutcome struct {
	Operation             string     `json:"operation" yaml:"operation"`
	TaskID                string     `json:"task_id" yaml:"task_id"`
	Outcome               string     `json:"outcome" yaml:"outcome"`
	SafeAction            string     `json:"safe_action" yaml:"safe_action"`
	TaskStatus            TaskStatus `json:"task_status" yaml:"task_status"`
	CurrentAssignee       string     `json:"current_assignee,omitempty" yaml:"current_assignee,omitempty"`
	CurrentReviewer       string     `json:"current_reviewer,omitempty" yaml:"current_reviewer,omitempty"`
	TransitionID          string     `json:"transition_id,omitempty" yaml:"transition_id,omitempty"`
	CompletedTransitionID string     `json:"completed_transition_id,omitempty" yaml:"completed_transition_id,omitempty"`
	RequestID             string     `json:"request_id,omitempty" yaml:"request_id,omitempty"`
	Effects               string     `json:"effects" yaml:"effects"`
}

// TaskLifecycle bounds replay metadata independently of invocation count.
// Preparation is separate from receipts and is never removed by pruning.
type TaskLifecycle struct {
	Revision           uint64                `yaml:"revision" json:"revision"`
	CompletionSequence uint64                `yaml:"completion_sequence" json:"completion_sequence"`
	Receipts           []LifecycleReceipt    `yaml:"receipts,omitempty" json:"receipts,omitempty"`
	Preparation        *LifecyclePreparation `yaml:"preparation,omitempty" json:"preparation,omitempty"`
}

// LifecycleIdentity scopes an invocation to its original observed boundary.
// GenerationDigest is persistence-only; presentation must also redact it from
// YAML/raw-state inspection, not merely rely on its JSON exclusion.
type LifecycleIdentity struct {
	Operation          string `yaml:"operation" json:"operation"`
	Actor              string `yaml:"actor" json:"actor"`
	GenerationDigest   string `yaml:"generation_digest,omitempty" json:"-"`
	RequestID          string `yaml:"request_id,omitempty" json:"request_id,omitempty"`
	ExpectedTransition string `yaml:"expected_transition" json:"expected_transition"`
	PayloadDigest      string `yaml:"payload_digest" json:"payload_digest"`
}

// LifecycleProjection is intentionally fixed and small. Reports, notes, and
// arbitrary payloads remain in their existing domain artifacts.
type LifecycleProjection struct {
	ReleasedDoer     bool       `yaml:"released_doer,omitempty" json:"released_doer,omitempty"`
	ReleasedReviewer bool       `yaml:"released_reviewer,omitempty" json:"released_reviewer,omitempty"`
	InputCommit      string     `yaml:"input_commit,omitempty" json:"input_commit,omitempty"`
	ReviewCommit     string     `yaml:"review_commit,omitempty" json:"review_commit,omitempty"`
	BaseCommit       string     `yaml:"base_commit,omitempty" json:"base_commit,omitempty"`
	MergeCommit      string     `yaml:"merge_commit,omitempty" json:"merge_commit,omitempty"`
	LeaseExpires     string     `yaml:"lease_expires,omitempty" json:"lease_expires,omitempty"`
	SourceStatus     TaskStatus `yaml:"source_status,omitempty" json:"source_status,omitempty"`
	Attempt          int        `yaml:"attempt,omitempty" json:"attempt,omitempty"`
	Iteration        int        `yaml:"iteration,omitempty" json:"iteration,omitempty"`
	Verdict          string     `yaml:"verdict,omitempty" json:"verdict,omitempty"`
}

type LifecycleReceipt struct {
	LifecycleIdentity `yaml:",inline"`
	Sequence          uint64              `yaml:"sequence" json:"sequence"`
	TransitionID      string              `yaml:"transition_id" json:"transition_id"`
	Projection        LifecycleProjection `yaml:"projection,omitempty" json:"projection,omitempty"`
}

// Boundary is the live token after any obsolete preparation was retired. The
// original ExpectedTransition remains immutable as part of request identity.
type LifecyclePreparation struct {
	LifecycleIdentity `yaml:",inline"`
	Boundary          string `yaml:"boundary" json:"boundary"`
}

func IsLifecycleOutcome(value string) bool {
	switch value {
	case LifecycleCompleted, LifecycleAlreadyCompleted, LifecycleAlreadyTransitioned,
		LifecycleStaleCaller, LifecycleStateChanged, LifecycleRetryable,
		LifecycleInvalidInput, LifecycleForbidden:
		return true
	}
	return false
}

func IsLifecycleOperation(value string) bool {
	switch value {
	case "submit-for-review", "submit-verdict", "mark-blocked", "assess-blocked",
		"assess-hypothesis-exhausted", "claim-task", "claim-reviewer-task", "release-claim",
		"wt-merge", "recover-task", "retarget-dependency", "narrow-inherited-dependencies", "apply-dependency-repair",
		"repair-superseded-dependencies", "cancel-task", "supersede-task", "unblock-task",
		"set-task-output", "handoff", "recover-agent", "transition-attempt":
		return true
	}
	return false
}

// TaskTransitionID changes for real ownership/history boundaries, including
// legacy writes and declared validation-contract changes, but not for lease
// renewal, heartbeat, registration, or passive readiness observations alone.
func TaskTransitionID(task *Task) string {
	if task == nil {
		return ""
	}
	type eventIdentity struct {
		Time   time.Time
		Event  string
		Agent  *string
		Commit *string
	}
	var last eventIdentity
	if len(task.History) > 0 {
		e := task.History[len(task.History)-1]
		last = eventIdentity{e.Time.UTC(), e.Event, e.Agent, e.Commit}
	}
	var revision uint64
	if task.Lifecycle != nil {
		revision = task.Lifecycle.Revision
	}
	var validationContract string
	if len(task.ValidationPrerequisites) > 0 {
		validationContract = ValidationPrerequisiteDigest(task.Validation, task.ValidationPrerequisites)
	}
	boundary := struct {
		ID                                             string
		Created                                        time.Time
		Revision                                       uint64
		Status                                         TaskStatus
		Attempt, Iteration, ReviewCycles, HistoryCount int
		Assignee, Reviewer, ReviewCommit               *string
		Last                                           eventIdentity
		ValidationContract                             string `json:",omitempty"`
	}{task.ID, task.Created.UTC(), revision, task.Status, task.EffectiveAttempt(),
		task.Iteration, task.ReviewCyclesCurrent, len(task.History), task.AssignedTo,
		task.ReviewingBy, task.ReviewCommit, last, validationContract}
	data, _ := json.Marshal(boundary) // Fixed scalars only; no unmarshalable payloads.
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// AdvanceLifecycle belongs in an already-authorized state transaction that
// changes the task boundary. It invalidates abandoned work without claiming
// completion or rollback, and preserves prior completed receipts.
func AdvanceLifecycle(task *Task) {
	if task.Lifecycle == nil {
		task.Lifecycle = &TaskLifecycle{}
	}
	task.Lifecycle.Revision++
	task.Lifecycle.Preparation = nil
}
