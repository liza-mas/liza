package models

import (
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/referencecontract"
	"gopkg.in/yaml.v3"
)

// AcceptanceSource records the immutable, independently reviewed allocation
// adopted by a task. It survives retries so strict admission cannot downgrade.
type AcceptanceSource struct {
	Ref    string `yaml:"ref" json:"ref"`
	Commit string `yaml:"commit" json:"commit"`
	// Blob is the object id of the reviewed allocation *section* named by Ref's
	// heading — what `git hash-object` returns for that extracted span — not of
	// the carrier file. Identity is the allocation, so an edit elsewhere in the
	// carrier leaves it unchanged and a change to the allocation does not.
	// Records written before that distinction existed hold the carrier file's
	// own object id; both are object ids, so the shape is stable and such a
	// record simply re-derives on its next allocation.
	Blob               string `yaml:"blob" json:"blob"`
	ParentTask         string `yaml:"parent_task" json:"parent_task"`
	ParentReviewCommit string `yaml:"parent_review_commit" json:"parent_review_commit"`
}

// AcceptanceCommandResult records one trusted execution. Command and Output
// are masked presentation; CommandSHA256 binds the exact original command.
type AcceptanceCommandResult struct {
	Command       string    `yaml:"command" json:"command"`
	CommandSHA256 string    `yaml:"command_sha256" json:"command_sha256"`
	ExitCode      int       `yaml:"exit_code" json:"exit_code"`
	StartedAt     time.Time `yaml:"started_at" json:"started_at"`
	FinishedAt    time.Time `yaml:"finished_at" json:"finished_at"`
	Output        string    `yaml:"output" json:"output"`
}

// MarshalYAML quotes execution text because go-yaml's block-scalar indentation
// can reject mixed-indentation output or silently strip its leading whitespace.
// Quoting preserves the exact masked command and output, including blank lines.
func (r AcceptanceCommandResult) MarshalYAML() (any, error) {
	return map[string]any{
		"command":        &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: r.Command},
		"command_sha256": r.CommandSHA256,
		"exit_code":      r.ExitCode,
		"started_at":     r.StartedAt,
		"finished_at":    r.FinishedAt,
		"output":         &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: r.Output},
	}, nil
}

// ArchivedFieldAcceptanceReceipt is the only task field that may be archived.
const ArchivedFieldAcceptanceReceipt = "acceptance_receipt"

// ArchivedFieldRef records a terminal-task field moved out of live state into
// the content-addressed archive object named by SHA256. The object path is
// derived from the digest, never stored, so state cannot direct archive I/O.
type ArchivedFieldRef struct {
	Field      string    `yaml:"field" json:"field"`
	SHA256     string    `yaml:"sha256" json:"sha256"`
	ArchivedAt time.Time `yaml:"archived_at" json:"archived_at"`
}

// AcceptanceReceipt binds validated mappings and successful executions to one
// immutable review commit. Admission stores it atomically with review state.
type AcceptanceReceipt struct {
	Version      int                                   `yaml:"version" json:"version"`
	ReviewCommit string                                `yaml:"review_commit" json:"review_commit"`
	Source       AcceptanceSource                      `yaml:"source" json:"source"`
	ManifestPath string                                `yaml:"manifest_path" json:"manifest_path"`
	ManifestBlob string                                `yaml:"manifest_blob" json:"manifest_blob"`
	Mappings     []referencecontract.AcceptanceMapping `yaml:"mappings" json:"mappings"`
	// Commands holds one result per executed (non-deferred) canonical command,
	// in canonical order.
	Commands []AcceptanceCommandResult `yaml:"commands" json:"commands"`
	// Deferred lists the canonical commands admission skipped; the
	// deferred-validation batch satisfies them on integration (ADR-0204).
	Deferred []AcceptanceDeferredCommand `yaml:"deferred,omitempty" json:"deferred,omitempty"`
	// TimeoutRetry is set when Commands come from a full re-run after the
	// batch hit its budget (ADR-0216).
	TimeoutRetry *AcceptanceTimeoutRetry `yaml:"timeout_retry,omitempty" json:"timeout_retry,omitempty"`
}

// CommandIndex returns the canonical validation index of Commands[i]:
// deferred commands leave gaps in the executed order.
func (r *AcceptanceReceipt) CommandIndex(i int) int {
	index := 0
	for next := 0; ; index++ {
		if slices.ContainsFunc(r.Deferred, func(d AcceptanceDeferredCommand) bool { return d.Index == index }) {
			continue
		}
		if next == i {
			return index
		}
		next++
	}
}

// AcceptanceDeferralAuthority names who deferred a canonical command.
type AcceptanceDeferralAuthority string

const (
	// AcceptanceDeferralContract is a deferral the reviewed Acceptance
	// Contract declares.
	AcceptanceDeferralContract AcceptanceDeferralAuthority = "contract"
	// AcceptanceDeferralOperator is a deferral an operator ruling grants for
	// an exact command.
	AcceptanceDeferralOperator AcceptanceDeferralAuthority = "operator"
)

// IsValid reports whether the authority is part of the persisted schema.
func (a AcceptanceDeferralAuthority) IsValid() bool {
	return a == AcceptanceDeferralContract || a == AcceptanceDeferralOperator
}

// AcceptanceDeferredCommand records one canonical command admission skipped.
// Command is masked presentation; Index and CommandSHA256 bind identity.
type AcceptanceDeferredCommand struct {
	Index          int                         `yaml:"index" json:"index"`
	Command        string                      `yaml:"command" json:"command"`
	CommandSHA256  string                      `yaml:"command_sha256" json:"command_sha256"`
	TimeoutSeconds int                         `yaml:"timeout_seconds" json:"timeout_seconds"`
	Authority      AcceptanceDeferralAuthority `yaml:"authority" json:"authority"`
}

// MarshalYAML quotes the masked command for the reason given on
// AcceptanceCommandResult.MarshalYAML.
func (d AcceptanceDeferredCommand) MarshalYAML() (any, error) {
	return map[string]any{
		"index":           d.Index,
		"command":         &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: d.Command},
		"command_sha256":  d.CommandSHA256,
		"timeout_seconds": d.TimeoutSeconds,
		"authority":       d.Authority,
	}, nil
}
