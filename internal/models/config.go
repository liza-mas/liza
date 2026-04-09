package models

import (
	"fmt"
	"slices"
	"time"
)

// SystemMode represents the operational mode of the Liza system
type SystemMode string

const (
	SystemModeRunning               SystemMode = "RUNNING"
	SystemModePaused                SystemMode = "PAUSED"
	SystemModeStopped               SystemMode = "STOPPED"
	SystemModeCircuitBreakerTripped SystemMode = "CIRCUIT_BREAKER_TRIPPED"
)

// IsValid checks if the system mode is valid
func (sm SystemMode) IsValid() bool {
	return sm == SystemModeRunning || sm == SystemModePaused || sm == SystemModeStopped || sm == SystemModeCircuitBreakerTripped
}

// systemModeTransition defines allowed source modes and rejection messages for a target mode.
type systemModeTransition struct {
	AllowedFrom []SystemMode
	Rejections  map[SystemMode]string
}

// systemModeTransitions declares the valid mode transition graph, keyed by target mode.
// Callers say "transition TO X"; the table says which source modes are valid and what
// error message to return for known-invalid sources.
var systemModeTransitions = map[SystemMode]systemModeTransition{
	SystemModeRunning: {
		AllowedFrom: []SystemMode{SystemModeStopped},
		Rejections: map[SystemMode]string{
			SystemModeRunning: "system is already RUNNING",
			SystemModePaused:  "system is PAUSED - use 'liza resume' instead",
		},
	},
	SystemModeStopped: {
		AllowedFrom: []SystemMode{SystemModeRunning, SystemModePaused, SystemModeCircuitBreakerTripped},
		Rejections: map[SystemMode]string{
			SystemModeStopped: "system is already STOPPED",
		},
	},
	SystemModePaused: {
		AllowedFrom: []SystemMode{SystemModeRunning, SystemModeCircuitBreakerTripped},
		Rejections: map[SystemMode]string{
			SystemModePaused:  "system is already PAUSED",
			SystemModeStopped: "cannot pause: system is STOPPED (use resume only from PAUSED state)",
		},
	},
}

// ValidateTransition checks whether transitioning from sm to the target mode is valid.
// Returns nil if allowed, or a descriptive error for known rejections / unknown sources.
func (sm SystemMode) ValidateTransition(to SystemMode) error {
	tr, ok := systemModeTransitions[to]
	if !ok {
		return fmt.Errorf("unknown target mode: %s", to)
	}

	if msg, rejected := tr.Rejections[sm]; rejected {
		return fmt.Errorf("%s", msg)
	}

	if slices.Contains(tr.AllowedFrom, sm) {
		return nil
	}

	return fmt.Errorf("can only transition to %s from %v (current: %s)", to, tr.AllowedFrom, sm)
}

// Default configuration values (seconds) used as fallbacks when config fields are unset.
const (
	DefaultMaxCoderIterations       = 10
	DefaultMaxReviewCycles          = 5
	DefaultLeaseDurationSeconds     = 1800 // 30 minutes
	DefaultCoderPollInterval        = 30
	DefaultCoderMaxWait             = 7200 // 2 hours
	DefaultOrchestratorPollInterval = 60
	DefaultOrchestratorMaxWait      = 7200 // 2 hours
	DefaultReviewerPollInterval     = 30
	DefaultReviewerMaxWait          = 7200 // 2 hours
	DefaultExit42MaxBackoffSec      = 60
	DefaultExit42RestartLimit       = 5
	DefaultCrashRestartThreshold    = 5
	DefaultSpinningRestartThreshold = 10
)

// Bounds for heartbeat interval validation.
const (
	MinHeartbeatIntervalSeconds = 1
	MaxHeartbeatIntervalSeconds = 300 // 5 minutes
	DefaultHeartbeatIntervalSec = 60
)

// NormalizeHeartbeatInterval validates and normalizes a heartbeat interval value.
// Returns the normalized duration or the default if the value is invalid.
// Invalid values: ≤ 0, > MaxHeartbeatIntervalSeconds (300s / 5min)
func NormalizeHeartbeatInterval(interval int) time.Duration {
	if interval <= 0 || interval > MaxHeartbeatIntervalSeconds {
		return DefaultHeartbeatIntervalSec * time.Second
	}
	return time.Duration(interval) * time.Second
}

// Config holds system configuration parameters
type Config struct {
	MaxCoderIterations       int `yaml:"max_coder_iterations"`
	MaxReviewCycles          int `yaml:"max_review_cycles"`
	HeartbeatInterval        int `yaml:"heartbeat_interval"`
	LeaseDuration            int `yaml:"lease_duration"`
	CoderPollInterval        int `yaml:"coder_poll_interval"`
	CoderMaxWait             int `yaml:"coder_max_wait"`
	OrchestratorPollInterval int `yaml:"orchestrator_poll_interval"`
	// OrchestratorMaxWait is the maximum time an orchestrator agent will wait for work
	// before exiting. When 0, defaults to DefaultOrchestratorMaxWait (2 hours).
	// The orchestrator will exit earlier if STOPPED mode is detected or context is cancelled.
	OrchestratorMaxWait      int            `yaml:"orchestrator_max_wait"`
	ReviewerPollInterval     int            `yaml:"reviewer_poll_interval"`
	ReviewerMaxWait          int            `yaml:"reviewer_max_wait"`
	Exit42RestartThreshold   int            `yaml:"exit42_restart_threshold,omitempty"`
	Exit42MaxBackoffSeconds  int            `yaml:"exit42_max_backoff_seconds,omitempty"`
	CrashRestartThreshold    int            `yaml:"crash_restart_threshold,omitempty"`
	SpinningRestartThreshold int            `yaml:"spinning_restart_threshold,omitempty"`
	DefaultCLI               string         `yaml:"default_cli,omitempty"`
	IntegrationBranch        string         `yaml:"integration_branch"`
	EscalationWebhook        *string        `yaml:"escalation_webhook,omitempty"`
	Mode                     SystemMode     `yaml:"mode,omitempty"`
	ModeChangedAt            *time.Time     `yaml:"mode_changed_at,omitempty"`
	ModeChangedBy            *string        `yaml:"mode_changed_by,omitempty"`
	DiagnosticLogging        bool           `yaml:"diagnostic_logging,omitempty"`
	AutoResume               bool           `yaml:"auto_resume,omitempty"`
	PostWorktreeCmd          *string        `yaml:"post_worktree_cmd,omitempty"`
	Extra                    map[string]any `yaml:",inline"`
}
