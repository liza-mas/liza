package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	bashpolicycli "github.com/liza-mas/liza/internal/bash-policy-cli"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/projectdetect"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/termutil"
)

var (
	initSembleLookPath semble.ExecutableLookup
	initSembleRunner   semble.CommandRunner

	initBashPolicyLookPath bashpolicycli.ExecutableLookup
	initBashPolicyRunner   bashpolicycli.CommandRunner

	initPreCommitLookPath = exec.LookPath
)

// SetInitPreCommitLookPathForTest replaces the pre-commit executable lookup so
// tests outside this package run init without the tool on PATH. It returns the
// restore function.
func SetInitPreCommitLookPathForTest(lookPath func(string) (string, error)) func() {
	previous := initPreCommitLookPath
	initPreCommitLookPath = lookPath
	return func() { initPreCommitLookPath = previous }
}

// InitParams holds the parameters for InitCommand.
type InitParams struct {
	Description                     string
	SpecRef                         string
	ConfigPath                      string   // --config: path to pipeline YAML
	EntryPoint                      string   // --entry-point: name of entry-point in config
	Branch                          string   // --branch: integration branch name (default: "integration")
	PostWorktreeCmd                 string   // --post-worktree-cmd: shell command to run after worktree creation
	ValidationExecution             string   // --validation-execution: "local" asserts agent CLIs validate locally
	CopyWorktreeEnvFiles            bool     // --copy-worktree-env-files: copy ignored root env files into task worktrees
	AgentEnvDeny                    []string // --agent-env-deny: host variables withheld from agents (ADR-0213)
	AutoResume                      bool     // --auto-resume: automatically resume at checkpoint and sprint completion
	NoFollowUp                      bool     // --no-follow-up: suppress top-level pipeline-transitions after the entry subpipeline
	MaxGlobalIntegrationGenerations int
	MaxInstances                    int      // --max-instances: default live-agent cap per non-orchestrator role
	DefaultCLI                      string   // --default-cli: default CLI for agent spawning
	DefaultDoerCLI                  string   // --default-doer-cli: default CLI for doer and orchestrator agent spawning
	DefaultReviewerCLI              string   // --default-reviewer-cli: default CLI for reviewer agent spawning
	ScipSearch                      []string // --scip-search: enabled SCIP languages
	ScipSearchPlans                 []string // --scip-search-plan: pairing SCIP root overrides
	Agents                          []string // --claude, --codex, --cursor, --opencode, --gemini, --mistral, --pi, --copilot
	Stdin                           io.Reader
	ForceInteractive                bool                      // bypass TTY check (for testing)
	ContractActions                 map[string]ContractAction // provider-scoped wizard actions keyed by canonical provider ID
	AutoConfirm                     bool                      // auto-confirm interactive approval prompts
}

// InitCommand initializes a new workspace.
// It creates the project runtime directory structure, generates initial state.yaml,
// validates the spec file exists, and creates the integration branch.
//
// Prerequisite: the setup command must have populated the global runtime directory.
// The stdin parameter allows for injected input in tests; pass os.Stdin for CLI usage.
func InitCommand(description string, specRef string, stdin io.Reader) error {
	return InitCommandWithConfig(InitParams{
		Description: description,
		SpecRef:     specRef,
		Stdin:       stdin,
	})
}

func runSembleInitPrewarm(projectRoot string) {
	if !semble.RuntimeEnabled() {
		return
	}
	if safety := semble.EnsureProjectRootIgnore(projectRoot); !safety.Safe {
		writeSembleDiagnostic(safety.Diagnostic)
		return
	}
	opts := semble.ValidationOptions{
		TargetRoot: projectRoot,
		LookPath:   initSembleLookPath,
		Runner:     initSembleRunner,
	}
	prewarm := semble.ExecutePrewarm(opts)
	if !prewarm.Enabled {
		return
	}
	if prewarm.Diagnostic != (semble.Diagnostic{}) {
		writeSembleDiagnostic(prewarm.Diagnostic)
		return
	}
	if !prewarm.Ready {
		return
	}
	offline := semble.CheckOfflineReadiness(opts)
	if offline.Diagnostic != (semble.Diagnostic{}) {
		writeSembleDiagnostic(offline.Diagnostic)
		return
	}
	if !offline.Ready {
		return
	}
	corpus := semble.PrepareRepository(opts)
	if corpus.Diagnostic != (semble.Diagnostic{}) {
		writeSembleDiagnostic(corpus.Diagnostic)
	}
}

func writeSembleDiagnostic(diagnostic semble.Diagnostic) {
	message := strings.TrimSpace(diagnostic.Message)
	if message == "" {
		return
	}
	if !strings.HasPrefix(message, "semble:") {
		message = "semble: " + message
	}
	fmt.Fprintln(os.Stderr, message)
}

func bashPolicyProviders(hasClaude, hasCodex, hasCursor bool) []string {
	providers := []string{}
	if hasClaude {
		providers = append(providers, bashpolicycli.ProviderClaude)
	}
	if hasCodex {
		providers = append(providers, bashpolicycli.ProviderCodex)
	}
	if hasCursor {
		providers = append(providers, bashpolicycli.ProviderCursor)
	}
	return providers
}

func providerHasAsset(items []providers.Provider, match func(providers.ActivationAssets) bool) bool {
	for _, item := range items {
		if match(item.Setup.ActivationAssets) {
			return true
		}
	}
	return false
}

func providerHas(items []providers.Provider, match func(providers.Provider) bool) bool {
	for _, item := range items {
		if match(item) {
			return true
		}
	}
	return false
}

func providerNeedsCursorHooks(provider providers.Provider) bool {
	return provider.ID == "cursor-acp" || provider.Setup.ActivationAssets.CursorHooks
}

func canonicalInitProviderIDs(ids []string) []string {
	out := make([]string, 0, len(ids)+2)
	seen := make(map[string]bool, len(ids)+2)
	appendID := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range ids {
		// The --cursor convenience flag prepares Cursor's harness dependencies;
		// cursor-acp is a synthesized ACP provider resolved from the base
		// cursor provider's acp_runtime block.
		if id == "cursor" {
			appendID("claude")
			appendID("codex")
			appendID("cursor-acp")
			continue
		}
		appendID(id)
	}
	return out
}

func bashPolicySubprocessStdin(rawStdin io.Reader, bufferedStdin *bufio.Reader) io.Reader {
	file, ok := rawStdin.(*os.File)
	if !ok {
		return bufferedStdin
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return bufferedStdin
	}
	return file
}

func runBashPolicyInits(projectRoot string, providers []string, stdin io.Reader, autoConfirm bool) {
	for _, provider := range providers {
		if runBashPolicyInit(projectRoot, provider, stdin, autoConfirm) == bashpolicycli.StatusMissing {
			return
		}
	}
}

func runBashPolicyInit(projectRoot, provider string, stdin io.Reader, autoConfirm bool) bashpolicycli.Status {
	result := bashpolicycli.InitHooks(bashpolicycli.InitHooksOptions{
		ProjectRoot: projectRoot,
		Provider:    provider,
		Stdin:       stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		LookPath:    initBashPolicyLookPath,
		Runner:      initBashPolicyRunner,
		AutoConfirm: autoConfirm,
	})
	switch result.Status {
	case bashpolicycli.StatusMissing:
		fmt.Fprintf(os.Stderr, "Warning: bash-policy requested by %s but bash-policy was not found on PATH; run '%s toolchain install --profile full --yes' and source ~/%s/toolchain/env.sh before re-running %s init.\n", bashpolicycli.EnvEnableBashPolicy, brand.BinaryName, brand.GlobalDirName, brand.BinaryName)
	case bashpolicycli.StatusFailed:
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize or activate bash-policy hooks: %s\n", result.Diagnostic())
	}
	return result.Status
}

// InitCommandWithConfig initializes a workspace with optional pipeline config.
//
// It runs the MAS init phases in order: input resolution and validation, project
// layout, provider, contract and hook installation, configuration and state
// composition, state write, log, integration branch. Each phase is a step of
// masInit (init_mas_*.go).
func InitCommandWithConfig(params InitParams) error {
	m, err := newMASInit(params)
	if err != nil {
		return err
	}
	if err := m.loadPipeline(); err != nil {
		return err
	}
	if err := m.resolveProject(); err != nil {
		return err
	}
	if err := m.requireGlobalConfig(); err != nil {
		return err
	}
	if err := m.requireIntegrationBranchCreatable(); err != nil {
		return err
	}
	if err := m.resolveScipSearch(); err != nil {
		return err
	}
	if err := m.confirmPostWorktreeCmd(); err != nil {
		return err
	}
	if err := m.activateContracts(); err != nil {
		return err
	}

	if err := m.cleanPriorProject(); err != nil {
		return err
	}
	if err := m.createLayout(); err != nil {
		return err
	}
	if err := m.writeRuntimeFiles(); err != nil {
		return err
	}
	m.installProviderAssets()

	m.resolvePostWorktreeCmd()
	m.warnMissingNodeDependencies()
	if err := m.resolveValidationTools(); err != nil {
		return err
	}
	if err := m.activateIndexing(); err != nil {
		return err
	}

	timestamp := time.Now().UTC()
	if err := m.writeState(m.composeState(timestamp)); err != nil {
		return err
	}
	if err := m.writeLog(timestamp); err != nil {
		return err
	}
	if err := m.ensureIntegrationBranch(); err != nil {
		return err
	}
	m.reportInitialized()
	return nil
}

// stringPtrOrNil returns a pointer to s if non-empty, otherwise nil.
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isInteractive returns true if r is connected to a terminal.
// Returns false for pipes, redirected input, or non-file readers (e.g. strings.Reader in tests).
func isInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// detectInstallCmdInDir checks a single directory for package.json and returns
// the appropriate install command based on which lockfile is present.
// Returns "" if no package.json is found.
func detectInstallCmdInDir(dir string) string {
	return projectdetect.DetectInstallCmdInDir(dir)
}

// detectNodeSubdirs returns sorted subdirectory names (depth 1) containing
// package.json. Dotfile-prefixed directories and node_modules are skipped —
// these commonly contain stray package.json files (build outputs, vendored
// deps) that don't represent real project directories.
func detectNodeSubdirs(projectRoot string) []string {
	return projectdetect.DetectNodeSubdirs(projectRoot)
}

// detectPostWorktreeCmd checks the project root (and immediate subdirectories
// if nothing at root) for package.json, returning the appropriate install
// command. For multiple subdirectories, returns "" — the caller should print
// a manual-configuration message instead of guessing at a compound command.
func detectPostWorktreeCmd(projectRoot string) string {
	return projectdetect.DetectPostWorktreeCmd(projectRoot)
}

// confirmMissingPostWorktreeCmd guards against forgetting --post-worktree-cmd.
// Auto-detection only covers Node layouts; every other stack would otherwise
// initialize silently and hand agents worktrees with no dependencies or build
// artifacts. Interactive callers must confirm; non-interactive callers get the
// warning only, since there is no one to answer.
func confirmMissingPostWorktreeCmd(params InitParams, projectRoot string, stdin *bufio.Reader, rawStdin io.Reader) error {
	if params.PostWorktreeCmd != "" || detectPostWorktreeCmd(projectRoot) != "" {
		return nil
	}
	nodeSubdirs := detectNodeSubdirs(projectRoot)
	if len(nodeSubdirs) > 1 {
		fmt.Fprintf(os.Stderr, "⚠️  No --post-worktree-cmd set, and auto-detection found multiple Node.js projects (%s), so no single setup command could be selected.\n", strings.Join(nodeSubdirs, ", "))
	} else {
		fmt.Fprintln(os.Stderr, "⚠️  No --post-worktree-cmd set, and auto-detection found no Node.js project layout.")
	}
	fmt.Fprintln(os.Stderr, "Task worktrees are fresh checkouts: no installed dependencies, no build artifacts. Agent builds and tests can fail for environment reasons and burn iterations.")
	fmt.Fprintf(os.Stderr, "Set one with: %s init \"<goal>\" --post-worktree-cmd \"make setup\", or %s config set config.post_worktree_cmd \"make setup\" later.\n", brand.BinaryName, brand.BinaryName)
	fmt.Fprintln(os.Stderr, "For an empty repository, you can continue without a setup command. After scaffolding merges, supported layouts may be detected automatically; otherwise validate and set your project's command.")
	if !params.ForceInteractive && !params.AutoConfirm && !isInteractive(rawStdin) {
		return nil
	}
	fmt.Fprint(os.Stderr, "Continue without a post-worktree setup command? (y/n): ")
	if params.AutoConfirm {
		fmt.Fprintln(os.Stderr, "yes")
		return nil
	}
	response, err := termutil.ReadSingleKey(stdin)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		// No answerable input (isInteractive also accepts /dev/null, so
		// scripted callers reach this prompt). Nobody is there to answer:
		// the warning stands and initialization continues, as it does on
		// the non-interactive path above.
		return nil
	}
	if response != confirmAnswerY {
		return fmt.Errorf("initialization cancelled by user")
	}
	return nil
}

// resolveValidationExecutionTools returns agent_tools entries asserting
// validation_execution: local for every CLI the pipeline's roles launch with by
// default, when the operator asserts it (flag or prompt). The assertion is the
// operator's (ADR-0136), so auto-confirm and non-interactive runs never make it.
// Without it, declared validation prerequisites stay off.
func resolveValidationExecutionTools(params InitParams, projectRoot string, pipelineCfg *pipeline.PipelineConfig, stdin *bufio.Reader, rawStdin io.Reader) (map[string]models.AgentToolConfig, error) {
	asserted := params.ValidationExecution == agent.ValidationExecutionLocal
	ask := !asserted && !params.AutoConfirm && (params.ForceInteractive || isInteractive(rawStdin))
	if !asserted && !ask {
		fmt.Fprintf(os.Stderr, "Validation prerequisites are off; pass --validation-execution %s if the agent CLIs run validation commands locally.\n", agent.ValidationExecutionLocal)
		return nil, nil
	}
	resolver := pipeline.NewResolver(pipelineCfg)
	roleModels, err := rolemodels.Load(projectRoot)
	var roleCLIs []agent.RoleCLI
	if err == nil {
		roleCLIs, err = agent.DefaultRoleCLIs(resolver.AllRoleNames(), resolver, models.Config{DefaultCLI: params.DefaultCLI}, roleModels)
	}
	if err != nil {
		if asserted {
			return nil, fmt.Errorf("--validation-execution: cannot resolve the agent CLIs to assert it for: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Skipping validation execution setup: %v\n", err)
		return nil, nil
	}
	var clis []string
	for _, rc := range roleCLIs {
		if !slices.Contains(clis, rc.CLI) {
			clis = append(clis, rc.CLI)
		}
	}
	slices.Sort(clis)
	if ask {
		fmt.Fprintf(os.Stderr, "Do these agent CLIs run validation commands locally, in this host's environment and the task worktree: %s?\n", strings.Join(clis, ", "))
		fmt.Fprint(os.Stderr, "Yes enables declared validation prerequisites, checked before agents claim work. (y/n): ")
		response, err := termutil.ReadSingleKey(stdin)
		fmt.Fprintln(os.Stderr)
		if err != nil || response != confirmAnswerY {
			return nil, nil
		}
	} else {
		fmt.Fprintf(os.Stderr, "Validation execution: %s for %s\n", agent.ValidationExecutionLocal, strings.Join(clis, ", "))
	}
	tools := make(map[string]models.AgentToolConfig, len(clis))
	for _, cli := range clis {
		tools[cli] = models.AgentToolConfig{ValidationExecution: agent.ValidationExecutionLocal}
	}
	return tools, nil
}

// detectPkgManagerContext returns a human-readable description of what was
// detected (e.g. "package.json + yarn.lock") for the suggestion prompt.
func detectPkgManagerContext(projectRoot string) string {
	return projectdetect.DetectPkgManagerContext(projectRoot)
}

func entryPointNames(cfg *pipeline.PipelineConfig) string {
	names := make([]string, 0, len(cfg.Pipeline.EntryPoints))
	for name := range cfg.Pipeline.EntryPoints {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// initModelsFile renders the initial models.yaml. The CLI init flags seed its
// defaults; roles without a flag stay commented out so they keep resolving
// through the configured CLI chain (including role environment variables).
func initModelsFile(params InitParams) []byte {
	doer, reviewer := params.DefaultDoerCLI, params.DefaultReviewerCLI
	if doer == "" {
		doer = params.DefaultCLI
	}
	if reviewer == "" {
		reviewer = params.DefaultCLI
	}
	var b strings.Builder
	b.WriteString(`# Per-role CLI and model selection, read at every agent start.
# An entry is {cli: <cli>, model: <model>}; model is optional and is supported
# for claude and codex. defaults.doer also covers orchestrators. Roles without
# an entry use the configured default CLI. An agent started with --cli or
# --profile ignores this file; --model replaces only the entry's model.
# A reviewer entry may be a list assigning quorum slots: item 1 reviews first,
# item 2 gives the second approval, the last item any later one.
`)
	if doer != "" || reviewer != "" {
		b.WriteString("defaults:\n")
		for _, d := range []struct{ key, cli string }{{string(roles.RoleTypeDoer), doer}, {string(roles.RoleTypeReviewer), reviewer}} {
			if d.cli != "" {
				fmt.Fprintf(&b, "  %s: {cli: %s}\n", d.key, d.cli)
			}
		}
	} else {
		b.WriteString(`#
# defaults:
#   doer: {cli: claude, model: <model>}
#   reviewer: {cli: codex, model: <model>}
`)
	}
	b.WriteString(`#
# roles:
#   <role>: {cli: claude, model: <model>}
#   <reviewer-role>:
#     - {cli: codex, model: <model>}
#     - {cli: claude, model: <model>}
`)
	return []byte(b.String())
}

// initAgentEnvDeny merges --agent-env-deny names with the comma-separated
// environment seed, flag names first, dropping exact repeats.
func initAgentEnvDeny(flagNames []string) []string {
	var names []string
	for _, name := range append(slices.Clone(flagNames), strings.Split(envgate.Value(models.EnvAgentEnvDeny), ",")...) {
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}
