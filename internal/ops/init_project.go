package ops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/envgate"
	lzerr "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/initcheck"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/semble"
)

// InitProjectParams holds parameters for non-interactive project initialization.
type InitProjectParams struct {
	Description          string
	SpecRef              string
	Branch               string // default "integration" if empty
	EntryPoint           string // optional
	PostWorktreeCmd      string // optional
	CopyWorktreeEnvFiles bool   // optional explicit authorization to copy ignored root env files into task worktrees
	DefaultCLI           string // optional; default CLI for agent spawning
	DefaultDoerCLI       string // optional; default CLI for doer and orchestrator agent spawning
	DefaultReviewerCLI   string // optional; default CLI for reviewer agent spawning
	AutoResume           bool
	NoFollowUp           bool
	PipelineConfig       []byte       // optional raw YAML; nil = use embedded default
	SembleDiagnosticSink func(string) // optional transient sink for bounded Semble diagnostics
}

var (
	initProjectSembleLookPath semble.ExecutableLookup
	initProjectSembleRunner   semble.CommandRunner
)

// InitProject initializes a workspace at projectRoot. No terminal I/O.
// Returns error if the project runtime directory already exists, spec file is
// missing, or setup has not run.
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

	// Validate project runtime directory doesn't already exist.
	if _, err := os.Stat(lp.LizaDir()); !os.IsNotExist(err) {
		return &PreconditionError{Reason: fmt.Sprintf("%s already exists at %s, remove or use existing", paths.ProjectDirName(), lp.LizaDir())}
	}

	// Resolve and validate spec file
	specPath := params.SpecRef
	if !filepath.IsAbs(specPath) {
		specPath = filepath.Join(projectRoot, specPath)
	}
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		return &lzerr.ValidationError{Message: fmt.Sprintf("spec file does not exist: %s", params.SpecRef)}
	}
	specRepoRel, err := initcheck.EnsureSpecCommittedClean(projectRoot, specPath)
	if err != nil {
		return &lzerr.ValidationError{Message: err.Error()}
	}
	if _, err := initcheck.EnsurePreCommitConfigCommittedClean(projectRoot, branch); err != nil {
		return &lzerr.ValidationError{Message: err.Error()}
	}

	// Validate global config exists (setup prerequisite).
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return fmt.Errorf("failed to determine global config path: %w", err)
	}
	globalCoreFile := filepath.Join(globalDir, "CORE.md")
	if _, err := os.Stat(globalCoreFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("global config not found at %s\nRun '%s setup' first to install contracts, skills, and support docs", globalDir, brand.BinaryName)
		}
		return fmt.Errorf("cannot access global config at %s: %w", globalCoreFile, err)
	}

	runInitProjectSemblePrewarm(projectRoot, params)

	// Create directory structure
	if err := os.MkdirAll(lp.LizaDir(), 0755); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", paths.ProjectDirName(), err)
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
	copyWorktreeEnvFiles := params.CopyWorktreeEnvFiles || envgate.TruthyEnv(models.EnvEnableCopyWorktreeEnvFiles)

	state := &models.State{
		Version:         1,
		PipelineVersion: 3,
		Goal: models.Goal{
			ID:          goalID,
			Description: params.Description,
			SpecRef:     specRepoRel,
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
			DoerMaxWait:              18000,
			OrchestratorPollInterval: 60,
			OrchestratorMaxWait:      18000,
			ReviewerPollInterval:     30,
			ReviewerMaxWait:          18000,
			AgentProgressTimeout:     models.DefaultAgentProgressTimeoutSec,
			DefaultCLI:               params.DefaultCLI,
			DefaultDoerCLI:           params.DefaultDoerCLI,
			DefaultReviewerCLI:       params.DefaultReviewerCLI,
			IntegrationBranch:        branch,
			EscalationWebhook:        nil,
			Mode:                     models.SystemModeRunning,
			AutoResume:               params.AutoResume,
			NoFollowUp:               params.NoFollowUp,
			PostWorktreeCmd:          postWorktreeCmd,
			CopyWorktreeEnvFiles:     copyWorktreeEnvFiles,
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

	// Create integration branch. Init must not succeed with a broken scaffold.
	if err := createIntegrationBranchAt(projectRoot, branch); err != nil {
		cleanup()
		return fmt.Errorf("failed to create integration branch %q: %w", branch, err)
	}

	return nil
}

func runInitProjectSemblePrewarm(projectRoot string, params InitProjectParams) {
	opts := semble.ValidationOptions{
		TargetRoot: projectRoot,
		LookPath:   initProjectSembleLookPath,
		Runner:     initProjectSembleRunner,
	}
	prewarm := semble.ExecutePrewarm(opts)
	if !prewarm.Enabled {
		return
	}
	if emitInitProjectSembleDiagnostic(params, prewarm.Diagnostic) {
		return
	}
	offline := semble.CheckOfflineReadiness(opts)
	emitInitProjectSembleDiagnostic(params, offline.Diagnostic)
}

func emitInitProjectSembleDiagnostic(params InitProjectParams, diagnostic semble.Diagnostic) bool {
	if diagnostic.Message == "" {
		return false
	}
	if params.SembleDiagnosticSink != nil {
		params.SembleDiagnosticSink(diagnostic.Message)
	}
	return true
}

// validateBranch checks that name is a valid git branch name.
func validateBranch(name string) error {
	cmd := exec.Command("git", "check-ref-format", "--branch", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("not a valid git branch name: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// createIntegrationBranchAt creates a git branch at projectRoot if it doesn't
// exist. Repositories with no commits cannot materialize the branch ref yet, so
// callers must surface a clear error instead of silently continuing.
func createIntegrationBranchAt(projectRoot, name string) error {
	cmd := exec.Command("git", "-C", projectRoot, "rev-parse", "--verify", name)
	if err := cmd.Run(); err == nil {
		return nil // branch already exists
	}

	cmd = exec.Command("git", "-C", projectRoot, "rev-parse", "--verify", "HEAD")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("repo has no commits (HEAD is unborn)")
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
