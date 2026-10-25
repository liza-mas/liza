package scipsearch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/subprocess"
	"github.com/liza-mas/liza/internal/worktreeexclude"
)

const maxFailureDiagnosticBytes = 1024

const outputPathPlaceholder = "__SCIP_OUTPUT__"

// RuntimePlanOptions configures runtime command planning for one target root.
type RuntimePlanOptions struct {
	TargetRoot          string
	ConfiguredLanguages []string
	GitFiles            GitFilesFunc
}

// LanguageAggregatePlan describes one final per-language SCIP index assembled
// from one or more per-root language indexer outputs.
type LanguageAggregatePlan struct {
	Language    Language
	ProjectRoot string
	OutputPath  string
	IndexPlans  []RuntimeCommandPlan
}

// RuntimeCommandPlan describes one fixed command invocation.
type RuntimeCommandPlan struct {
	Language   Language
	Name       string
	Args       []string
	Dir        string
	OutputPath string
	Root       string
}

// RuntimeRunner executes one runtime scip-search command plan.
type RuntimeRunner func(RuntimeCommandPlan) (string, error)

// RefreshOptions configures one best-effort task worktree index refresh.
type RefreshOptions struct {
	// ProjectRoot holds the repo-root <language>.scip indexes the worktree
	// copies are re-rooted from.
	ProjectRoot         string
	TargetRoot          string
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
	Language Language
	Path     string
}

// RefreshFailure contains bounded diagnostics for one failed language indexer.
type RefreshFailure struct {
	Language   Language
	Diagnostic string
}

// PlanRuntimeCommands selects detected configured languages for a target root
// and returns fixed command plans without executing indexers or writing files.
func PlanRuntimeCommands(opts RuntimePlanOptions) ([]LanguageAggregatePlan, error) {
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

	return buildRuntimeCommandPlans(targetRoot, filterRuntimeLanguages(opts.ConfiguredLanguages, detectLanguages(files)), files), nil
}

// RefreshIndexes re-roots the repo-root SCIP indexes into one task worktree and
// reports per-language results; it never runs a language indexer. The index
// lifecycle hooks own the repo-root indexes. Their document paths are relative,
// but their project root is the repo root, which scip-search prints in every
// result, so each copy is rewritten with scip-search reroot to name the
// worktree. A copy lags the worktree by at most one repo-root refresh and
// excludes the task's own edits. Failures, including a missing repo-root
// index, are isolated to their language and leave it without an index.
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
	projectRoot, err := filepath.Abs(opts.ProjectRoot)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("resolve scip-search project root: %w", err)
	}

	if err := ensureTaskWorktreeScipExclude(plans[0].ProjectRoot); err != nil {
		return RefreshResult{}, err
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
		output, err := rerootProjectRootIndex(projectRoot, plan, runner)
		if err != nil {
			if cleanupErr := removeStaleIndex(plan.OutputPath); cleanupErr != nil {
				err = fmt.Errorf("%w; additionally %v", err, cleanupErr)
			}
			result.Failures = append(result.Failures, RefreshFailure{
				Language:   plan.Language,
				Diagnostic: boundedFailureDiagnostic(err, output),
			})
			continue
		}
		result.Successes = append(result.Successes, IndexRef{Language: plan.Language, Path: plan.OutputPath})
	}
	return result, nil
}

// rerootProjectRootIndex writes plan.OutputPath from the language's repo-root
// index. scip-search reroot publishes its output atomically.
func rerootProjectRootIndex(projectRoot string, plan LanguageAggregatePlan, runner RuntimeRunner) (string, error) {
	if err := removeStaleIndex(plan.OutputPath); err != nil {
		return "", err
	}
	source := projectRootIndexPath(projectRoot, plan.Language)
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("repo-root index %s not found; the worktree has no %s SCIP index until the repo-root index exists", source, plan.Language)
	}
	output, err := runner(rerootCommandPlan(plan, source))
	if err != nil {
		if strings.Contains(output, "unsupported command") {
			err = fmt.Errorf("%w (scip-search lacks the reroot command; upgrade it with %s toolchain install scip-search)", err, brand.RuntimeValues().BinaryName)
		}
		return output, err
	}
	if _, err := os.Stat(plan.OutputPath); err != nil {
		return output, fmt.Errorf("scip-search reroot did not write %s: %w", plan.OutputPath, err)
	}
	return output, nil
}

func rerootCommandPlan(plan LanguageAggregatePlan, source string) RuntimeCommandPlan {
	return RuntimeCommandPlan{
		Language:   plan.Language,
		Name:       "scip-search",
		Args:       []string{"reroot", "--index", source, "--project-root", plan.ProjectRoot, "--out", plan.OutputPath},
		Dir:        plan.ProjectRoot,
		OutputPath: plan.OutputPath,
	}
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
		if indexes, err = appendExistingIndex(indexes, plan.Language, plan.OutputPath); err != nil {
			return nil, err
		}
	}
	return indexes, nil
}

// AvailableProjectRootIndexes returns the existing repo-root indexes that the
// index lifecycle hooks publish at <root>/<language>.scip, for the configured
// languages. The hooks' install-time plan already decided which languages have
// an index, so nothing is planned here; a missing file is omitted.
func AvailableProjectRootIndexes(opts RuntimePlanOptions) ([]IndexRef, error) {
	if !RuntimeEnabled(opts.ConfiguredLanguages) {
		return nil, nil
	}
	projectRoot, err := filepath.Abs(opts.TargetRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve scip-search project root: %w", err)
	}
	var indexes []IndexRef
	for _, language := range filterRuntimeLanguages(opts.ConfiguredLanguages, supportedLanguages) {
		if indexes, err = appendExistingIndex(indexes, language, projectRootIndexPath(projectRoot, language)); err != nil {
			return nil, err
		}
	}
	return indexes, nil
}

// appendExistingIndex appends the index at path when it is an existing file.
func appendExistingIndex(indexes []IndexRef, language Language, path string) ([]IndexRef, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return indexes, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect scip-search index %q: %w", path, err)
	}
	if info.IsDir() {
		return indexes, nil
	}
	return append(indexes, IndexRef{Language: language, Path: path}), nil
}

// projectRootIndexPath is where the index lifecycle hooks publish a
// language's repo-root index.
func projectRootIndexPath(projectRoot string, language Language) string {
	return filepath.Join(projectRoot, string(language)+".scip")
}

func buildRuntimeCommandPlans(targetRoot string, languages []Language, files []string) []LanguageAggregatePlan {
	plans := make([]LanguageAggregatePlan, 0, len(languages))
	for _, language := range languages {
		outputPath := filepath.Join(targetRoot, paths.ProjectDirName(), "scip", string(language)+".scip")
		plan, ok := languageAggregatePlan(targetRoot, language, outputPath, files)
		if ok {
			plans = append(plans, plan)
		}
	}
	return plans
}

func languageAggregatePlan(targetRoot string, language Language, outputPath string, files []string) (LanguageAggregatePlan, bool) {
	indexPlans := languageIndexPlans(targetRoot, language, files)
	indexPlans = dedupeIndexPlans(indexPlans)
	if len(indexPlans) == 0 {
		return LanguageAggregatePlan{}, false
	}
	return LanguageAggregatePlan{
		Language:    language,
		ProjectRoot: targetRoot,
		OutputPath:  outputPath,
		IndexPlans:  indexPlans,
	}, true
}

func languageIndexPlans(targetRoot string, language Language, files []string) []RuntimeCommandPlan {
	switch language {
	case LanguageGo:
		return goIndexPlans(targetRoot, goRootCandidates(targetRoot, files))
	case LanguageTypeScript:
		inputs := pairingTypeScriptCommandInputs(targetRoot, files)
		if len(inputs) == 0 && languageDetected(files, LanguageTypeScript) {
			inputs = []typeScriptCommandInputs{{Cwd: targetRoot, ProjectRoot: targetRoot}}
		}
		return typeScriptIndexPlans(targetRoot, inputs)
	case LanguagePython:
		return pythonIndexPlans(targetRoot, pairingPythonCommandInputs(targetRoot, files))
	}
	return nil
}

func aggregateRootFor(targetRoot, root string) (string, bool) {
	rel, err := filepath.Rel(targetRoot, root)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "" {
		rel = "."
	}
	return rel, true
}

func commandPlanWithOutputPath(plan RuntimeCommandPlan, outputPath string) RuntimeCommandPlan {
	plan.Args = append([]string(nil), plan.Args...)
	for i, arg := range plan.Args {
		if arg == outputPathPlaceholder {
			plan.Args[i] = outputPath
		}
	}
	plan.OutputPath = outputPath
	return plan
}

func dedupeIndexPlans(plans []RuntimeCommandPlan) []RuntimeCommandPlan {
	seen := make(map[string]bool, len(plans))
	out := make([]RuntimeCommandPlan, 0, len(plans))
	for _, plan := range plans {
		key := string(plan.Language) + "\x00" + plan.Root
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, plan)
	}
	return out
}

func removeStaleIndex(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale scip-search index %q: %w", path, err)
	}
	return nil
}

func runRuntimeCommandPlan(plan RuntimeCommandPlan) (string, error) {
	return subprocess.CombinedOutput(plan.Name, plan.Args, plan.Dir)
}

func ensureTaskWorktreeScipExclude(targetRoot string) error {
	if err := worktreeexclude.EnsureRepoExclude(targetRoot, "/"+paths.ProjectDirName()+"/scip/"); err != nil {
		return fmt.Errorf("ensure task worktree scip-search exclude: %w", err)
	}
	return nil
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
