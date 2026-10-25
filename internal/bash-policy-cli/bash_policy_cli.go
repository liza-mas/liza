package bashpolicycli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
)

var EnvEnableBashPolicy = brand.EnvName("ENABLE_BASH_POLICY")

const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
	ProviderCursor = "cursor"
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
	AutoConfirm bool
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

	initCommand := Command{
		Path: executable,
		Args: []string{
			"init",
			"--provider", opts.Provider,
			"--policy-artifact-root", opts.ProjectRoot,
		},
		Dir:    opts.ProjectRoot,
		Stdin:  initHooksStdin(opts.Stdin, opts.AutoConfirm),
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	}
	runner := opts.Runner
	if runner == nil {
		runner = realRunner{}
	}
	activationCommand := Command{
		Path: executable,
		Args: []string{
			"activation", "on",
			"--provider", opts.Provider,
			"--policy-artifact-root", opts.ProjectRoot,
		},
		Dir:    opts.ProjectRoot,
		Stdin:  initHooksStdin(opts.Stdin, opts.AutoConfirm),
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	}
	var result InitHooksResult
	for _, command := range []Command{initCommand, activationCommand} {
		output, err := runner.Run(command)
		result = InitHooksResult{
			Status:     StatusInstalled,
			Executable: executable,
			Command:    command,
			Output:     output,
			Err:        err,
		}
		if err != nil {
			result.Status = StatusFailed
			result.Err = fmt.Errorf("bash-policy %s failed: %w", command.Args[0], err)
			return result
		}
	}
	return result
}

// hookConfirmationAnswers is how many "yes" answers auto-confirm supplies to the hook installer.
const hookConfirmationAnswers = 16

func initHooksStdin(stdin io.Reader, autoConfirm bool) io.Reader {
	if !autoConfirm {
		return stdin
	}
	return strings.NewReader(strings.Repeat("yes\n", hookConfirmationAnswers))
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
