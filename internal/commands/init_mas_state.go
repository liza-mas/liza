package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/termutil"
)

// Values of the initial state MAS init writes.
const (
	initStateVersion         = 1
	initPipelineVersion      = 3 // always v3: the pipeline is mandatory
	initSprintID             = "sprint-1"
	initSprintNumber         = 1
	initCircuitBreakerStatus = circuitBreakerStatusOK
	initAlignmentSummary     = "Initial goal. No tasks defined yet."
	initGoalIDFormat         = "goal-%d"
)

// resolvePostWorktreeCmd takes --post-worktree-cmd, or auto-suggests one when it
// is not set and stdin is a terminal.
func (m *masInit) resolvePostWorktreeCmd() {
	params := m.params
	postWorktreeCmd := params.PostWorktreeCmd
	if postWorktreeCmd == "" && (params.ForceInteractive || params.AutoConfirm || isInteractive(m.rawStdin)) {
		root := m.lizaPaths.ProjectRoot()
		if suggested := detectPostWorktreeCmd(root); suggested != "" {
			fmt.Fprintf(os.Stderr, "Detected %s — set post_worktree_cmd to %q?\n", detectPkgManagerContext(root), suggested)
			fmt.Fprintf(os.Stderr, "This runs after every worktree creation so agents have dependencies. (y/n): ")
			if params.AutoConfirm {
				fmt.Fprintln(os.Stderr, "yes")
				postWorktreeCmd = suggested
			} else {
				response, err := termutil.ReadSingleKey(m.stdin)
				if err == nil && response == confirmAnswerY {
					postWorktreeCmd = suggested
				}
			}
		} else if subdirs := detectNodeSubdirs(root); len(subdirs) > 1 {
			fmt.Fprintf(os.Stderr, "Detected Node projects in: %s. Configure --post-worktree-cmd manually.\n", strings.Join(subdirs, ", "))
		}
	}
	m.postWorktreeCmd = postWorktreeCmd
}

// warnMissingNodeDependencies warns if a Node.js project lacks installed
// dependencies. post_worktree_cmd runs npm/yarn/pnpm install in worktrees, but it
// can fail silently if the main repo's deps aren't installed (no cache, missing
// native modules, etc.). Catching it here prevents agents from discovering the
// problem 17 turns into a review session.
func (m *masInit) warnMissingNodeDependencies() {
	root := m.lizaPaths.ProjectRoot()
	if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(root, "node_modules")); os.IsNotExist(err) {
			installCmd := detectInstallCmdInDir(root)
			fmt.Fprintf(os.Stderr, "⚠️  package.json found but node_modules/ is missing. Run %q before starting agents.\n", installCmd)
		}
	}
	for _, dir := range detectNodeSubdirs(root) {
		dirPath := filepath.Join(root, dir)
		if _, err := os.Stat(filepath.Join(dirPath, "node_modules")); os.IsNotExist(err) {
			installCmd := detectInstallCmdInDir(dirPath)
			fmt.Fprintf(os.Stderr, "⚠️  %s/package.json found but %s/node_modules/ is missing. Run %q in %s/ before starting agents.\n", dir, dir, installCmd, dir)
		}
	}
}

// resolveValidationTools resolves the validation-execution assertion; a failure
// removes the runtime directory.
func (m *masInit) resolveValidationTools() error {
	validationTools, err := resolveValidationExecutionTools(m.params, m.lizaPaths.ProjectRoot(), m.pipelineCfg, m.stdin, m.rawStdin)
	if err != nil {
		m.cleanupInit()
		return err
	}
	m.validationTools = validationTools
	return nil
}

// activateIndexing installs the project-root indexing lifecycle hooks. No agent
// refreshes project-root indexes: the lifecycle hooks do. They are installed after
// the last prompt, so a cancelled init leaves Git hooks alone, and before writing
// state, so an unmanaged hook collision fails init rather than leaving a project
// whose indexes silently never refresh.
func (m *masInit) activateIndexing() error {
	return activateProjectRootIndexing(pairingindex.MASActivationOptions(m.lizaPaths.ProjectRoot(), m.scipSearchConfig.LanguageNames()))
}

// composeState builds the initial state of the goal at timestamp.
func (m *masInit) composeState(timestamp time.Time) *models.State {
	goalID := fmt.Sprintf(initGoalIDFormat, timestamp.Unix())
	return &models.State{
		Version:         initStateVersion,
		PipelineVersion: initPipelineVersion,
		Goal: models.Goal{
			ID:          goalID,
			Description: m.params.Description,
			SpecRef:     m.specRepoRel,
			EntryPoint:  m.params.EntryPoint,
			Created:     timestamp,
			Status:      models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{
				{
					Timestamp: timestamp,
					Event:     string(models.TaskEventInitialization),
					Summary:   initAlignmentSummary,
				},
			},
		},
		Tasks:       []models.Task{},
		Agents:      make(map[string]models.Agent),
		Discovered:  []models.Discovery{},
		HumanNotes:  []models.HumanNote{},
		SpecChanges: []models.SpecChange{},
		Anomalies:   []models.Anomaly{},
		Sprint:      initialSprint(goalID, timestamp),
		CircuitBreaker: models.CircuitBreaker{
			LastCheck:      time.Time{}, // zero value for null
			Status:         initCircuitBreakerStatus,
			CurrentTrigger: nil,
			History:        []models.CircuitBreakerHistory{},
		},
		Config: m.composeConfig(),
	}
}

func initialSprint(goalID string, timestamp time.Time) models.Sprint {
	return models.Sprint{
		ID:      initSprintID,
		Number:  initSprintNumber,
		GoalRef: goalID,
		Scope: models.SprintScope{
			Planned: []string{},
			Stretch: []string{},
		},
		Timeline: models.SprintTimeline{
			Started:      timestamp,
			Deadline:     time.Time{}, // zero value for null
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
	}
}

// composeConfig builds the initial configuration from the flags, the resolved
// SCIP languages, the post-worktree command and the validation tools.
func (m *masInit) composeConfig() models.Config {
	params := m.params
	copyWorktreeEnvFiles := params.CopyWorktreeEnvFiles || envgate.TruthyEnv(models.EnvEnableCopyWorktreeEnvFiles)
	return models.Config{
		TerminalTaskArchival:            true,
		MaxCoderIterations:              models.DefaultMaxCoderIterations,
		MaxReviewCycles:                 models.DefaultMaxReviewCycles,
		MaxGlobalIntegrationGenerations: models.NormalizeGlobalIntegrationGenerationLimit(params.MaxGlobalIntegrationGenerations),
		MaxInstances:                    models.EffectiveMaxInstances(0, params.MaxInstances),
		HeartbeatInterval:               models.DefaultHeartbeatIntervalSec,
		LeaseDuration:                   models.DefaultLeaseDurationSeconds,
		CoderPollInterval:               models.DefaultCoderPollInterval,
		DoerMaxWait:                     models.DefaultDoerMaxWait,
		OrchestratorPollInterval:        models.DefaultOrchestratorPollInterval,
		OrchestratorMaxWait:             models.DefaultOrchestratorMaxWait,
		ReviewerPollInterval:            models.DefaultReviewerPollInterval,
		ReviewerMaxWait:                 models.DefaultReviewerMaxWait,
		AgentProgressTimeout:            models.DefaultAgentProgressTimeoutSec,
		DefaultCLI:                      params.DefaultCLI,
		ScipSearch:                      m.scipSearchConfig.LanguageNames(),
		IntegrationBranch:               m.branch,
		EscalationWebhook:               nil,
		Mode:                            models.SystemModeRunning,
		AutoResume:                      params.AutoResume,
		NoFollowUp:                      params.NoFollowUp,
		PostWorktreeCmd:                 stringPtrOrNil(m.postWorktreeCmd),
		CopyWorktreeEnvFiles:            copyWorktreeEnvFiles,
		AgentEnvDeny:                    m.agentEnvDeny,
		AgentTools:                      m.validationTools,
	}
}

// writeState writes the initial state file; a failure removes the runtime
// directory.
func (m *masInit) writeState(state *models.State) error {
	bb := db.For(m.lizaPaths.StatePath())
	if err := bb.Write(state); err != nil {
		m.cleanupInit()
		return fmt.Errorf("failed to write state file: %w", err)
	}
	return nil
}

// writeLog writes the log file, the alerts log and the lock file. The log is a
// simple file write since it is not managed by the blackboard.
func (m *masInit) writeLog(timestamp time.Time) error {
	logContent := fmt.Sprintf(`- timestamp: %s
  agent: system
  action: initialized
  detail: %s
`, timestamp.Format(time.RFC3339), m.params.Description)

	if err := os.WriteFile(m.lizaPaths.LogPath(), []byte(logContent), initFileMode); err != nil {
		m.cleanupInit()
		return fmt.Errorf("failed to write log file: %w", err)
	}

	// Create supporting files
	if err := os.WriteFile(m.lizaPaths.AlertsLogPath(), []byte{}, initFileMode); err != nil {
		m.cleanupInit()
		return fmt.Errorf("failed to create alerts.log: %w", err)
	}

	// Create lock file
	if err := os.WriteFile(m.lizaPaths.LockPath(), []byte{}, initFileMode); err != nil {
		m.cleanupInit()
		return fmt.Errorf("failed to create lock file: %w", err)
	}
	return nil
}

// reportInitialized prints the completion summary.
func (m *masInit) reportInitialized() {
	fmt.Printf("%s initialized at %s\n", brand.NameTitle, m.lizaPaths.LizaDir())
	fmt.Printf("Integration branch: %s\n", m.branch)

	hasNonClaude := false
	for _, a := range m.params.Agents {
		if a != "claude" {
			hasNonClaude = true
			break
		}
	}
	if hasNonClaude {
		fmt.Println("Some agents require manual configuration.")
		fmt.Printf("See: https://github.com/%s/blob/main/GETTING_STARTED.md\n", brand.Repo)
	}
}
