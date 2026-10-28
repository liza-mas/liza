package semble

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/subprocess"
)

var EnvEnableSemble = brand.EnvName("ENABLE_SEMBLE")

const sembleExecutableName = "semble"

// SembleValidationTimeout is the default timeout for prewarm and offline
// validation command execution.
const SembleValidationTimeout = 30 * time.Second

// ExecutableLookup resolves an executable name to the path that would be run.
type ExecutableLookup func(name string) (string, error)

// CommandPlanOptions configures Semble command planning for one validation
// fixture directory.
type CommandPlanOptions struct {
	FixtureDir string
	LookPath   ExecutableLookup
	Timeout    time.Duration
	Fixture    FixtureIdentity
}

// PlanResult contains activation state, executable state, fixed command plans,
// and bounded diagnostics from command planning.
type PlanResult struct {
	Enabled           bool
	ExecutablePath    string
	Prewarm           CommandPlan
	OfflineValidation CommandPlan
	Diagnostics       []Diagnostic
}

// CommandPlan describes one fixed Semble validation command without executing
// it.
type CommandPlan struct {
	Enabled        bool
	Name           string
	ExecutablePath string
	Args           []string
	Dir            string
	Env            []EnvVar
	Timeout        time.Duration
	Fixture        FixtureIdentity
}

// EnvVar is one environment override added to an otherwise inherited process
// environment.
type EnvVar struct {
	Name  string
	Value string
}

// CommandRunner executes one fixed Semble validation command plan.
type CommandRunner func(CommandPlan) (CommandResult, error)

// CommandResult captures bounded subprocess facts used for Semble diagnostics.
type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// RuntimeEnabled reports whether Semble behavior is active for this process.
func RuntimeEnabled() bool {
	return envgate.Truthy(envgate.Value(EnvEnableSemble))
}

// PlanCommands returns fixed Semble prewarm and offline validation plans. When
// disabled, it returns no command plans, diagnostics, or executable lookup.
func PlanCommands(opts CommandPlanOptions) PlanResult {
	if !RuntimeEnabled() {
		return PlanResult{}
	}

	lookup := opts.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	executablePath, err := lookup(sembleExecutableName)
	if err != nil {
		return PlanResult{
			Enabled: true,
			Diagnostics: []Diagnostic{{
				Kind:    DiagnosticMissingExecutable,
				Message: boundedDiagnosticMessage("semble executable not found", err.Error()),
			}},
		}
	}

	fixtureDir := opts.FixtureDir
	if abs, err := filepath.Abs(fixtureDir); err == nil {
		fixtureDir = abs
	}
	fixture := opts.Fixture
	if fixture == (FixtureIdentity{}) {
		fixture = DefaultValidationFixtureIdentity()
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = SembleValidationTimeout
	}
	topK := strconv.Itoa(fixture.TopK)

	base := CommandPlan{
		Enabled:        true,
		Name:           sembleExecutableName,
		ExecutablePath: executablePath,
		Args:           []string{"search", fixture.Query, fixtureDir, "--top-k", topK, "--content", fixture.ContentMode},
		Dir:            fixtureDir,
		Timeout:        timeout,
		Fixture:        fixture,
	}
	offline := base
	offline.Env = []EnvVar{{Name: "HF_HUB_OFFLINE", Value: "1"}}

	return PlanResult{
		Enabled:           true,
		ExecutablePath:    executablePath,
		Prewarm:           base,
		OfflineValidation: offline,
	}
}

// StartClearOrphans launches a detached `semble clear orphans`, which drops
// cached indexes whose source path no longer exists, such as deleted task
// worktrees, and returns without waiting. Semble's cache is per user, so this
// also clears orphans outside the current project. It does nothing when Semble
// is disabled or not installed.
func StartClearOrphans(lookPath ExecutableLookup) error {
	if !RuntimeEnabled() {
		return nil
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	executablePath, err := lookPath(sembleExecutableName)
	if err != nil {
		return nil
	}
	// Nil standard streams use the null device. The caller's working directory
	// may be the worktree just removed.
	cmd := exec.Command(executablePath, "clear", "orphans")
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "HF_HUB_OFFLINE=1")
	subprocess.SetDetachedProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start semble clear orphans: %w", err)
	}
	// A long-lived supervisor must reap the child; a CLI that exits first
	// leaves it to be adopted by init.
	go func() { _ = cmd.Wait() }()
	return nil
}

func runCommandPlan(plan CommandPlan) (CommandResult, error) {
	return runCommandPlanWithFiles(plan, nil)
}

func runCommandPlanWithFiles(plan CommandPlan, files []*os.File) (CommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), plan.Timeout)
	defer cancel()

	name := plan.ExecutablePath
	if name == "" {
		name = plan.Name
	}
	cmd := exec.CommandContext(ctx, name, plan.Args...)
	subprocess.ConfigureCancellation(cmd)
	cmd.Dir = plan.Dir
	cmd.Env = append(os.Environ(), envVars(plan.Env)...)
	cmd.ExtraFiles = files
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	}
	if err == nil {
		result.ExitCode = 0
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return result, err
}

func envVars(vars []EnvVar) []string {
	values := make([]string, 0, len(vars))
	for _, env := range vars {
		values = append(values, env.Name+"="+env.Value)
	}
	return values
}
