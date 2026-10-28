package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/termutil"
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

// initialStateInputs maps the resolved initialization inputs for the ops
// operation; the copy-flag environment read stays here, with input resolution.
func (m *masInit) initialStateInputs(timestamp time.Time) ops.InitialStateInputs {
	params := m.params
	return ops.InitialStateInputs{
		Description:                     params.Description,
		SpecRef:                         m.specRepoRel,
		EntryPoint:                      params.EntryPoint,
		Timestamp:                       timestamp,
		Branch:                          m.branch,
		DefaultCLI:                      params.DefaultCLI,
		AutoResume:                      params.AutoResume,
		NoFollowUp:                      params.NoFollowUp,
		CopyWorktreeEnvFiles:            params.CopyWorktreeEnvFiles || envgate.TruthyEnv(models.EnvEnableCopyWorktreeEnvFiles),
		MaxGlobalIntegrationGenerations: params.MaxGlobalIntegrationGenerations,
		MaxInstances:                    params.MaxInstances,
		ScipSearch:                      m.scipSearchConfig.LanguageNames(),
		PostWorktreeCmd:                 m.postWorktreeCmd,
		AgentEnvDeny:                    m.agentEnvDeny,
		AgentTools:                      m.validationTools,
	}
}

// writeState writes the initial state file; a failure removes the runtime
// directory.
func (m *masInit) writeState(in ops.InitialStateInputs) error {
	if err := ops.PublishInitialState(m.lizaPaths.StatePath(), in); err != nil {
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
