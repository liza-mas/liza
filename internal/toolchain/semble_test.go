package toolchain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/semble"
)

// offlineModelStderr is huggingface_hub's refusal when Semble's default model
// is not cached and HF_HUB_OFFLINE=1.
const offlineModelStderr = "Cannot find an appropriate cached snapshot folder for the specified revision on the local disk and outgoing traffic has been disabled. To enable repo look-ups and downloads online, set 'HF_HUB_OFFLINE=0' as environment variable."

// sembleRunner scripts a semble install: uv creates the managed binary, and
// each model query answers by its HF_HUB_OFFLINE setting.
type sembleRunner struct {
	paths   map[string]string
	runs    []Command
	creates string
	// offline answers successive HF_HUB_OFFLINE=1 queries; the last repeats.
	offline []CommandOutput
	prewarm CommandOutput
}

func (r *sembleRunner) LookPath(name string) (string, error) {
	if path := r.paths[name]; path != "" {
		return path, nil
	}
	return "", errors.New("missing")
}

func (r *sembleRunner) Run(command Command) (CommandOutput, error) {
	r.runs = append(r.runs, command)
	var output CommandOutput
	switch {
	case command.Name == "uv":
		if r.creates != "" {
			if err := os.WriteFile(r.creates, []byte("#!/bin/sh\n"), 0o755); err != nil {
				return CommandOutput{}, err
			}
		}
	case command.Env["HF_HUB_OFFLINE"] == "1":
		if len(r.offline) > 0 {
			output = r.offline[0]
			if len(r.offline) > 1 {
				r.offline = r.offline[1:]
			}
		}
	case command.Env["HF_HUB_OFFLINE"] == "0":
		output = r.prewarm
	}
	if output.ExitCode != 0 {
		return output, errors.New("exit status 1")
	}
	return output, nil
}

func (r *sembleRunner) modelQueries() []Command {
	var queries []Command
	for _, run := range r.runs {
		if run.Env["HF_HUB_OFFLINE"] != "" {
			queries = append(queries, run)
		}
	}
	return queries
}

var (
	modelReady       = CommandOutput{Stdout: "[]"}
	modelUnavailable = CommandOutput{Stderr: offlineModelStderr, ExitCode: 1}
	queryBroken      = CommandOutput{Stderr: "Traceback: ImportError", ExitCode: 1}
)

func installSemble(t *testing.T, runner *sembleRunner, installDir, goos string) (InstallResult, error) {
	t.Helper()
	return Install(InstallOptions{ToolID: sembleToolID, InstallDir: installDir, Runner: runner, GOOS: goos})
}

func TestSembleFreshInstallPinsAndPrewarmsWithTheManagedExecutable(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			installDir := t.TempDir()
			managed := filepath.Join(installDir, "semble")
			if goos == "windows" {
				managed += ".exe"
			}
			// The install directory is not on PATH: configure has not run.
			runner := &sembleRunner{creates: managed, offline: []CommandOutput{modelReady}, prewarm: modelReady}

			got, err := installSemble(t, runner, installDir, goos)
			if err != nil {
				t.Fatalf("Install() error = %v; steps %+v", err, got.Steps)
			}
			if len(got.Steps) != 1 || got.Steps[0].Status != InstallInstalled || !strings.Contains(got.Steps[0].Message, "model prewarmed") {
				t.Fatalf("steps = %+v, want one installed semble step with a prewarmed model", got.Steps)
			}
			if install := runner.runs[0]; install.Name != "uv" || install.Args[len(install.Args)-1] != "semble==0.6.2" {
				t.Fatalf("install command = %+v, want the pinned uv tool install", install)
			}
			queries := runner.modelQueries()
			if len(queries) != 2 || queries[0].Env["HF_HUB_OFFLINE"] != "0" || queries[1].Env["HF_HUB_OFFLINE"] != "1" {
				t.Fatalf("model queries = %+v, want an online prewarm then an offline check", queries)
			}
			for _, query := range queries {
				if query.Name != managed {
					t.Fatalf("model query ran %q, want the managed executable %q", query.Name, managed)
				}
			}
		})
	}
}

func TestSembleIncompleteModelSetupFailsTheInstall(t *testing.T) {
	for name, runner := range map[string]*sembleRunner{
		"prewarm fails":                {prewarm: queryBroken, offline: []CommandOutput{modelReady}},
		"still uncached after prewarm": {prewarm: modelReady, offline: []CommandOutput{modelUnavailable}},
	} {
		t.Run(name, func(t *testing.T) {
			installDir := t.TempDir()
			runner.creates = filepath.Join(installDir, "semble")
			got, err := installSemble(t, runner, installDir, "linux")
			if err == nil || !strings.Contains(err.Error(), "semble:failed") {
				t.Fatalf("Install() error = %v, want the semble step to fail the install", err)
			}
			step := got.Steps[0]
			if step.Status != InstallFailed || !strings.Contains(step.Message, "semble installed at "+runner.creates) || !strings.Contains(step.Message, semble.ModelRecoveryCommand()) {
				t.Fatalf("step = %+v, want failed naming the installed binary and the retry command", step)
			}
		})
	}
}

func TestSembleExistingInstallRepairsAnUncachedModel(t *testing.T) {
	const existing = "/opt/tools/semble"
	tests := map[string]struct {
		offline     []CommandOutput
		wantStatus  InstallStatus
		wantMessage string
		wantPrewarm bool
	}{
		"model ready":    {offline: []CommandOutput{modelReady}, wantStatus: InstallSkipped, wantMessage: "already installed at " + existing + "; model ready offline"},
		"model uncached": {offline: []CommandOutput{modelUnavailable, modelReady}, wantStatus: InstallInstalled, wantMessage: "model prewarmed", wantPrewarm: true},
		"query broken":   {offline: []CommandOutput{queryBroken}, wantStatus: InstallFailed, wantMessage: "offline query failed"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &sembleRunner{paths: map[string]string{"semble": existing}, offline: tt.offline, prewarm: modelReady}
			got, _ := installSemble(t, runner, t.TempDir(), "linux")
			step := got.Steps[0]
			if step.Status != tt.wantStatus || !strings.Contains(step.Message, tt.wantMessage) {
				t.Fatalf("step = %+v, want %s containing %q", step, tt.wantStatus, tt.wantMessage)
			}
			prewarmed := false
			for _, query := range runner.modelQueries() {
				if query.Name != existing {
					t.Fatalf("model query ran %q, want the installed %q", query.Name, existing)
				}
				prewarmed = prewarmed || query.Env["HF_HUB_OFFLINE"] == "0"
			}
			if prewarmed != tt.wantPrewarm {
				t.Fatalf("prewarmed = %v, want %v (runs %+v)", prewarmed, tt.wantPrewarm, runner.runs)
			}
			for _, run := range runner.runs {
				if run.Name == "uv" {
					t.Fatalf("an existing semble was reinstalled: %+v", run)
				}
			}
		})
	}
}

func TestSembleDryRunRunsNothing(t *testing.T) {
	runner := &sembleRunner{paths: map[string]string{"semble": "/opt/tools/semble"}, offline: []CommandOutput{modelUnavailable}}
	got, err := Install(InstallOptions{ToolID: sembleToolID, InstallDir: t.TempDir(), DryRun: true, Runner: runner, GOOS: "linux"})
	if err != nil || len(got.Steps) != 1 || got.Steps[0].ToolID != sembleToolID {
		t.Fatalf("Install() = %+v, %v, want one semble step", got, err)
	}
	if len(runner.runs) != 0 {
		t.Fatalf("dry run ran %+v", runner.runs)
	}
}

func TestDoctorChecksSembleOffline(t *testing.T) {
	const path = "/opt/tools/semble"
	tests := map[string]struct {
		offline     CommandOutput
		wantStatus  DoctorStatus
		wantMessage string
	}{
		"model ready":    {offline: modelReady, wantStatus: DoctorOK},
		"model uncached": {offline: modelUnavailable, wantStatus: DoctorFailed, wantMessage: "model unavailable offline; prewarm it with " + semble.ModelRecoveryCommand()},
		"query broken":   {offline: queryBroken, wantStatus: DoctorFailed, wantMessage: "offline query failed (exit 1)"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &sembleRunner{paths: map[string]string{"semble": path}, offline: []CommandOutput{tt.offline}}
			got, err := Doctor(DoctorOptions{ToolID: sembleToolID, Runner: runner})
			if err != nil {
				t.Fatal(err)
			}
			check := got.Checks[0]
			if check.Status != tt.wantStatus || !strings.Contains(check.Message, tt.wantMessage) {
				t.Fatalf("check = %+v, want %s containing %q", check, tt.wantStatus, tt.wantMessage)
			}
			queries := runner.modelQueries()
			if len(queries) != 1 || queries[0].Name != path || queries[0].Env["HF_HUB_OFFLINE"] != "1" {
				t.Fatalf("model queries = %+v, want one offline query of %s", queries, path)
			}
		})
	}
}
