package stacklit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/gitenv"
)

const EnvEnableStacklit = "LIZA_ENABLE_STACKLIT"

const maxFailureDiagnosticBytes = 1024

// TargetKind identifies the lifecycle target being refreshed.
type TargetKind string

const (
	TargetKindProjectRoot  TargetKind = "project-root"
	TargetKindTaskWorktree TargetKind = "task-worktree"
)

// RuntimeCommandPlan describes the fixed Stacklit generation command.
type RuntimeCommandPlan struct {
	Name       string
	Args       []string
	Dir        string
	OutputPath string
}

// RuntimeRunner executes one Stacklit runtime command plan.
type RuntimeRunner func(RuntimeCommandPlan) (string, error)

// RefreshOptions configures one best-effort runtime Stacklit refresh.
type RefreshOptions struct {
	TargetRoot string
	TargetKind TargetKind
	Runner     RuntimeRunner
}

// RefreshResult contains the generated index and isolated failure diagnostics.
type RefreshResult struct {
	Successes []IndexRef
	Failures  []RefreshFailure
}

// IndexRef identifies one prompt-safe generated Stacklit index file.
type IndexRef struct {
	Path string
}

// RefreshFailure contains bounded diagnostics for a failed Stacklit generation.
type RefreshFailure struct {
	Diagnostic string
}

// RuntimePlanOptions configures read-only Stacklit index discovery.
type RuntimePlanOptions struct {
	TargetRoot string
}

var (
	runnerMu sync.Mutex
	// taskWorktreeStacklitGitStateMu serializes skip-worktree updates for
	// stacklit.json so concurrent lifecycle hooks do not race on git index state.
	taskWorktreeStacklitGitStateMu sync.Mutex
	defaultRunner                  RuntimeRunner = runRuntimeCommandPlan
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

// RefreshIndex generates stacklit.json for one target root. For task worktrees,
// it first isolates the generated file from task diffs: tracked stacklit.json is
// marked skip-worktree for that linked worktree, while ignored stacklit.json is
// generated as an ignored prompt-local file. Task-local generation requires one
// of those git states so generated snapshots cannot dirty task diffs.
func RefreshIndex(opts RefreshOptions) (RefreshResult, error) {
	if !RuntimeEnabled() {
		return RefreshResult{}, nil
	}

	plan, err := PlanRuntimeCommand(opts.TargetRoot)
	if err != nil {
		return RefreshResult{}, err
	}

	if opts.TargetKind == TargetKindTaskWorktree {
		if err := prepareTaskWorktreeStacklitFile(plan.Dir); err != nil {
			return RefreshResult{}, err
		}
		if err := removeTaskWorktreeIndex(plan.OutputPath); err != nil {
			return RefreshResult{}, err
		}
	}

	runner := opts.Runner
	if runner == nil {
		runner = getDefaultRunner()
	}

	output, err := runner(plan)
	if err != nil {
		if opts.TargetKind == TargetKindTaskWorktree {
			_ = removeTaskWorktreeIndex(plan.OutputPath)
		}
		return RefreshResult{
			Failures: []RefreshFailure{{Diagnostic: boundedFailureDiagnostic(err, output)}},
		}, nil
	}
	if _, err := os.Stat(plan.OutputPath); err != nil {
		return RefreshResult{
			Failures: []RefreshFailure{{Diagnostic: boundedFailureDiagnostic(fmt.Errorf("stacklit did not write %s: %w", plan.OutputPath, err), output)}},
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

func getDefaultRunner() RuntimeRunner {
	runnerMu.Lock()
	defer runnerMu.Unlock()
	return defaultRunner
}

func runRuntimeCommandPlan(plan RuntimeCommandPlan) (string, error) {
	cmd := exec.Command(plan.Name, plan.Args...)
	cmd.Dir = plan.Dir
	output, err := cmd.CombinedOutput()
	return string(output), err
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
