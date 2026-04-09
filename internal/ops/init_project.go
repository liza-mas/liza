package ops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	lzerr "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// InitProjectParams holds parameters for non-interactive project initialization.
type InitProjectParams struct {
	Description     string
	SpecRef         string
	Branch          string // default "integration" if empty
	EntryPoint      string // optional
	PostWorktreeCmd string // optional
	DefaultCLI      string // optional; default CLI for agent spawning
	AutoResume      bool
	PipelineConfig  []byte // optional raw YAML; nil = use embedded default
}

// InitProject initializes a Liza workspace at projectRoot. No terminal I/O.
// Returns error if .liza already exists, spec file is missing, or setup not run.
func InitProject(projectRoot string, params InitProjectParams) error {
	branch := params.Branch
	if branch == "" {
		branch = "integration"
	}

	// Validate branch name
	if err := validateBranch(branch); err != nil {
		return fmt.Errorf("invalid branch name %q: %w", branch, err)
	}

	// Load and validate pipeline config
	var pipelineCfg *pipeline.PipelineConfig
	var pipelineData []byte
	if params.PipelineConfig != nil {
		var err error
		pipelineCfg, err = pipeline.LoadFromBytes(params.PipelineConfig)
		if err != nil {
			return fmt.Errorf("invalid pipeline config: %w", err)
		}
		pipelineData = params.PipelineConfig
	} else {
		pipelineData = embedded.PipelineConfigContent()
		var err error
		pipelineCfg, err = pipeline.LoadFromBytes(pipelineData)
		if err != nil {
			return fmt.Errorf("invalid embedded pipeline config: %w", err)
		}
	}

	// Validate entry-point if provided
	if params.EntryPoint != "" {
		if _, ok := pipelineCfg.Pipeline.EntryPoints[params.EntryPoint]; !ok {
			names := entryPointNamesSorted(pipelineCfg)
			return fmt.Errorf("entry-point %q not found in pipeline config (available: %s)",
				params.EntryPoint, strings.Join(names, ", "))
		}
	}

	lp := paths.New(projectRoot)

	// Validate .liza doesn't already exist
	if _, err := os.Stat(lp.LizaDir()); !os.IsNotExist(err) {
		return &PreconditionError{Reason: fmt.Sprintf(".liza already exists at %s, remove or use existing", lp.LizaDir())}
	}

	// Resolve and validate spec file
	specPath := params.SpecRef
	if !filepath.IsAbs(specPath) {
		specPath = filepath.Join(projectRoot, specPath)
	}
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		return &lzerr.ValidationError{Message: fmt.Sprintf("spec file does not exist: %s", params.SpecRef)}
	}

	// Validate global config exists (liza setup prerequisite)
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return fmt.Errorf("failed to determine global config path: %w", err)
	}
	globalCoreFile := filepath.Join(globalDir, "CORE.md")
	if _, err := os.Stat(globalCoreFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("global config not found at %s\nRun 'liza setup' first to install contracts and skills", globalDir)
		}
		return fmt.Errorf("cannot access global config at %s: %w", globalCoreFile, err)
	}

	// Create directory structure
	if err := os.MkdirAll(lp.LizaDir(), 0755); err != nil {
		return fmt.Errorf("failed to create .liza directory: %w", err)
	}

	cleanup := func() {
		os.RemoveAll(lp.LizaDir())
	}

	if err := os.Mkdir(lp.ArchiveDir(), 0755); err != nil {
		cleanup()
		return fmt.Errorf("failed to create archive directory: %w", err)
	}

	// Write support doc (non-fatal)
	_ = embedded.WriteSupportDoc(lp.LizaDir())

	// Freeze pipeline config
	frozenPath := filepath.Join(lp.LizaDir(), "pipeline.yaml")
	if err := os.WriteFile(frozenPath, pipelineData, 0644); err != nil {
		cleanup()
		return fmt.Errorf("failed to freeze pipeline config: %w", err)
	}

	// Build initial state
	timestamp := time.Now().UTC()
	goalID := fmt.Sprintf("goal-%d", timestamp.Unix())

	postWorktreeCmd := stringPtrIfNonEmpty(params.PostWorktreeCmd)

	state := &models.State{
		Version:         1,
		PipelineVersion: 2,
		Goal: models.Goal{
			ID:          goalID,
			Description: params.Description,
			SpecRef:     specPath,
			EntryPoint:  params.EntryPoint,
			Created:     timestamp,
			Status:      models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{
				{
					Timestamp: timestamp,
					Event:     models.TaskEventInitialization,
					Summary:   "Initial goal. No tasks defined yet.",
				},
			},
		},
		Tasks:       []models.Task{},
		Agents:      make(map[string]models.Agent),
		Discovered:  []models.Discovery{},
		HumanNotes:  []models.HumanNote{},
		SpecChanges: []models.SpecChange{},
		Anomalies:   []models.Anomaly{},
		Sprint: models.Sprint{
			ID:      "sprint-1",
			Number:  1,
			GoalRef: goalID,
			Scope: models.SprintScope{
				Planned: []string{},
				Stretch: []string{},
			},
			Timeline: models.SprintTimeline{
				Started:      timestamp,
				Deadline:     time.Time{},
				CheckpointAt: nil,
				Ended:        nil,
			},
			Status: models.SprintStatusInProgress,
			Metrics: models.SprintMetrics{
				TasksDone:         0,
				TasksInProgress:   0,
				TasksBlocked:      0,
				IterationsTotal:   0,
				ReviewCyclesTotal: 0,
			},
			Retrospective: nil,
		},
		CircuitBreaker: models.CircuitBreaker{
			LastCheck:      time.Time{},
			Status:         "OK",
			CurrentTrigger: nil,
			History:        []models.CircuitBreakerHistory{},
		},
		Config: models.Config{
			MaxCoderIterations:       10,
			MaxReviewCycles:          5,
			HeartbeatInterval:        60,
			LeaseDuration:            1800,
			CoderPollInterval:        30,
			CoderMaxWait:             7200,
			OrchestratorPollInterval: 60,
			OrchestratorMaxWait:      7200,
			ReviewerPollInterval:     30,
			ReviewerMaxWait:          7200,
			DefaultCLI:               params.DefaultCLI,
			IntegrationBranch:        branch,
			EscalationWebhook:        nil,
			Mode:                     models.SystemModeRunning,
			AutoResume:               params.AutoResume,
			PostWorktreeCmd:          postWorktreeCmd,
		},
	}

	// Write state file
	bb := db.For(lp.StatePath())
	if err := bb.Write(state); err != nil {
		cleanup()
		return fmt.Errorf("failed to write state file: %w", err)
	}

	// Write log file
	logContent := fmt.Sprintf("- timestamp: %s\n  agent: system\n  action: initialized\n  detail: %s\n",
		timestamp.Format(time.RFC3339), params.Description)
	if err := os.WriteFile(lp.LogPath(), []byte(logContent), 0644); err != nil {
		cleanup()
		return fmt.Errorf("failed to write log file: %w", err)
	}

	// Create alerts.log
	if err := os.WriteFile(lp.AlertsLogPath(), []byte{}, 0644); err != nil {
		cleanup()
		return fmt.Errorf("failed to create alerts.log: %w", err)
	}

	// Create lock file
	if err := os.WriteFile(lp.LockPath(), []byte{}, 0644); err != nil {
		cleanup()
		return fmt.Errorf("failed to create lock file: %w", err)
	}

	// Create integration branch (non-fatal)
	_ = createIntegrationBranchAt(projectRoot, branch)

	return nil
}

// validateBranch checks that name is a valid git branch name.
func validateBranch(name string) error {
	cmd := exec.Command("git", "check-ref-format", "--branch", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("not a valid git branch name: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// createIntegrationBranchAt creates a git branch at projectRoot if it doesn't exist.
func createIntegrationBranchAt(projectRoot, name string) error {
	cmd := exec.Command("git", "-C", projectRoot, "rev-parse", "--verify", name)
	if err := cmd.Run(); err == nil {
		return nil // branch already exists
	}

	cmd = exec.Command("git", "-C", projectRoot, "branch", name, "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch failed: %w: %s", err, string(output))
	}
	return nil
}

// entryPointNamesSorted returns sorted entry-point names from pipeline config.
func entryPointNamesSorted(cfg *pipeline.PipelineConfig) []string {
	names := make([]string, 0, len(cfg.Pipeline.EntryPoints))
	for name := range cfg.Pipeline.EntryPoints {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// stringPtrIfNonEmpty returns a pointer to s if non-empty, otherwise nil.
func stringPtrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
