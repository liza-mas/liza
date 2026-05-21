package scipsearch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/gitenv"
)

const EnvEnableScipSearch = "LIZA_ENABLE_SCIP_SEARCH"

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

var (
	runnerMu      sync.Mutex
	defaultRunner CommandRunner = runCommand
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
