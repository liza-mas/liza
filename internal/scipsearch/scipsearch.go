package scipsearch

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/gitenv"
)

const EnvEnableScipSearch = "LIZA_ENABLE_SCIP_SEARCH"

const maxFailureDiagnosticBytes = 1024

var supportedLanguages = []string{"go", "typescript", "python"}

var languageIndexers = map[string]string{
	"go":         "scip-go",
	"typescript": "scip-typescript",
	"python":     "scip-python",
}

// CommandRunner runs a fixed executable name with argv entries.
type CommandRunner func(name string, args ...string) (string, error)

// GitFilesFunc returns git-tracked files for a project root.
type GitFilesFunc func(projectRoot string) ([]string, error)

type InitOptions struct {
	ProjectRoot       string
	ExplicitLanguages []string
	EnvValue          string
	CommandRunner     CommandRunner
	GitFiles          GitFilesFunc
}

type InitResult struct {
	Languages   []string
	Diagnostics []string
	Warnings    []string
}

// RuntimePlanOptions configures runtime command planning for one target root.
type RuntimePlanOptions struct {
	TargetRoot          string
	ConfiguredLanguages []string
	GitFiles            GitFilesFunc
}

// RuntimeCommandPlan describes one fixed language-indexer invocation.
type RuntimeCommandPlan struct {
	Language   string
	Name       string
	Args       []string
	Dir        string
	OutputPath string
}

// RuntimeRunner executes one runtime indexer command plan.
type RuntimeRunner func(RuntimeCommandPlan) (string, error)

// TargetKind identifies the lifecycle target being refreshed.
type TargetKind string

const (
	TargetKindProjectRoot  TargetKind = "project-root"
	TargetKindTaskWorktree TargetKind = "task-worktree"
)

// RefreshOptions configures one best-effort runtime index refresh.
type RefreshOptions struct {
	TargetRoot          string
	TargetKind          TargetKind
	ConfiguredLanguages []string
	GitFiles            GitFilesFunc
	Runner              RuntimeRunner
}

// RefreshResult contains successful indexes and isolated language failures from
// one refresh attempt.
type RefreshResult struct {
	Successes []IndexRef
	Failures  []RefreshFailure
}

// IndexRef identifies one prompt-safe generated index file.
type IndexRef struct {
	Language string
	Path     string
}

// RefreshFailure contains bounded diagnostics for one failed language indexer.
type RefreshFailure struct {
	Language   string
	Diagnostic string
}

var (
	runnerMu                    sync.Mutex
	taskWorktreeExcludeConfigMu sync.Mutex
	defaultRunner               CommandRunner = runCommand
)

// SetCommandRunnerForTest replaces the process runner until the returned restore
// function is called. It is intended for package-level init integration tests.
func SetCommandRunnerForTest(runner CommandRunner) func() {
	runnerMu.Lock()
	previous := defaultRunner
	defaultRunner = runner
	runnerMu.Unlock()

	return func() {
		runnerMu.Lock()
		defaultRunner = previous
		runnerMu.Unlock()
	}
}

func ResolveInitConfig(opts InitOptions) (InitResult, error) {
	runner := opts.CommandRunner
	if runner == nil {
		runner = getDefaultRunner()
	}
	gitFiles := opts.GitFiles
	if gitFiles == nil {
		gitFiles = listGitFiles
	}

	var result InitResult
	if output, err := runner("scip-search", "--help"); err != nil {
		return result, fmt.Errorf("scip-search setup validation failed: scip-search --help failed: %w%s", err, outputSuffix(output))
	}

	if output, err := runner("scip-search", "--version"); err == nil {
		if version := strings.TrimSpace(output); version != "" {
			result.Diagnostics = append(result.Diagnostics, "scip-search --version: "+version)
		}
	}

	selected, err := selectLanguages(opts, gitFiles)
	if err != nil {
		return result, err
	}

	result.Languages, result.Warnings = validateIndexers(selected, runner)
	return result, nil
}

func ParseEnvGate(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// RuntimeEnabled reports whether runtime scip-search behavior is active for the
// current process. It is true only when LIZA_ENABLE_SCIP_SEARCH is truthy and at
// least one configured language from Config.ScipSearch remains available.
func RuntimeEnabled(configuredLanguages []string) bool {
	return ParseEnvGate(os.Getenv(EnvEnableScipSearch)) && len(configuredLanguages) > 0
}

// PlanRuntimeCommands selects detected configured languages for a target root
// and returns fixed command plans without executing indexers or writing files.
func PlanRuntimeCommands(opts RuntimePlanOptions) ([]RuntimeCommandPlan, error) {
	if !RuntimeEnabled(opts.ConfiguredLanguages) {
		return nil, nil
	}

	targetRoot, err := filepath.Abs(opts.TargetRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve scip-search target root: %w", err)
	}

	gitFiles := opts.GitFiles
	if gitFiles == nil {
		gitFiles = listGitFiles
	}
	files, err := gitFiles(targetRoot)
	if err != nil {
		return nil, fmt.Errorf("detect runtime scip-search languages: %w", err)
	}

	return buildRuntimeCommandPlans(targetRoot, filterRuntimeLanguages(opts.ConfiguredLanguages, detectLanguages(files))), nil
}

// RefreshIndexes executes selected runtime indexer command plans and reports
// per-language results. Indexer failures are isolated to their language.
func RefreshIndexes(opts RefreshOptions) (RefreshResult, error) {
	plans, err := PlanRuntimeCommands(RuntimePlanOptions{
		TargetRoot:          opts.TargetRoot,
		ConfiguredLanguages: opts.ConfiguredLanguages,
		GitFiles:            opts.GitFiles,
	})
	if err != nil {
		return RefreshResult{}, err
	}
	if len(plans) == 0 {
		return RefreshResult{}, nil
	}

	if opts.TargetKind == TargetKindTaskWorktree {
		if err := ensureTaskWorktreeScipExclude(plans[0].Dir); err != nil {
			return RefreshResult{}, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(plans[0].OutputPath), 0o755); err != nil {
		return RefreshResult{}, fmt.Errorf("create scip-search index directory: %w", err)
	}

	runner := opts.Runner
	if runner == nil {
		runner = runRuntimeCommandPlan
	}

	var result RefreshResult
	for _, plan := range plans {
		if err := removeStaleIndex(plan.OutputPath); err != nil {
			result.Failures = append(result.Failures, RefreshFailure{
				Language:   plan.Language,
				Diagnostic: boundedFailureDiagnostic(err, ""),
			})
			continue
		}
		output, err := runner(plan)
		if err != nil {
			diagnosticErr := err
			if cleanupErr := removeStaleIndex(plan.OutputPath); cleanupErr != nil {
				diagnosticErr = fmt.Errorf("%w; additionally %v", err, cleanupErr)
			}
			result.Failures = append(result.Failures, RefreshFailure{
				Language:   plan.Language,
				Diagnostic: boundedFailureDiagnostic(diagnosticErr, output),
			})
			continue
		}
		if _, err := os.Stat(plan.OutputPath); err != nil {
			result.Failures = append(result.Failures, RefreshFailure{
				Language:   plan.Language,
				Diagnostic: boundedFailureDiagnostic(fmt.Errorf("indexer did not write %s: %w", plan.OutputPath, err), output),
			})
			continue
		}
		result.Successes = append(result.Successes, IndexRef{Language: plan.Language, Path: plan.OutputPath})
	}
	return result, nil
}

// AvailableIndexes returns existing absolute index paths for selected runtime
// languages. Missing files are omitted rather than reported as failures.
func AvailableIndexes(opts RuntimePlanOptions) ([]IndexRef, error) {
	plans, err := PlanRuntimeCommands(opts)
	if err != nil {
		return nil, err
	}

	indexes := make([]IndexRef, 0, len(plans))
	for _, plan := range plans {
		info, err := os.Stat(plan.OutputPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect scip-search index %q: %w", plan.OutputPath, err)
		}
		if info.IsDir() {
			continue
		}
		indexes = append(indexes, IndexRef{Language: plan.Language, Path: plan.OutputPath})
	}
	return indexes, nil
}

func selectLanguages(opts InitOptions, gitFiles GitFilesFunc) ([]string, error) {
	if len(opts.ExplicitLanguages) > 0 {
		return canonicalizeExplicit(opts.ExplicitLanguages)
	}
	if !ParseEnvGate(opts.EnvValue) {
		return nil, nil
	}
	files, err := gitFiles(opts.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to auto-detect scip-search languages with git ls-files: %w", err)
	}
	return detectLanguages(files), nil
}

func canonicalizeExplicit(raw []string) ([]string, error) {
	seen := make(map[string]bool, len(raw))
	for _, language := range raw {
		language = strings.TrimSpace(language)
		if !slices.Contains(supportedLanguages, language) {
			return nil, fmt.Errorf("unsupported scip-search language %q (supported: %s)", language, strings.Join(supportedLanguages, ", "))
		}
		seen[language] = true
	}

	var out []string
	for _, language := range supportedLanguages {
		if seen[language] {
			out = append(out, language)
		}
	}
	return out, nil
}

func detectLanguages(files []string) []string {
	detected := map[string]bool{}
	for _, file := range files {
		base := filepath.Base(file)
		ext := filepath.Ext(file)
		switch {
		case base == "go.mod" || ext == ".go":
			detected["go"] = true
		case base == "tsconfig.json" || ext == ".ts" || ext == ".tsx":
			detected["typescript"] = true
		case base == "pyproject.toml" || ext == ".py":
			detected["python"] = true
		}
	}

	var out []string
	for _, language := range supportedLanguages {
		if detected[language] {
			out = append(out, language)
		}
	}
	return out
}

func filterRuntimeLanguages(configuredLanguages, detectedLanguages []string) []string {
	configured := make(map[string]bool, len(configuredLanguages))
	for _, language := range configuredLanguages {
		configured[language] = true
	}
	detected := make(map[string]bool, len(detectedLanguages))
	for _, language := range detectedLanguages {
		detected[language] = true
	}

	var out []string
	for _, language := range supportedLanguages {
		if configured[language] && detected[language] {
			out = append(out, language)
		}
	}
	return out
}

func buildRuntimeCommandPlans(targetRoot string, languages []string) []RuntimeCommandPlan {
	plans := make([]RuntimeCommandPlan, 0, len(languages))
	for _, language := range languages {
		outputPath := filepath.Join(targetRoot, ".liza", "scip", language+".scip")
		plans = append(plans, runtimeCommandPlan(targetRoot, language, outputPath))
	}
	return plans
}

func runtimeCommandPlan(targetRoot, language, outputPath string) RuntimeCommandPlan {
	plan := RuntimeCommandPlan{
		Language:   language,
		Name:       languageIndexers[language],
		Dir:        targetRoot,
		OutputPath: outputPath,
	}
	switch language {
	case "go":
		plan.Args = []string{"index", "--module-root", targetRoot, "--skip-tests", "--output", outputPath}
	case "typescript":
		plan.Args = []string{"index", "--cwd", targetRoot, "--output", outputPath, targetRoot}
	case "python":
		plan.Args = []string{"index", "--cwd", targetRoot, "--output", outputPath}
	}
	return plan
}

func validateIndexers(languages []string, runner CommandRunner) ([]string, []string) {
	var validated []string
	var warnings []string
	for _, language := range languages {
		indexer := languageIndexers[language]
		if output, err := runner(indexer, "--help"); err != nil {
			warnings = append(warnings, fmt.Sprintf("dropping scip-search language %q: %s --help failed: %v%s", language, indexer, err, outputSuffix(output)))
			continue
		}
		validated = append(validated, language)
	}
	return validated, warnings
}

func getDefaultRunner() CommandRunner {
	runnerMu.Lock()
	defer runnerMu.Unlock()
	return defaultRunner
}

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func runRuntimeCommandPlan(plan RuntimeCommandPlan) (string, error) {
	cmd := exec.Command(plan.Name, plan.Args...)
	cmd.Dir = plan.Dir
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func removeStaleIndex(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale scip-search index %q: %w", path, err)
	}
	return nil
}

func ensureTaskWorktreeScipExclude(targetRoot string) error {
	output, err := gitenv.Output(targetRoot, "rev-parse", "--git-dir")
	if err != nil {
		return fmt.Errorf("resolve task worktree gitdir: %w", err)
	}

	gitDir := strings.TrimSpace(string(output))
	if gitDir == "" {
		return fmt.Errorf("resolve task worktree gitdir: git rev-parse --git-dir returned empty path")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(targetRoot, gitDir)
	}

	excludePath := filepath.Join(gitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("create task worktree exclude directory: %w", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read task worktree exclude: %w", err)
	}
	if hasTaskWorktreeScipExclude(content) {
		return configureTaskWorktreeExclude(targetRoot, excludePath)
	}

	next := slices.Clone(content)
	if len(next) > 0 && next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	next = append(next, ".liza/scip/\n"...)
	if err := os.WriteFile(excludePath, next, 0o644); err != nil {
		return fmt.Errorf("write task worktree exclude: %w", err)
	}
	if err := configureTaskWorktreeExclude(targetRoot, excludePath); err != nil {
		return err
	}
	return nil
}

func hasTaskWorktreeScipExclude(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == ".liza/scip/" {
			return true
		}
	}
	return false
}

func configureTaskWorktreeExclude(targetRoot, excludePath string) error {
	taskWorktreeExcludeConfigMu.Lock()
	defer taskWorktreeExcludeConfigMu.Unlock()

	// Linked worktrees do not consult their private info/exclude unless
	// worktree-specific config points core.excludesFile at it.
	worktreeConfigEnabled := false
	output, err := gitenv.Output(targetRoot, "config", "--get", "extensions.worktreeConfig")
	if err == nil {
		worktreeConfigEnabled = strings.EqualFold(strings.TrimSpace(string(output)), "true")
	} else if !gitConfigUnset(err) {
		return fmt.Errorf("inspect task worktree config extension: %w", err)
	}

	if output, err := gitenv.CombinedOutput(targetRoot, "config", "extensions.worktreeConfig", "true"); err != nil {
		return fmt.Errorf("enable task worktree config for scip-search exclude: %w%s", err, outputSuffix(string(output)))
	}
	if !worktreeConfigEnabled {
		log.Printf("INFO: enabled git extensions.worktreeConfig for scip-search task worktree excludes in %s", targetRoot)
	}

	output, err = gitenv.Output(targetRoot, "config", "--worktree", "--get", "core.excludesFile")
	if err == nil {
		current := strings.TrimSpace(string(output))
		if current != "" && filepath.Clean(current) != filepath.Clean(excludePath) {
			return fmt.Errorf("task worktree core.excludesFile already configured as %q", current)
		}
	}
	if err != nil && !gitConfigUnset(err) {
		return fmt.Errorf("inspect task worktree core.excludesFile: %w", err)
	}

	if output, err := gitenv.CombinedOutput(targetRoot, "config", "--worktree", "core.excludesFile", excludePath); err != nil {
		return fmt.Errorf("configure task worktree scip-search exclude: %w%s", err, outputSuffix(string(output)))
	}
	return nil
}

func gitConfigUnset(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

func listGitFiles(projectRoot string) ([]string, error) {
	output, err := gitenv.Output(projectRoot, "ls-files")
	if err != nil {
		return nil, err
	}

	var files []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func outputSuffix(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return ": " + output
}

func boundedFailureDiagnostic(err error, output string) string {
	diagnostic := err.Error()
	if output = strings.TrimSpace(output); output != "" {
		diagnostic += ": " + output
	}
	if len(diagnostic) <= maxFailureDiagnosticBytes {
		return diagnostic
	}
	return diagnostic[:maxFailureDiagnosticBytes]
}
