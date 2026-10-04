// Package functionalclusters owns the optional Functional Clusters runtime
// indexing contract.
//
// Functional Clusters is downstream of Stacklit and scip-search: the index
// lifecycle hooks build the repo-root functional-clusters.json from their
// exports, and refresh callers copy it into task worktrees when all three gates
// are enabled. Prompt callers expose only explicit existing artifact paths when
// the Functional Clusters gate is enabled.
package functionalclusters

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/atomicfile"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/worktreeexclude"
)

var EnvEnableFunctionalClusters = brand.EnvName("ENABLE_FUNCTIONAL_CLUSTERS")

const outputArtifactName = "functional-clusters.json"
const maxFailureDiagnosticBytes = 1024

// RefreshOptions configures one best-effort task worktree Functional Clusters
// refresh.
type RefreshOptions struct {
	// ProjectRoot holds the repo-root functional-clusters.json the worktree copy
	// comes from.
	ProjectRoot         string
	TargetRoot          string
	ConfiguredLanguages []string
}

// RefreshResult contains the provisioned artifact and isolated failure diagnostics.
type RefreshResult struct {
	Successes []IndexRef
	Failures  []RefreshFailure
}

// IndexRef identifies one prompt-safe Functional Clusters artifact.
type IndexRef struct {
	Path string
}

// RefreshFailure contains bounded diagnostics for a failed Functional Clusters refresh.
type RefreshFailure struct {
	Diagnostic string
}

// RuntimePlanOptions configures read-only Functional Clusters artifact discovery.
type RuntimePlanOptions struct {
	TargetRoot string
}

// taskWorktreeFunctionalClustersGitStateMu serializes skip-worktree and
// worktree-private exclude updates for functional-clusters.json.
var taskWorktreeFunctionalClustersGitStateMu sync.Mutex

// RuntimeEnabled reports whether Functional Clusters prompt guidance is active
// for the current process.
func RuntimeEnabled() bool {
	return envgate.TruthyEnv(EnvEnableFunctionalClusters)
}

// RefreshEnabled reports whether Liza may refresh Functional Clusters for a
// target. Refreshing requires the Functional Clusters gate plus the Stacklit and
// scip-search runtime gates because the artifact is built from both index
// families.
func RefreshEnabled(configuredLanguages []string) bool {
	return RuntimeEnabled() &&
		stacklit.RuntimeEnabled() &&
		scipsearch.RuntimeEnabled(configuredLanguages)
}

// RefreshIndex copies the repo-root functional-clusters.json into one task
// worktree; it never runs Functional Clusters, Stacklit or scip-search. The
// index lifecycle hooks own the repo-root artifact, which holds no absolute
// paths, so the copy needs no rewrite. It lags the worktree by at most one
// repo-root refresh and excludes the task's own edits. A missing repo-root
// artifact is reported as a failure and leaves the worktree without one.
func RefreshIndex(opts RefreshOptions) (RefreshResult, error) {
	if !RefreshEnabled(opts.ConfiguredLanguages) {
		return RefreshResult{}, nil
	}

	targetRoot, err := filepath.Abs(opts.TargetRoot)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("resolve functional-clusters target root: %w", err)
	}
	projectRoot, err := filepath.Abs(opts.ProjectRoot)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("resolve functional-clusters project root: %w", err)
	}
	outputPath := filepath.Join(targetRoot, outputArtifactName)
	sourcePath := filepath.Join(projectRoot, outputArtifactName)

	if err := prepareTaskWorktreeFunctionalClustersFile(targetRoot); err != nil {
		return RefreshResult{}, err
	}
	if err := removeArtifact(outputPath); err != nil {
		return RefreshResult{}, err
	}

	if _, err := os.Stat(sourcePath); errors.Is(err, os.ErrNotExist) {
		return RefreshResult{
			Failures: []RefreshFailure{{Diagnostic: fmt.Sprintf("repo-root artifact %s not found; the worktree has no Functional Clusters artifact until the repo-root artifact exists", sourcePath)}},
		}, nil
	}
	if err := atomicfile.Copy(sourcePath, outputPath); err != nil {
		return RefreshResult{
			Failures: []RefreshFailure{{Diagnostic: boundedFailureDiagnostic(err, "")}},
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
