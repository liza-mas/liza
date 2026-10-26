package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/initcheck"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/scipsearch"
)

// defaultInitBranch is the integration branch used when --branch is not given.
const defaultInitBranch = "integration"

// masInit carries the resolved inputs of one MAS init run from one phase to the
// next. InitCommandWithConfig runs the phases in a fixed order; every phase
// reads what the earlier ones resolved and none is reused by another command.
type masInit struct {
	params            InitParams
	branch            string
	agentEnvDeny      []string
	rawStdin          io.Reader
	stdin             *bufio.Reader
	confirmOptions    embedded.ConfirmOptions
	catalog           providers.Catalog
	selectedProviders []providers.Provider
	pipelineCfg       *pipeline.PipelineConfig
	pipelineData      []byte
	lizaPaths         paths.LizaPaths
	specRepoRel       string
	globalCoreFile    string
	scipSearchConfig  scipsearch.InitResult
	postWorktreeCmd   string
	validationTools   map[string]models.AgentToolConfig
}

// newMASInit validates the flag values that need no project and resolves the
// input stream and the selected providers.
func newMASInit(params InitParams) (*masInit, error) {
	branch := params.Branch
	if branch == "" {
		branch = defaultInitBranch
	}
	if len(params.ScipSearchPlans) > 0 {
		return nil, fmt.Errorf("--scip-search-plan is only supported for pairing init without a description")
	}

	// Validate branch name using git's own ref format rules
	if err := validateBranchName(branch); err != nil {
		return nil, fmt.Errorf("invalid branch name %q: %w", branch, err)
	}
	agentEnvDeny := initAgentEnvDeny(params.AgentEnvDeny)
	if err := models.ValidateAgentEnvDeny(agentEnvDeny); err != nil {
		return nil, fmt.Errorf("invalid agent_env_deny: %w", err)
	}
	rawStdin := params.Stdin
	if rawStdin == nil {
		rawStdin = os.Stdin
	}
	catalog := loadProviderCatalog("")
	selectedProviders, err := resolveCatalogProviders(catalog, canonicalInitProviderIDs(params.Agents))
	if err != nil {
		return nil, err
	}
	return &masInit{
		params:       params,
		branch:       branch,
		agentEnvDeny: agentEnvDeny,
		rawStdin:     rawStdin,
		// Single shared buffered reader — avoids multiple bufio.NewReader instances
		// consuming from the same underlying reader (which causes EOF for later readers).
		stdin:             bufio.NewReader(rawStdin),
		confirmOptions:    embedded.ConfirmOptions{AutoConfirm: params.AutoConfirm},
		catalog:           catalog,
		selectedProviders: selectedProviders,
	}, nil
}

// loadPipeline validates and loads the pipeline config early (before creating
// the project runtime directory), then checks the entry point and the
// validation-execution assertion against it.
func (m *masInit) loadPipeline() error {
	if m.params.ConfigPath != "" {
		absConfigPath, err := filepath.Abs(m.params.ConfigPath)
		if err != nil {
			return fmt.Errorf("failed to resolve config path: %w", err)
		}
		m.pipelineCfg, err = pipeline.Load(absConfigPath)
		if err != nil {
			return fmt.Errorf("invalid pipeline config: %w", err)
		}
		m.pipelineData, err = os.ReadFile(absConfigPath)
		if err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		// Auto-freeze embedded pipeline config when --config is not provided
		m.pipelineData = embedded.PipelineConfigContent()
		var err error
		m.pipelineCfg, err = pipeline.LoadFromBytes(m.pipelineData)
		if err != nil {
			return fmt.Errorf("invalid embedded pipeline config: %w", err)
		}
	}

	// Validate entry-point if provided
	if entryPoint := m.params.EntryPoint; entryPoint != "" {
		if _, ok := m.pipelineCfg.Pipeline.EntryPoints[entryPoint]; !ok {
			return fmt.Errorf("entry-point %q not found in pipeline config (available: %s)",
				entryPoint, entryPointNames(m.pipelineCfg))
		}
	}

	if v := m.params.ValidationExecution; v != "" && v != agent.ValidationExecutionLocal {
		return fmt.Errorf("--validation-execution must be %q, got %q", agent.ValidationExecutionLocal, v)
	}
	return nil
}

// resolveProject locates the project and checks the committed spec and
// pre-commit config that init requires of it.
func (m *masInit) resolveProject() error {
	lizaPaths, err := paths.LizaPathsFromGit()
	if err != nil {
		return fmt.Errorf("failed to setup paths: %w", err)
	}
	m.lizaPaths = lizaPaths

	warnLegacyProjectRoot(lizaPaths.ProjectRoot(), lizaPaths.LizaDir())

	// Resolve spec file relative to cwd (where user ran the command), not project root
	specRef := m.params.SpecRef
	specPath, err := filepath.Abs(specRef)
	if err != nil {
		return fmt.Errorf("failed to resolve spec path: %w", err)
	}
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		return fmt.Errorf("spec file does not exist: %s\nCreate spec document first. See templates/vision-template.md", specRef)
	}
	m.specRepoRel, err = initcheck.EnsureSpecCommittedClean(lizaPaths.ProjectRoot(), specPath)
	if err != nil {
		return err
	}
	if _, err := initcheck.EnsurePreCommitConfigCommittedClean(lizaPaths.ProjectRoot(), m.branch); err != nil {
		return err
	}
	// The committed config is useless to agents without the tool that runs it:
	// they would discover the gap only when a hook-driven validation fails.
	if _, err := initPreCommitLookPath("pre-commit"); err != nil {
		return fmt.Errorf("pre-commit is not on PATH; agents need it to run the committed .pre-commit-config.yaml. Install it (%s) or add it to PATH, then re-run %s", brand.Command("toolchain", "install", "--include", "pre-commit"), brand.Command("init"))
	}
	return nil
}

// requireGlobalConfig checks that setup installed the global contracts.
func (m *masInit) requireGlobalConfig() error {
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return fmt.Errorf("failed to determine global config path: %w", err)
	}
	m.globalCoreFile = filepath.Join(globalDir, "CORE.md")
	if _, err := os.Stat(m.globalCoreFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("global config not found at %s\nRun '%s setup' first to install contracts, skills, and support docs", globalDir, brand.BinaryName)
		}
		return fmt.Errorf("cannot access global config at %s: %w\nCheck permissions on %s", m.globalCoreFile, err, globalDir)
	}
	return nil
}

// resolveScipSearch resolves the SCIP languages and reports its diagnostics and
// warnings.
func (m *masInit) resolveScipSearch() error {
	scipSearchConfig, err := scipsearch.ResolveInitConfig(scipsearch.InitOptions{
		ProjectRoot:       m.lizaPaths.ProjectRoot(),
		ExplicitLanguages: m.params.ScipSearch,
		EnvValue:          envgate.Value(scipsearch.EnvEnableScipSearch),
	})
	if err != nil {
		return err
	}
	m.scipSearchConfig = scipSearchConfig
	for _, diagnostic := range scipSearchConfig.Diagnostics {
		fmt.Fprintf(os.Stderr, "scip-search: %s\n", diagnostic)
	}
	for _, warning := range scipSearchConfig.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}
	return nil
}

// confirmPostWorktreeCmd asks, before anything is installed, whether to proceed
// without a post-worktree command.
func (m *masInit) confirmPostWorktreeCmd() error {
	return confirmMissingPostWorktreeCmd(m.params, m.lizaPaths.ProjectRoot(), m.stdin, m.rawStdin)
}
