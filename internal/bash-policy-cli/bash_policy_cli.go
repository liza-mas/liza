package bashpolicycli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/liza-mas/liza/internal/envgate"
)

const (
	EnvEnableBashPolicy = "LIZA_ENABLE_BASH_POLICY"

	ProviderClaude = "claude"
	ProviderCodex  = "codex"
	ProviderAll    = "all"
)

type Status string

const (
	StatusDisabled  Status = "disabled"
	StatusNoTarget  Status = "no-target"
	StatusMissing   Status = "missing"
	StatusInstalled Status = "installed"
	StatusFailed    Status = "failed"
)

type ExecutableLookup func(string) (string, error)

type Command struct {
	Path   string
	Args   []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type CommandOutput struct {
	Stdout string
	Stderr string
}

type CommandRunner interface {
	Run(Command) (CommandOutput, error)
}

type InitHooksOptions struct {
	ProjectRoot string
	Provider    string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	LookPath    ExecutableLookup
	Runner      CommandRunner
}

type InitHooksResult struct {
	Status     Status
	Executable string
	Command    Command
	Output     CommandOutput
	Err        error
}

func RuntimeEnabled() bool {
	return envgate.TruthyEnv(EnvEnableBashPolicy)
}

func InitHooks(opts InitHooksOptions) InitHooksResult {
	if !RuntimeEnabled() {
		return InitHooksResult{Status: StatusDisabled}
	}
	if opts.ProjectRoot == "" || opts.Provider == "" {
		return InitHooksResult{Status: StatusNoTarget}
	}

	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	executable, err := lookPath("bash-policy")
	if err != nil || executable == "" {
		return InitHooksResult{Status: StatusMissing, Err: err}
	}

	command := Command{
		Path: executable,
		Args: []string{
			"init",
			"--provider", opts.Provider,
			"--policy-artifact-root", opts.ProjectRoot,
		},
		Dir:    opts.ProjectRoot,
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	}
	runner := opts.Runner
	if runner == nil {
		runner = realRunner{}
	}
	output, err := runner.Run(command)
	result := InitHooksResult{
		Status:     StatusInstalled,
		Executable: executable,
		Command:    command,
		Output:     output,
		Err:        err,
	}
	if err != nil {
		result.Status = StatusFailed
		return result
	}
	return result
}

func (r InitHooksResult) Diagnostic() string {
	var parts []string
	if r.Err != nil {
		parts = append(parts, r.Err.Error())
	}
	if trimmed := strings.TrimSpace(r.Output.Stderr); trimmed != "" {
		parts = append(parts, "stderr: "+trimDiagnostic(trimmed))
	}
	if trimmed := strings.TrimSpace(r.Output.Stdout); trimmed != "" {
		parts = append(parts, "stdout: "+trimDiagnostic(trimmed))
	}
	if len(parts) == 0 {
		return "unknown failure"
	}
	return strings.Join(parts, "; ")
}

type realRunner struct{}

func (realRunner) Run(command Command) (CommandOutput, error) {
	if command.Path == "" {
		return CommandOutput{}, errors.New("missing bash-policy executable path")
	}
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Dir = command.Dir
	cmd.Stdin = command.Stdin
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = captureAndStream(&stdout, command.Stdout)
	cmd.Stderr = captureAndStream(&stderr, command.Stderr)
	err := cmd.Run()
	return CommandOutput{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

func captureAndStream(capture io.Writer, stream io.Writer) io.Writer {
	if stream == nil {
		return capture
	}
	return io.MultiWriter(capture, stream)
}

func trimDiagnostic(value string) string {
	const limit = 1200
	if len(value) <= limit {
		return value
	}
	return fmt.Sprintf("%s... [truncated %d bytes]", value[:limit], len(value)-limit)
}
