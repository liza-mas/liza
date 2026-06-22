package models

import (
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/brand"
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
			SystemModePaused:  fmt.Sprintf("system is PAUSED - use %q instead", brand.Command("resume")),
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
	DefaultDoerMaxWait              = 18000 // 5 hours
	DefaultOrchestratorPollInterval = 60
	DefaultOrchestratorMaxWait      = 18000 // 5 hours
	DefaultReviewerPollInterval     = 30
	DefaultReviewerMaxWait          = 18000 // 5 hours
	DefaultExit42MaxBackoffSec      = 60
	DefaultExit42RestartLimit       = 5
	DefaultCrashRestartThreshold    = 5
	DefaultSpinningRestartThreshold = 10
	DefaultAgentProgressTimeoutSec  = 1800 // 30 minutes
)

const EnvEnableCopyWorktreeEnvFiles = "LIZA_ENABLE_COPY_ENV_FILES"

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
	MaxCoderIterations int `yaml:"max_coder_iterations"`
	MaxReviewCycles    int `yaml:"max_review_cycles"`
	HeartbeatInterval  int `yaml:"heartbeat_interval"`
	LeaseDuration      int `yaml:"lease_duration"`
	CoderPollInterval  int `yaml:"coder_poll_interval"`
	DoerMaxWait        int `yaml:"doer_max_wait"`
	// DeprecatedCoderMaxWait preserves read compatibility for state files that
	// still use coder_max_wait. New state files should write doer_max_wait.
	DeprecatedCoderMaxWait   int `yaml:"coder_max_wait,omitempty" inspect:"-"`
	OrchestratorPollInterval int `yaml:"orchestrator_poll_interval"`
	// OrchestratorMaxWait is the maximum time an orchestrator agent will wait for work
	// before exiting. When 0, defaults to DefaultOrchestratorMaxWait (5 hours).
	// The orchestrator will exit earlier if STOPPED mode is detected or context is cancelled.
	OrchestratorMaxWait      int        `yaml:"orchestrator_max_wait"`
	ReviewerPollInterval     int        `yaml:"reviewer_poll_interval"`
	ReviewerMaxWait          int        `yaml:"reviewer_max_wait"`
	Exit42RestartThreshold   int        `yaml:"exit42_restart_threshold,omitempty"`
	Exit42MaxBackoffSeconds  int        `yaml:"exit42_max_backoff_seconds,omitempty"`
	CrashRestartThreshold    int        `yaml:"crash_restart_threshold,omitempty"`
	SpinningRestartThreshold int        `yaml:"spinning_restart_threshold,omitempty"`
	AgentProgressTimeout     int        `yaml:"agent_progress_timeout,omitempty"`
	DefaultCLI               string     `yaml:"default_cli,omitempty"`
	DefaultDoerCLI           string     `yaml:"default_doer_cli,omitempty"`
	DefaultReviewerCLI       string     `yaml:"default_reviewer_cli,omitempty"`
	ScipSearch               []string   `yaml:"scip_search,omitempty"`
	CodexPackageVersion      string     `yaml:"codex_package_version,omitempty"`
	IntegrationBranch        string     `yaml:"integration_branch"`
	EscalationWebhook        *string    `yaml:"escalation_webhook,omitempty"`
	Mode                     SystemMode `yaml:"mode,omitempty"`
	ModeChangedAt            *time.Time `yaml:"mode_changed_at,omitempty"`
	ModeChangedBy            *string    `yaml:"mode_changed_by,omitempty"`
	DiagnosticLogging        bool       `yaml:"diagnostic_logging,omitempty"`
	AutoResume               bool       `yaml:"auto_resume,omitempty"`
	NoFollowUp               bool       `yaml:"no_follow_up,omitempty"`
	PostWorktreeCmd          *string    `yaml:"post_worktree_cmd,omitempty"`
	CopyWorktreeEnvFiles     bool       `yaml:"copy_worktree_env_files,omitempty"`
	// AutoCheckpointSummary controls whether Liza auto-invokes the
	// checkpoint-summary skill against a task that just reached MERGED and
	// writes the result to .liza/checkpoint-summary.md. Default (nil) is ON.
	// Set explicitly to false to opt out.
	AutoCheckpointSummary *bool          `yaml:"auto_checkpoint_summary,omitempty"`
	Extra                 map[string]any `yaml:",inline"`
}
