package models

import "time"

// ValidationRetryInterval bounds unchanged failures. Records are audit evidence,
// never reusable authority to launch or claim in another process.
const ValidationRetryInterval = time.Minute

// ValidationReadiness retains only the latest sanitized observation per agent/task.
type ValidationReadiness struct {
	Generation     string    `yaml:"generation" json:"generation"`
	TaskID         string    `yaml:"task_id" json:"task_id"`
	Commit         string    `yaml:"commit" json:"commit"`
	ReviewCommit   string    `yaml:"review_commit,omitempty" json:"review_commit,omitempty"`
	IntegrationSHA string    `yaml:"integration_sha" json:"integration_sha"`
	Digest         string    `yaml:"digest" json:"digest"`
	Fingerprint    string    `yaml:"fingerprint" json:"fingerprint"`
	CheckedAt      time.Time `yaml:"checked_at" json:"checked_at"`
	Method         string    `yaml:"method" json:"method"`
	Result         string    `yaml:"result" json:"result"`
	Code           string    `yaml:"code,omitempty" json:"code,omitempty"`
	CommandIndex   int       `yaml:"command_index,omitempty" json:"command_index,omitempty"`
	CheckIndex     int       `yaml:"check_index,omitempty" json:"check_index,omitempty"`
	Variable       string    `yaml:"variable,omitempty" json:"variable,omitempty"`
}

// CurrentValidationReadiness reports a recent observation of this exact declared
// contract and registration. Filesystem identities must additionally be checked
// by ops before making an assignment; this function is a scheduling projection.
func CurrentValidationReadiness(state *State, task *Task, agentID string, now time.Time) (ValidationReadiness, bool) {
	if state == nil || task == nil || len(task.ValidationPrerequisites) == 0 {
		return ValidationReadiness{}, false
	}
	agent, ok := state.Agents[agentID]
	if !ok {
		return ValidationReadiness{}, false
	}
	r, ok := state.ValidationReadiness[agentID][task.ID]
	if !ok || r.Generation != agent.Generation || r.Digest != ValidationPrerequisiteDigest(task.Validation, task.ValidationPrerequisites) || r.CheckedAt.After(now) || now.Sub(r.CheckedAt) >= ValidationRetryInterval {
		return ValidationReadiness{}, false
	}
	reviewCommit := ""
	if task.ReviewCommit != nil {
		reviewCommit = *task.ReviewCommit
	}
	if r.ReviewCommit != reviewCommit {
		return ValidationReadiness{}, false
	}
	return r, true
}

// ValidationTaskKnownFailed excludes only a recent failed task/registration pair.
func ValidationTaskKnownFailed(state *State, task *Task, agentID string, now time.Time) bool {
	r, ok := CurrentValidationReadiness(state, task, agentID, now)
	return ok && r.Result == "failed"
}
