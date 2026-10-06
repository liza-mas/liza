package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// StateLockHold is an explicit infrastructure-only recovery authorization for
// one blocked episode. A later publication is evidence of acquisition recovery,
// never authority to bypass other unblock/admission gates (ADR-0183).
type StateLockHold struct {
	EpisodeAt     time.Time `yaml:"episode_at" json:"episode_at"`
	BlockerDigest string    `yaml:"blocker_digest" json:"blocker_digest"`
	AfterSequence uint64    `yaml:"after_sequence" json:"after_sequence"`
	// An unchanged mechanical refusal must not create provider wake loops.
	RefusedFingerprint string `yaml:"refused_fingerprint,omitempty" json:"refused_fingerprint,omitempty"`
	RefusedReason      string `yaml:"refused_reason,omitempty" json:"refused_reason,omitempty"`
}

// StateLockBlockerDigest binds the tag to the exact reason/questions it
// classifies. Human asks, repairs, dependencies and RCA obligations are checked
// separately at recovery; changing them never grants automatic authorization.
func StateLockBlockerDigest(task *Task) string {
	reason := ""
	if task.BlockedReason != nil {
		reason = *task.BlockedReason
	}
	data, _ := json.Marshal(struct {
		Reason    string   `json:"reason"`
		Questions []string `json:"questions"`
	}{reason, task.BlockedQuestions})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// CurrentStateLockHold rejects tags carried over a reblock or a new blocker.
func CurrentStateLockHold(task *Task) bool {
	return task != nil && task.Status == TaskStatusBlocked && task.StateLockHold != nil &&
		task.StateLockHold.AfterSequence > 0 &&
		task.StateLockHold.EpisodeAt.Equal(BlockedEpisodeAt(task)) &&
		task.StateLockHold.BlockerDigest == StateLockBlockerDigest(task)
}
