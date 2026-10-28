package bashpolicycli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	commands        []Command
	output          CommandOutput
	outputByCommand map[string]CommandOutput
	err             error
	errByCommand    map[string]error
}

func (f *fakeCommandRunner) Run(command Command) (CommandOutput, error) {
	f.commands = append(f.commands, command)
	output := f.output
	if len(command.Args) > 0 {
		if commandOutput, ok := f.outputByCommand[command.Args[0]]; ok {
			output = commandOutput
		}
	}
	if command.Stdout != nil {
		_, _ = command.Stdout.Write([]byte(output.Stdout))
	}
	if command.Stderr != nil {
		_, _ = command.Stderr.Write([]byte(output.Stderr))
	}
	err := f.err
	if len(command.Args) > 0 {
		if commandErr, ok := f.errByCommand[command.Args[0]]; ok {
			err = commandErr
		}
	}
	return output, err
}

func TestInitHooksDisabledSkipsLookup(t *testing.T) {
	t.Setenv(EnvEnableBashPolicy, "")

	var lookups int
	got := InitHooks(InitHooksOptions{
		ProjectRoot: t.TempDir(),
		Provider:    ProviderClaude,
		LookPath: func(name string) (string, error) {
			lookups++
			return "/bin/" + name, nil
		},
		Runner: &fakeCommandRunner{},
	})

	if got.Status != StatusDisabled {
		t.Fatalf("status = %s, want disabled", got.Status)
	}
	if lookups != 0 {
		t.Fatalf("lookups = %d, want zero", lookups)
	}
}

func TestInitHooksMissingExecutable(t *testing.T) {
	t.Setenv(EnvEnableBashPolicy, " true ")

	got := InitHooks(InitHooksOptions{
		ProjectRoot: t.TempDir(),
		Provider:    ProviderClaude,
		LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
		Runner: &fakeCommandRunner{},
	})

	if got.Status != StatusMissing {
		t.Fatalf("status = %s, want missing", got.Status)
	}
}

func TestInitHooksRunsProviderAwareInitThenActivation(t *testing.T) {
	t.Setenv(EnvEnableBashPolicy, "1")
	stdin := strings.NewReader("input\n")
	projectRoot := t.TempDir()
	runner := &fakeCommandRunner{
		output: CommandOutput{Stdout: "installed\n", Stderr: "diagnostic\n"},
	}
	var stdout strings.Builder
	var stderr strings.Builder

	got := InitHooks(InitHooksOptions{
		ProjectRoot: projectRoot,
		Provider:    ProviderClaude,
		Stdin:       stdin,
		Stdout:      &stdout,
		Stderr:      &stderr,
		LookPath: func(name string) (string, error) {
			return "/custom/bin/" + name, nil
		},
		Runner: runner,
	})

	if got.Status != StatusInstalled {
		t.Fatalf("status = %s, want installed", got.Status)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(runner.commands))
	}
	wantArgs := [][]string{
		{"init", "--provider", "claude", "--policy-artifact-root", projectRoot},
		{"activation", "on", "--provider", "claude", "--policy-artifact-root", projectRoot},
	}
	for i, command := range runner.commands {
		if command.Path != "/custom/bin/bash-policy" {
			t.Fatalf("command %d path = %q", i, command.Path)
		}
		if strings.Join(command.Args, "\x00") != strings.Join(wantArgs[i], "\x00") {
			t.Fatalf("command %d args = %v", i, command.Args)
		}
		if command.Dir != projectRoot {
			t.Fatalf("command %d dir = %q, want %q", i, command.Dir, projectRoot)
		}
		if command.Stdin != stdin {
			t.Fatalf("command %d stdin was not preserved", i)
		}
		if command.Stdout != &stdout {
			t.Fatalf("command %d stdout writer was not preserved", i)
		}
		if command.Stderr != &stderr {
			t.Fatalf("command %d stderr writer was not preserved", i)
		}
	}
	if stdout.String() != "installed\ninstalled\n" || stderr.String() != "diagnostic\ndiagnostic\n" {
		t.Fatalf("stdout/stderr = %q/%q", stdout.String(), stderr.String())
	}
}

func TestInitHooksAutoConfirmPassesYesInputToEachSubprocess(t *testing.T) {
	t.Setenv(EnvEnableBashPolicy, "1")
	projectRoot := t.TempDir()
	runner := &fakeCommandRunner{}

	got := InitHooks(InitHooksOptions{
		ProjectRoot: projectRoot,
		Provider:    ProviderClaude,
		Stdin:       strings.NewReader(""),
		LookPath: func(name string) (string, error) {
			return "/custom/bin/" + name, nil
		},
		Runner:      runner,
		AutoConfirm: true,
	})

	if got.Status != StatusInstalled {
		t.Fatalf("status = %s, want installed", got.Status)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(runner.commands))
	}
	for i, command := range runner.commands {
		content, err := io.ReadAll(command.Stdin)
		if err != nil {
			t.Fatalf("read command %d stdin: %v", i, err)
		}
		if string(content) != strings.Repeat("yes\n", 16) {
			t.Fatalf("command %d stdin = %q, want scripted yes input", i, string(content))
		}
	}
}

func TestRealRunnerStreamsAndCapturesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script helper is unix-only")
	}

	scriptPath := filepath.Join(t.TempDir(), "bash-policy-helper")
	script := "#!/bin/sh\nprintf 'visible prompt'\nprintf 'visible diagnostic' >&2\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	output, err := realRunner{}.Run(Command{
		Path:   scriptPath,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("run helper script: %v", err)
	}
	if output.Stdout != "visible prompt" || stdout.String() != "visible prompt" {
		t.Fatalf("stdout captured/streamed = %q/%q", output.Stdout, stdout.String())
	}
	if output.Stderr != "visible diagnostic" || stderr.String() != "visible diagnostic" {
		t.Fatalf("stderr captured/streamed = %q/%q", output.Stderr, stderr.String())
	}
}

func TestInitHooksFailureDiagnosticIncludesCapturedOutput(t *testing.T) {
	t.Setenv(EnvEnableBashPolicy, "true")
	runner := &fakeCommandRunner{
		output: CommandOutput{Stdout: "partial stdout", Stderr: "policy failed"},
		err:    errors.New("exit status 2"),
	}

	got := InitHooks(InitHooksOptions{
		ProjectRoot: t.TempDir(),
		Provider:    ProviderCodex,
		LookPath: func(name string) (string, error) {
			return "/bin/" + name, nil
		},
		Runner: runner,
	})

	if got.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %d, want init failure to skip activation", len(runner.commands))
	}
	diagnostic := got.Diagnostic()
	for _, want := range []string{"exit status 2", "policy failed", "partial stdout"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("diagnostic %q missing %q", diagnostic, want)
		}
	}
}

func TestInitHooksActivationFailureDiagnosticIncludesCapturedOutput(t *testing.T) {
	t.Setenv(EnvEnableBashPolicy, "true")
	runner := &fakeCommandRunner{
		outputByCommand: map[string]CommandOutput{
			"activation": {Stdout: "activation stdout", Stderr: "activation failed"},
		},
		errByCommand: map[string]error{
			"activation": errors.New("exit status 3"),
		},
	}

	got := InitHooks(InitHooksOptions{
		ProjectRoot: t.TempDir(),
		Provider:    ProviderCodex,
		LookPath: func(name string) (string, error) {
			return "/bin/" + name, nil
		},
		Runner: runner,
	})

	if got.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("commands = %d, want init then activation", len(runner.commands))
	}
	diagnostic := got.Diagnostic()
	for _, want := range []string{"bash-policy activation failed", "exit status 3", "activation failed", "activation stdout"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("diagnostic %q missing %q", diagnostic, want)
		}
	}
}

func TestInitHooksStdinAutoConfirmSuppliesSixteenAnswers(t *testing.T) {
	data, err := io.ReadAll(initHooksStdin(strings.NewReader("ignored"), true))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "yes\n"); got != 16 || hookConfirmationAnswers != 16 {
		t.Fatalf("auto-confirm answers = %d (const %d), want 16", got, hookConfirmationAnswers)
	}
}
