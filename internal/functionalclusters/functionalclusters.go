// Package functionalclusters owns the optional Functional Clusters runtime
// indexing contract.
//
// Functional Clusters is downstream of Stacklit and scip-search: refresh callers
// require both prerequisite index families to be enabled and available, then run
// temporary exports before writing the target-local functional-clusters.json.
// Prompt callers expose only explicit existing artifact paths when the
// Functional Clusters gate is enabled.
package functionalclusters

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/subprocess"
	"github.com/liza-mas/liza/internal/worktreeexclude"
)

var EnvEnableFunctionalClusters = brand.EnvName("ENABLE_FUNCTIONAL_CLUSTERS")

const outputArtifactName = "functional-clusters.json"
const stacklitArchitectureArtifactName = "stacklit-architecture.json"
const maxFailureDiagnosticBytes = 1024

// RuntimeCommandPlan describes one fixed Functional Clusters prerequisite or
// build command.
type RuntimeCommandPlan struct {
	Name       string
	Args       []string
	Dir        string
	OutputPath string
}

// RuntimeRunner executes one runtime command plan.
type RuntimeRunner func(RuntimeCommandPlan) (string, error)

// RefreshOptions configures one best-effort runtime Functional Clusters refresh.
type RefreshOptions struct {
	TargetRoot          string
	ConfiguredLanguages []string
	Runner              RuntimeRunner
}

// RefreshResult contains the generated artifact and isolated failure diagnostics.
type RefreshResult struct {
	Successes []IndexRef
	Failures  []RefreshFailure
}

// IndexRef identifies one prompt-safe generated Functional Clusters artifact.
type IndexRef struct {
	Path string
}

// RefreshFailure contains bounded diagnostics for a failed Functional Clusters build.
type RefreshFailure struct {
	Diagnostic string
}

// RuntimePlanOptions configures read-only Functional Clusters artifact discovery.
type RuntimePlanOptions struct {
	TargetRoot string
}

var (
	runnerMu sync.Mutex
	// taskWorktreeFunctionalClustersGitStateMu serializes skip-worktree and
	// worktree-private exclude updates for functional-clusters.json.
	taskWorktreeFunctionalClustersGitStateMu sync.Mutex
	defaultRunner                            RuntimeRunner = runRuntimeCommandPlan
)

// SetRuntimeRunnerForTest replaces the process runner until the returned
// restore function is called.
func SetRuntimeRunnerForTest(runner RuntimeRunner) func() {
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

// RuntimeEnabled reports whether Functional Clusters prompt guidance is active
// for the current process.
func RuntimeEnabled() bool {
	return envgate.TruthyEnv(EnvEnableFunctionalClusters)
}

// RefreshEnabled reports whether Liza may refresh Functional Clusters for a
// target. Refreshing requires the Functional Clusters gate plus the Stacklit and
// scip-search runtime gates because the build consumes both index families.
func RefreshEnabled(configuredLanguages []string) bool {
	return RuntimeEnabled() &&
		stacklit.RuntimeEnabled() &&
		scipsearch.RuntimeEnabled(configuredLanguages)
}

// RefreshIndex builds functional-clusters.json for one task worktree; the index
// lifecycle hooks own the repo-root artifact. It runs the export sequence in a
// deterministic order:
//  1. stacklit export-architecture
//  2. scip-search graph-export for each available SCIP index
//  3. functional-clusters build
func RefreshIndex(opts RefreshOptions) (RefreshResult, error) {
	if !RefreshEnabled(opts.ConfiguredLanguages) {
		return RefreshResult{}, nil
	}

	targetRoot, err := filepath.Abs(opts.TargetRoot)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("resolve functional-clusters target root: %w", err)
	}
	outputPath := filepath.Join(targetRoot, outputArtifactName)

	stacklitIndexes, err := stacklit.AvailableIndexes(stacklit.RuntimePlanOptions{TargetRoot: targetRoot})
	if err != nil {
		return RefreshResult{}, fmt.Errorf("discover Stacklit prerequisite index: %w", err)
	}
	scipIndexes, err := scipsearch.AvailableIndexes(scipsearch.RuntimePlanOptions{
		TargetRoot:          targetRoot,
		ConfiguredLanguages: opts.ConfiguredLanguages,
	})
	if err != nil {
		return RefreshResult{}, fmt.Errorf("discover SCIP prerequisite indexes: %w", err)
	}
	if len(stacklitIndexes) == 0 || len(scipIndexes) == 0 {
		return RefreshResult{}, nil
	}

	if err := prepareTaskWorktreeFunctionalClustersFile(targetRoot); err != nil {
		return RefreshResult{}, err
	}
	if err := removeArtifact(outputPath); err != nil {
		return RefreshResult{}, err
	}

	runner := opts.Runner
	if runner == nil {
		runner = getDefaultRunner()
	}

	output, err := runBuild(targetRoot, outputPath, stacklitIndexes[0], scipIndexes, runner)
	if err != nil {
		_ = removeArtifact(outputPath)
		return RefreshResult{
			Failures: []RefreshFailure{{Diagnostic: boundedFailureDiagnostic(err, output)}},
		}, nil
	}
	if _, err := os.Stat(outputPath); err != nil {
		_ = removeArtifact(outputPath)
		return RefreshResult{
			Failures: []RefreshFailure{{Diagnostic: boundedFailureDiagnostic(fmt.Errorf("functional-clusters did not write %s: %w", outputPath, err), output)}},
		}, nil
	}
	return RefreshResult{Successes: []IndexRef{{Path: outputPath}}}, nil
}

// AvailableIndexes returns the existing absolute functional-clusters.json path
// for a target root. Missing files are omitted.
func AvailableIndexes(opts RuntimePlanOptions) ([]IndexRef, error) {
	if !RuntimeEnabled() {
		return nil, nil
	}

	targetRoot, err := filepath.Abs(opts.TargetRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve functional-clusters target root: %w", err)
	}
	outputPath := filepath.Join(targetRoot, outputArtifactName)
	info, err := os.Stat(outputPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect functional-clusters artifact %q: %w", outputPath, err)
	}
	if info.IsDir() {
		return nil, nil
	}
	return []IndexRef{{Path: outputPath}}, nil
}

func runBuild(targetRoot, outputPath string, stacklitIndex stacklit.IndexRef, scipIndexes []scipsearch.IndexRef, runner RuntimeRunner) (string, error) {
	tmpDir, err := os.MkdirTemp(targetRoot, "."+brand.RuntimeValues().BinaryName+"-functional-clusters-")
	if err != nil {
		return "", fmt.Errorf("create temporary functional-clusters directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	architecturePath := filepath.Join(tmpDir, stacklitArchitectureArtifactName)
	output, err := runner(stacklitExportArchitecturePlan(targetRoot, stacklitIndex.Path, architecturePath))
	if err != nil {
		return output, err
	}
	if _, err := os.Stat(architecturePath); err != nil {
		return output, fmt.Errorf("stacklit export-architecture did not write %s: %w", architecturePath, err)
	}

	graphPaths := make([]string, 0, len(scipIndexes))
	for _, scipIndex := range scipIndexes {
		graphPath := filepath.Join(tmpDir, scipIndex.Language+"-scip-graph.json")
		output, err = runner(scipGraphExportPlan(targetRoot, scipIndex.Path, graphPath))
		if err != nil {
			return output, err
		}
		if _, err := os.Stat(graphPath); err != nil {
			return output, fmt.Errorf("scip-search graph-export did not write %s: %w", graphPath, err)
		}
		graphPaths = append(graphPaths, graphPath)
	}

	tmpOutputPath := filepath.Join(tmpDir, outputArtifactName)
	output, err = runner(buildPlan(targetRoot, architecturePath, graphPaths, tmpOutputPath))
	if err != nil {
		return output, err
	}
	if _, err := os.Stat(tmpOutputPath); err != nil {
		return output, fmt.Errorf("functional-clusters build did not write %s: %w", tmpOutputPath, err)
	}
	if err := os.Rename(tmpOutputPath, outputPath); err != nil {
		return output, fmt.Errorf("replace functional-clusters artifact %s: %w", outputPath, err)
	}
	return output, nil
}

func stacklitExportArchitecturePlan(targetRoot, stacklitIndexPath, outputPath string) RuntimeCommandPlan {
	return RuntimeCommandPlan{
		Name:       "stacklit",
		Args:       []string{"export-architecture", "-i", stacklitIndexPath, "-o", outputPath},
		Dir:        targetRoot,
		OutputPath: outputPath,
	}
}

func scipGraphExportPlan(targetRoot, scipIndexPath, outputPath string) RuntimeCommandPlan {
	return RuntimeCommandPlan{
		Name:       "scip-search",
		Args:       []string{"graph-export", "--index", scipIndexPath, "-o", outputPath},
		Dir:        targetRoot,
		OutputPath: outputPath,
	}
}

func buildPlan(targetRoot, architecturePath string, graphPaths []string, outputPath string) RuntimeCommandPlan {
	args := []string{"build"}
	for _, graphPath := range graphPaths {
		args = append(args, "--scip-graph", graphPath)
	}
	args = append(args, "--stacklit-architecture", architecturePath, "-o", outputPath)
	return RuntimeCommandPlan{
		Name:       "functional-clusters",
		Args:       args,
		Dir:        targetRoot,
		OutputPath: outputPath,
	}
}

func getDefaultRunner() RuntimeRunner {
	runnerMu.Lock()
	defer runnerMu.Unlock()
	return defaultRunner
}

func runRuntimeCommandPlan(plan RuntimeCommandPlan) (string, error) {
	return subprocess.CombinedOutput(plan.Name, plan.Args, plan.Dir)
}

func prepareTaskWorktreeFunctionalClustersFile(targetRoot string) error {
	taskWorktreeFunctionalClustersGitStateMu.Lock()
	defer taskWorktreeFunctionalClustersGitStateMu.Unlock()

	tracked, err := functionalClustersTracked(targetRoot)
	if err != nil {
		return err
	}
	if tracked {
		output, err := gitenv.CombinedOutput(targetRoot, "update-index", "--skip-worktree", outputArtifactName)
		if err != nil {
			return fmt.Errorf("mark task worktree functional-clusters.json skip-worktree: %w%s", err, outputSuffix(string(output)))
		}
		return nil
	}
	return worktreeexclude.EnsurePrivateExclude(targetRoot, outputArtifactName)
}

func functionalClustersTracked(targetRoot string) (bool, error) {
	output, err := gitenv.CombinedOutput(targetRoot, "ls-files", "--error-unmatch", outputArtifactName)
	if err == nil {
		return true, nil
	}
	if gitUnmatchedPath(err, output) {
		return false, nil
	}
	return false, fmt.Errorf("inspect task worktree functional-clusters.json tracking: %w%s", err, outputSuffix(string(output)))
}

func gitUnmatchedPath(err error, output []byte) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.Contains(string(output), "did not match any file")
}

func removeArtifact(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale functional-clusters artifact %q: %w", path, err)
	}
	return nil
}

func boundedFailureDiagnostic(err error, output string) string {
	diagnostic := strings.TrimSpace(err.Error())
	output = strings.TrimSpace(output)
	if output != "" {
		diagnostic += ": " + output
	}
	if len(diagnostic) <= maxFailureDiagnosticBytes {
		return diagnostic
	}
	return diagnostic[:maxFailureDiagnosticBytes] + "...(truncated)"
}

func outputSuffix(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return ": " + output
}
