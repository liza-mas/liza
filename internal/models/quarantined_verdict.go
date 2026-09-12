package models

import (
	"encoding/hex"
	"time"
)

// QuarantinedVerdict retains evidence from a fenced registration without
// granting it task-transition or approval authority. Matched is fixed at capture:
// an unknown boundary must never acquire authority through a later coincidence.
type QuarantinedVerdict struct {
	ID                     string                  `yaml:"id" json:"id"`
	TaskID                 string                  `yaml:"task_id" json:"task_id"`
	ReviewCommit           string                  `yaml:"review_commit" json:"review_commit"`
	ReviewerID             string                  `yaml:"reviewer_id" json:"reviewer_id"`
	Verdict                string                  `yaml:"verdict" json:"verdict"`
	Reason                 string                  `yaml:"reason" json:"reason"`
	Timestamp              time.Time               `yaml:"timestamp" json:"timestamp"`
	GenerationFingerprints []string                `yaml:"generation_fingerprints" json:"generation_fingerprints"`
	Matched                bool                    `yaml:"matched" json:"matched"`
	Reconciliations        []VerdictReconciliation `yaml:"reconciliations,omitempty" json:"reconciliations,omitempty"`
}

// VerdictReconciliation is an append-only, authenticated judgment on evidence.
// It never counts toward review quorum or mutates the task lifecycle.
type VerdictReconciliation struct {
	Actor       string    `yaml:"actor" json:"actor"`
	Timestamp   time.Time `yaml:"timestamp" json:"timestamp"`
	Disposition string    `yaml:"disposition" json:"disposition"`
	Reason      string    `yaml:"reason" json:"reason"`
}

// IsFullReviewCommit accepts immutable Git object IDs, never refs or abbreviations.
func IsFullReviewCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	_, err := hex.DecodeString(commit)
	return err == nil
}

// IsVerdictDisposition reports the supported reconciliation judgments.
func IsVerdictDisposition(disposition string) bool {
	switch disposition {
	case "accepted", "refuted", "superseded", "escalated":
		return true
	default:
		return false
	}
}
