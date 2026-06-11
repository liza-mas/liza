package toolchain

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeRunner struct {
	paths             map[string]string
	runs              []Command
	failInstallScript bool
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if f.paths == nil {
		return "", errors.New("missing")
	}
	if path := f.paths[name]; path != "" {
		return path, nil
	}
	return "", errors.New("missing")
}

func (f *fakeRunner) Run(command Command) (CommandOutput, error) {
	f.runs = append(f.runs, command)
	if f.failInstallScript && command.Env["LIZA_TOOL_INSTALL_URL"] != "" {
		return CommandOutput{Stderr: "release metadata unavailable"}, errors.New("exit status 1")
	}
	return CommandOutput{Stdout: "ok"}, nil
}

func TestInstallSkipsAlreadyInstalledTools(t *testing.T) {
	runner := &fakeRunner{paths: map[string]string{"rtk": "/bin/rtk"}}

	got, err := Install(InstallOptions{
		Profile:    ProfileLean,
		Include:    []string{"rtk"},
		Exclude:    []string{"stacklit", "scip-search", "scip-go", "scip-typescript", "scip-python", "rg", "ast-grep", "mdtoc", "mdq", "jq", "yq", "gh", "pre-commit"},
		InstallDir: t.TempDir(),
		Runner:     runner,
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(got.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(got.Steps))
	}
	if got.Steps[0].Status != InstallSkipped || !strings.Contains(got.Steps[0].Message, "/bin/rtk") {
		t.Fatalf("rtk step = %+v, want installed skip", got.Steps[0])
	}
	if len(runner.runs) != 0 {
		t.Fatalf("runner executed commands for installed tool: %+v", runner.runs)
	}
}

func TestInstallDryRunPlansCommandsWithoutRunning(t *testing.T) {
	runner := &fakeRunner{paths: map[string]string{"brew": "/opt/homebrew/bin/brew"}}

	got, err := Install(InstallOptions{
		Profile:    ProfileBalanced,
		Include:    []string{"functional-clusters"},
		Exclude:    allToolIDsExcept("stacklit", "rg", "functional-clusters"),
		InstallDir: t.TempDir(),
		DryRun:     true,
		Runner:     runner,
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(runner.runs) != 0 {
		t.Fatalf("dry run executed commands: %+v", runner.runs)
	}
	stepsByID := map[string]InstallStep{}
	for _, step := range got.Steps {
		stepsByID[step.ToolID] = step
	}
	if stepsByID["stacklit"].Status != InstallPlanned || stepsByID["stacklit"].Command.Name != "bash" {
		t.Fatalf("stacklit dry-run step = %+v", stepsByID["stacklit"])
	}
	if stepsByID["rg"].Command.Name != "brew" {
		t.Fatalf("rg should use brew package command, got %+v", stepsByID["rg"].Command)
	}
	if stepsByID["functional-clusters"].Status != InstallPlanned {
		t.Fatalf("functional-clusters dry-run step = %+v", stepsByID["functional-clusters"])
	}
}

func TestInstallManualToolIsSkipped(t *testing.T) {
	got, err := Install(InstallOptions{
		Profile:    ProfileBalanced,
		Include:    []string{"postgres-mcp"},
		Exclude:    allToolIDsExcept("postgres-mcp"),
		InstallDir: t.TempDir(),
		Runner:     &fakeRunner{},
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got.Steps[0].Status != InstallSkipped {
		t.Fatalf("manual MCP status = %s, want skipped", got.Steps[0].Status)
	}
	if !strings.Contains(got.Steps[0].Message, "provider") {
		t.Fatalf("manual MCP message = %q, want provider guidance", got.Steps[0].Message)
	}
}

func TestInstallNativeWindowsIsUnsupported(t *testing.T) {
	got, err := Install(InstallOptions{
		Profile:    ProfileLean,
		Include:    []string{"rtk"},
		Exclude:    allToolIDsExcept("rtk"),
		InstallDir: t.TempDir(),
		Runner:     &fakeRunner{},
		GOOS:       "windows",
	})
	if err == nil {
		t.Fatal("Install() error = nil, want unsupported platform error")
	}
	if got.Steps[0].Status != InstallUnsupported {
		t.Fatalf("status = %s, want unsupported", got.Steps[0].Status)
	}
	if strings.Contains(got.Steps[0].Message, "doctor-only") {
		t.Fatalf("message = %q, should not claim Windows is doctor-only", got.Steps[0].Message)
	}
}

func TestPackageInstallCommandRequiresKnownPackageManager(t *testing.T) {
	_, err := packageInstallCommand("jq", &fakeRunner{})
	if err == nil {
		t.Fatal("packageInstallCommand() error = nil, want missing package manager error")
	}
	if !strings.Contains(err.Error(), "no supported package manager") {
		t.Fatalf("error = %v", err)
	}
}

func TestPackageInstallCommandRejectsURLPackage(t *testing.T) {
	_, err := packageInstallCommand("https://example.test/tool.rb", &fakeRunner{})
	if err == nil {
		t.Fatal("packageInstallCommand() error = nil, want URL package rejection")
	}
	if !strings.Contains(err.Error(), "URL package installs are not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallFallsBackToGoSourceBuildWhenScriptFails(t *testing.T) {
	runner := &fakeRunner{paths: map[string]string{}, failInstallScript: true}
	got, err := Install(InstallOptions{
		Profile:    ProfileBalanced,
		Include:    []string{"mdtoc"},
		Exclude:    allToolIDsExcept("mdtoc"),
		InstallDir: t.TempDir(),
		Runner:     runner,
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got.Steps[0].Status != InstallInstalled {
		t.Fatalf("status = %s, want installed via fallback: %+v", got.Steps[0].Status, got.Steps[0])
	}
	if !strings.Contains(got.Steps[0].Message, "source") {
		t.Fatalf("message = %q, want source fallback note", got.Steps[0].Message)
	}
	if len(runner.runs) != 2 {
		t.Fatalf("runs = %d, want primary script plus fallback", len(runner.runs))
	}
	fallback := runner.runs[1]
	if fallback.Env["LIZA_TOOL_SOURCE_REPO"] != "https://github.com/liza-mas/mdtoc" {
		t.Fatalf("fallback source repo = %q", fallback.Env["LIZA_TOOL_SOURCE_REPO"])
	}
	if fallback.Env["LIZA_TOOL_SOURCE_PACKAGE"] != "./cmd/mdtoc" {
		t.Fatalf("fallback package = %q", fallback.Env["LIZA_TOOL_SOURCE_PACKAGE"])
	}
	if !strings.Contains(strings.Join(fallback.Args, " "), "git clone") || !strings.Contains(strings.Join(fallback.Args, " "), "go install") {
		t.Fatalf("fallback command does not build from source: %+v", fallback)
	}
}

func TestScipSearchCatalogDeclaresSourceFallback(t *testing.T) {
	selection, err := ResolveSelection(ProfileLean, []string{"scip-search"}, allToolIDsExcept("scip-search"))
	if err != nil {
		t.Fatalf("ResolveSelection() error = %v", err)
	}
	tool := selection.Tools[0]
	if tool.SourceRepo != "https://github.com/liza-mas/scip-search" || tool.SourcePackage != "./cmd/scip-search" {
		t.Fatalf("scip-search fallback = repo %q package %q", tool.SourceRepo, tool.SourcePackage)
	}
}

func allToolIDsExcept(keep ...string) []string {
	var ids []string
	for _, tool := range Catalog() {
		if slices.Contains(keep, tool.ID) {
			continue
		}
		ids = append(ids, tool.ID)
	}
	return ids
}

func TestInstallRunsCommandWhenNotDryRun(t *testing.T) {
	runner := &fakeRunner{paths: map[string]string{}}
	installDir := t.TempDir()
	got, err := Install(InstallOptions{
		Profile:    ProfileBalanced,
		Include:    []string{"rtk"},
		Exclude:    allToolIDsExcept("rtk"),
		InstallDir: installDir,
		Runner:     runner,
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got.Steps[0].Status != InstallInstalled {
		t.Fatalf("status = %s, want installed: %+v", got.Steps[0].Status, got.Steps[0])
	}
	if len(runner.runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runner.runs))
	}
	if runner.runs[0].Env["RTK_INSTALL_DIR"] != installDir {
		t.Fatalf("RTK_INSTALL_DIR = %q, want %q", runner.runs[0].Env["RTK_INSTALL_DIR"], installDir)
	}
	if runner.runs[0].Env["INSTALL_DIR"] != "" {
		t.Fatalf("INSTALL_DIR = %q, want empty for rtk-specific installer", runner.runs[0].Env["INSTALL_DIR"])
	}
}

func TestInstallReturnsErrorWhenAnyStepFails(t *testing.T) {
	got, err := Install(InstallOptions{
		Profile:    ProfileBalanced,
		Include:    []string{"jq"},
		Exclude:    allToolIDsExcept("jq"),
		InstallDir: t.TempDir(),
		Runner:     &fakeRunner{},
	})
	if err == nil {
		t.Fatal("Install() error = nil, want failed step error")
	}
	if !strings.Contains(err.Error(), "jq:failed") {
		t.Fatalf("error = %v, want failed tool id", err)
	}
	if got.Steps[0].Status != InstallFailed {
		t.Fatalf("status = %s, want failed", got.Steps[0].Status)
	}
}

func TestInstallNPMUsesPrefixForInstallDirBin(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "bin")
	got, err := installCommand(Tool{ID: "npm-tool", InstallKind: InstallNPM, NPMPackage: "example"}, installDir, &fakeRunner{})
	if err != nil {
		t.Fatalf("installCommand() error = %v", err)
	}
	if got.Env["NPM_CONFIG_PREFIX"] != filepath.Dir(installDir) {
		t.Fatalf("NPM_CONFIG_PREFIX = %q, want %q", got.Env["NPM_CONFIG_PREFIX"], filepath.Dir(installDir))
	}
}

func TestInstallNPMRejectsInstallDirOutsideBin(t *testing.T) {
	_, err := installCommand(Tool{ID: "npm-tool", InstallKind: InstallNPM, NPMPackage: "example"}, t.TempDir(), &fakeRunner{})
	if err == nil {
		t.Fatal("installCommand() error = nil, want npm bin-dir validation")
	}
	if !strings.Contains(err.Error(), "ending in /bin") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallUVUsesToolBinDir(t *testing.T) {
	installDir := t.TempDir()
	got, err := installCommand(Tool{ID: "uv-tool", InstallKind: InstallUVTool, UVPackage: "example"}, installDir, &fakeRunner{})
	if err != nil {
		t.Fatalf("installCommand() error = %v", err)
	}
	if got.Env["UV_TOOL_BIN_DIR"] != installDir {
		t.Fatalf("UV_TOOL_BIN_DIR = %q, want %q", got.Env["UV_TOOL_BIN_DIR"], installDir)
	}
}
