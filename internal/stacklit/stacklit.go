package stacklit

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
)

var EnvEnableStacklit = brand.EnvName("ENABLE_STACKLIT")

const maxFailureDiagnosticBytes = 1024

// RuntimeCommandPlan describes the fixed Stacklit generation command.
type RuntimeCommandPlan struct {
	Name       string
	Args       []string
	Dir        string
	OutputPath string
}

// RefreshOptions configures one best-effort task worktree Stacklit refresh.
type RefreshOptions struct {
	// ProjectRoot holds the repo-root stacklit.json the worktree copy comes from.
	ProjectRoot string
	TargetRoot  string
}

// RefreshResult contains the provisioned index and isolated failure diagnostics.
type RefreshResult struct {
	Successes []IndexRef
	Failures  []RefreshFailure
}

// IndexRef identifies one prompt-safe Stacklit index file.
type IndexRef struct {
	Path string
}

// RefreshFailure contains bounded diagnostics for a failed Stacklit refresh.
type RefreshFailure struct {
	Diagnostic string
}

// RuntimePlanOptions configures read-only Stacklit index discovery.
type RuntimePlanOptions struct {
	TargetRoot string
}

// taskWorktreeStacklitGitStateMu serializes skip-worktree updates for
// stacklit.json so concurrent lifecycle hooks do not race on git index state.
var taskWorktreeStacklitGitStateMu sync.Mutex

func parseEnvGate(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// RuntimeEnabled reports whether runtime Stacklit behavior is active for the
// current process.
func RuntimeEnabled() bool {
	return parseEnvGate(envgate.Value(EnvEnableStacklit))
}

// RefreshIndex copies the repo-root stacklit.json into one task worktree; it
// never runs Stacklit. The index lifecycle hooks own the repo-root index, and
// its paths are relative to the project root, so the copy needs no rewrite. It
// lags the worktree by at most one repo-root refresh and excludes the task's
// own edits. The copy is first isolated from task diffs: tracked stacklit.json
// is marked skip-worktree for that linked worktree, while ignored stacklit.json
// stays an ignored prompt-local file. A missing repo-root index is reported as
// a failure and leaves the worktree without one.
func RefreshIndex(opts RefreshOptions) (RefreshResult, error) {
	if !RuntimeEnabled() {
		return RefreshResult{}, nil
	}

	plan, err := PlanRuntimeCommand(opts.TargetRoot)
	if err != nil {
		return RefreshResult{}, err
	}
	source, err := PlanRuntimeCommand(opts.ProjectRoot)
	if err != nil {
		return RefreshResult{}, err
	}

	if err := prepareTaskWorktreeStacklitFile(plan.Dir); err != nil {
		return RefreshResult{}, err
	}
	if err := removeTaskWorktreeIndex(plan.OutputPath); err != nil {
		return RefreshResult{}, err
	}

	if _, err := os.Stat(source.OutputPath); errors.Is(err, os.ErrNotExist) {
		return RefreshResult{
			Failures: []RefreshFailure{{Diagnostic: fmt.Sprintf("repo-root index %s not found; the worktree has no Stacklit index until the repo-root index exists", source.OutputPath)}},
		}, nil
	}
	if err := atomicfile.Copy(source.OutputPath, plan.OutputPath); err != nil {
		return RefreshResult{
			Failures: []RefreshFailure{{Diagnostic: boundedFailureDiagnostic(err, "")}},
		}, nil
	}
	return RefreshResult{Successes: []IndexRef{{Path: plan.OutputPath}}}, nil
}

// PlanRuntimeCommand returns the fixed Stacklit generation command without
// executing it.
func PlanRuntimeCommand(targetRoot string) (RuntimeCommandPlan, error) {
	targetRoot, err := filepath.Abs(targetRoot)
	if err != nil {
		return RuntimeCommandPlan{}, fmt.Errorf("resolve stacklit target root: %w", err)
	}
	return RuntimeCommandPlan{
		Name:       "stacklit",
		Args:       []string{"generate-json", "-o", "stacklit.json"},
		Dir:        targetRoot,
		OutputPath: filepath.Join(targetRoot, "stacklit.json"),
	}, nil
}

// AvailableIndexes returns the existing absolute stacklit.json path for a target
// root. Missing files are omitted.
func AvailableIndexes(opts RuntimePlanOptions) ([]IndexRef, error) {
	if !RuntimeEnabled() {
		return nil, nil
	}

	plan, err := PlanRuntimeCommand(opts.TargetRoot)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(plan.OutputPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect stacklit index %q: %w", plan.OutputPath, err)
	}
	if info.IsDir() {
		return nil, nil
	}
	return []IndexRef{{Path: plan.OutputPath}}, nil
}

func prepareTaskWorktreeStacklitFile(targetRoot string) error {
	taskWorktreeStacklitGitStateMu.Lock()
	defer taskWorktreeStacklitGitStateMu.Unlock()

	tracked, err := stacklitJSONTracked(targetRoot)
	if err != nil {
		return err
	}
	if tracked {
		output, err := gitenv.CombinedOutput(targetRoot, "update-index", "--skip-worktree", "stacklit.json")
		if err != nil {
			return fmt.Errorf("mark task worktree stacklit.json skip-worktree: %w%s", err, outputSuffix(string(output)))
		}
		return nil
	}
	ignored, err := stacklitJSONIgnored(targetRoot)
	if err != nil {
		return err
	}
	if ignored {
		return nil
	}
	return fmt.Errorf("task worktree stacklit.json is neither tracked nor ignored; commit or ignore stacklit.json before enabling %s", EnvEnableStacklit)
}

func stacklitJSONTracked(targetRoot string) (bool, error) {
	output, err := gitenv.CombinedOutput(targetRoot, "ls-files", "--error-unmatch", "stacklit.json")
	if err == nil {
		return true, nil
	}
	if gitUnmatchedPath(err, output) {
		return false, nil
	}
	return false, fmt.Errorf("inspect task worktree stacklit.json tracking: %w%s", err, outputSuffix(string(output)))
}

func stacklitJSONIgnored(targetRoot string) (bool, error) {
	output, err := gitenv.CombinedOutput(targetRoot, "check-ignore", "stacklit.json")
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("inspect task worktree stacklit.json ignore state: %w%s", err, outputSuffix(string(output)))
}

func gitUnmatchedPath(err error, output []byte) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.Contains(string(output), "did not match any file")
}

func removeTaskWorktreeIndex(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale stacklit index %q: %w", path, err)
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
