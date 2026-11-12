package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/toolchain"
)

// doctorFixture is a machine after `setup --provider claude` and, when
// project is set, `init --claude` in a git repository.
type doctorFixture struct {
	home      string
	globalDir string
	project   string
}

func newDoctorSetup(t *testing.T) doctorFixture {
	t.Helper()
	testhelpers.RequireSymlinkCapability(t)
	home := setupGlobalLiza(t)
	// No network: providers resolve from the embedded catalog and no cache is written.
	t.Setenv(providers.EnvCatalogURL, "://invalid")
	f := doctorFixture{home: home, globalDir: filepath.Join(home, paths.GlobalDirName())}
	if err := SetupCommand(SetupParams{
		TargetDir:   f.globalDir,
		Force:       true,
		AutoConfirm: true,
		ProviderIDs: []string{"claude"},
		HomeDir:     home,
		Stdin:       strings.NewReader(""),
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return f
}

func newDoctorInit(t *testing.T) doctorFixture {
	t.Helper()
	f := newDoctorSetup(t)
	f.project = setupGitRepo(t)
	t.Chdir(f.project)
	if err := InitPairingCommand(InitPairingParams{Agents: []string{"claude"}, Stdin: strings.NewReader(""), AutoConfirm: true}); err != nil {
		t.Fatalf("init: %v", err)
	}
	return f
}

func (f doctorFixture) doctor(t *testing.T, opts DoctorOptions) *jsonout.Report {
	t.Helper()
	opts.HomeDir = f.home
	report := jsonout.NewReport()
	if err := Doctor(opts, report); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	return report
}

func (f doctorFixture) fullOptions() DoctorOptions {
	return DoctorOptions{SkillProviders: []string{"claude"}, ContractProviders: []string{"claude"}, ProjectRoot: f.project, Toolchain: true}
}

// findCheck returns the only check with id whose path is path ("" = any path).
func findCheck(t *testing.T, report *jsonout.Report, id, path string) jsonout.Check {
	t.Helper()
	var found []jsonout.Check
	for _, check := range report.Checks {
		if check.ID == id && (path == "" || check.Path == path) {
			found = append(found, check)
		}
	}
	if len(found) != 1 {
		t.Fatalf("checks %s at %q = %v, want exactly one", id, path, found)
	}
	return found[0]
}

func checkIDs(report *jsonout.Report) []string {
	var ids []string
	for _, check := range report.Checks {
		if !slices.Contains(ids, check.ID) {
			ids = append(ids, check.ID)
		}
	}
	return ids
}

func TestDoctorCleanSetupAndInitEveryCheckOK(t *testing.T) {
	f := newDoctorInit(t)
	report := f.doctor(t, f.fullOptions())
	if notOK := report.NotOK(); len(notOK) != 0 {
		t.Fatalf("clean setup + init has non-ok checks: %v", notOK)
	}
	ids := checkIDs(report)
	for _, want := range []string{
		"doctor.provider-catalog", "doctor.global-file", "doctor.skill-link",
		"doctor.global-contract", "doctor.repo-contract",
		"doctor.activation-state", "doctor.repo-asset",
	} {
		if !slices.Contains(ids, want) {
			t.Fatalf("clean run lacks %s checks; ids = %v", want, ids)
		}
	}
	// No toolchain profile and no index gate: those checks do not run.
	for _, id := range ids {
		if strings.HasPrefix(id, "toolchain.") || id == "doctor.index-hooks" || id == "doctor.toolchain-profile" {
			t.Fatalf("unexpected %s check on a machine without toolchain profile or index gate", id)
		}
	}
}

func TestDoctorPersonalFileAtGlobalContractIsAWarning(t *testing.T) {
	f := newDoctorSetup(t)
	globalPath := filepath.Join(f.home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte("my own instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := f.doctor(t, DoctorOptions{ContractProviders: []string{"claude"}})
	check := findCheck(t, report, "doctor.global-contract", globalPath)
	if check.Status != jsonout.CheckFailed || check.Severity != jsonout.SeverityWarn || check.Provider != "claude" {
		t.Fatalf("personal file check = %+v, want failed warn for claude", check)
	}
	if !strings.Contains(check.Fix, "move "+globalPath+" aside") {
		t.Fatalf("fix = %q, want the move-aside instruction", check.Fix)
	}
	if data, err := os.ReadFile(globalPath); err != nil || string(data) != "my own instructions\n" {
		t.Fatalf("personal file changed: %q, %v", data, err)
	}
}

func TestDoctorGlobalContractStates(t *testing.T) {
	f := newDoctorSetup(t)
	target := globalContractTarget(f.home)
	globalPath := filepath.Join(f.home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		prepare func(t *testing.T)
		status  jsonout.CheckStatus
	}{
		{"absent for a global-first provider", func(t *testing.T) {}, jsonout.CheckMissing},
		{"linked", func(t *testing.T) { mustSymlink(t, target, globalPath) }, jsonout.CheckOK},
		{"symlink elsewhere", func(t *testing.T) { mustSymlink(t, filepath.Join(f.home, "other.md"), globalPath) }, jsonout.CheckFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(globalPath)
			tc.prepare(t)
			check := findCheck(t, f.doctor(t, DoctorOptions{ContractProviders: []string{"claude"}}), "doctor.global-contract", globalPath)
			if check.Status != tc.status {
				t.Fatalf("status = %s, want %s: %+v", check.Status, tc.status, check)
			}
		})
	}
}

func TestDoctorRepoFirstProviderWithoutGlobalLinkIsOK(t *testing.T) {
	f := newDoctorSetup(t)
	repoFirst := placementProvider("repo-first", "AGENTS.md", ".repo-first/AGENTS.md", "", false)
	d := doctorRun{homeDir: f.home, globalDir: f.globalDir, report: jsonout.NewReport()}
	d.checkGlobalContract(repoFirst)
	if check := findCheck(t, d.report, "doctor.global-contract", ""); check.Status != jsonout.CheckOK {
		t.Fatalf("repo-first provider without global link = %+v, want ok", check)
	}
}

func TestDoctorGlobalFileStates(t *testing.T) {
	f := newDoctorSetup(t)
	skillDir := filepath.Join(f.globalDir, "skills", "debugging")
	file := filepath.Join(skillDir, "SKILL.md")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	check := findCheck(t, f.doctor(t, DoctorOptions{}), "doctor.global-file", file)
	if check.Status != jsonout.CheckMissing || check.Severity != jsonout.SeverityWarn {
		t.Fatalf("missing skill file = %+v, want missing warn", check)
	}
	if err := os.Mkdir(file, 0o755); err != nil {
		t.Fatal(err)
	}
	if check := findCheck(t, f.doctor(t, DoctorOptions{}), "doctor.global-file", file); check.Status != jsonout.CheckFailed {
		t.Fatalf("directory instead of a file = %+v, want failed", check)
	}
	pipeline := embedded.PipelineConfigPath(f.globalDir)
	if err := os.Remove(pipeline); err != nil {
		t.Fatal(err)
	}
	if check := findCheck(t, f.doctor(t, DoctorOptions{}), "doctor.global-file", pipeline); check.Status != jsonout.CheckMissing {
		t.Fatalf("missing pipeline.yaml = %+v, want missing", check)
	}
}

func TestDoctorSkillLinkStates(t *testing.T) {
	f := newDoctorSetup(t)
	link := filepath.Join(f.home, ".claude", "skills", "debugging")
	cases := []struct {
		name    string
		prepare func(t *testing.T)
		status  jsonout.CheckStatus
	}{
		{"absent", func(t *testing.T) {}, jsonout.CheckMissing},
		{"symlink elsewhere", func(t *testing.T) { mustSymlink(t, f.home, link) }, jsonout.CheckFailed},
		{"regular file", func(t *testing.T) { writeUserFile(t, link, "mine\n") }, jsonout.CheckFailed},
		{"directory", func(t *testing.T) {
			if err := os.Mkdir(link, 0o755); err != nil {
				t.Fatal(err)
			}
		}, jsonout.CheckFailed},
		{"linked", func(t *testing.T) { mustSymlink(t, filepath.Join(f.globalDir, "skills", "debugging"), link) }, jsonout.CheckOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.RemoveAll(link); err != nil {
				t.Fatal(err)
			}
			tc.prepare(t)
			check := findCheck(t, f.doctor(t, DoctorOptions{}), "doctor.skill-link", link)
			if check.Status != tc.status || check.Provider != "claude" {
				t.Fatalf("check = %+v, want %s for claude", check, tc.status)
			}
		})
	}
}

func TestDoctorNamedProviderWithoutLinksIsChecked(t *testing.T) {
	f := newDoctorSetup(t)
	report := f.doctor(t, DoctorOptions{SkillProviders: []string{"codex"}})
	link := filepath.Join(f.home, ".codex", "skills", "debugging")
	if check := findCheck(t, report, "doctor.skill-link", link); check.Status != jsonout.CheckMissing || check.Provider != "codex" {
		t.Fatalf("named provider never set up = %+v, want missing for codex", check)
	}
}

func TestDoctorUnknownProviderFailsTheRun(t *testing.T) {
	f := newDoctorSetup(t)
	if err := Doctor(DoctorOptions{HomeDir: f.home, ContractProviders: []string{"no-such-provider"}}, jsonout.NewReport()); err == nil {
		t.Fatal("unknown provider did not fail the run")
	}
}

func TestDoctorProviderCatalogCache(t *testing.T) {
	f := newDoctorSetup(t)
	cachePath, _ := providers.CachePaths(f.home)
	if check := findCheck(t, f.doctor(t, DoctorOptions{}), "doctor.provider-catalog", cachePath); check.Status != jsonout.CheckOK {
		t.Fatalf("absent cache = %+v, want ok (embedded catalog)", check)
	}
	writeUserFile(t, cachePath, "providers: [unterminated\n")
	if check := findCheck(t, f.doctor(t, DoctorOptions{}), "doctor.provider-catalog", cachePath); check.Status != jsonout.CheckFailed {
		t.Fatalf("corrupt cache = %+v, want failed", check)
	}
	data, err := os.ReadFile(filepath.Join(repoRootForTest(t), "provider-catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeUserFile(t, cachePath, string(data))
	if check := findCheck(t, f.doctor(t, DoctorOptions{}), "doctor.provider-catalog", cachePath); check.Status != jsonout.CheckOK {
		t.Fatalf("valid cache = %+v, want ok", check)
	}
}

// repoRootForTest returns the repository holding this package.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatal("go.mod not found")
	return ""
}

// recordingRunner is a toolchain runner that knows a fixed set of binaries
// and records every command it is asked to run.
type recordingRunner struct {
	present  map[string]bool
	commands *[]toolchain.Command
}

func (r recordingRunner) LookPath(name string) (string, error) {
	if r.present[name] {
		return "/fake/bin/" + name, nil
	}
	return "", os.ErrNotExist
}

func (r recordingRunner) Run(command toolchain.Command) (toolchain.CommandOutput, error) {
	*r.commands = append(*r.commands, command)
	return toolchain.CommandOutput{Stdout: "ok"}, nil
}

func writeToolchainProfile(t *testing.T, globalDir string, tools ...string) {
	t.Helper()
	quoted := make([]string, 0, len(tools))
	for _, tool := range tools {
		quoted = append(quoted, fmt.Sprintf("%q", tool))
	}
	writeUserFile(t, toolchain.ConfiguredProfilePath(globalDir), fmt.Sprintf(`{"profile":"balanced","selected_tools":[%s]}`, strings.Join(quoted, ",")))
}

func manualToolID(t *testing.T) string {
	t.Helper()
	for _, tool := range toolchain.Catalog() {
		if tool.InstallKind == toolchain.InstallManualOnly {
			return tool.ID
		}
	}
	t.Skip("catalog has no manual-only tool")
	return ""
}

func TestDoctorToolchainChecksTheConfiguredTools(t *testing.T) {
	f := newDoctorSetup(t)
	manual := manualToolID(t)
	writeToolchainProfile(t, f.globalDir, "semble", "jq", manual, "no-such-tool")
	var commands []toolchain.Command
	runner := recordingRunner{present: map[string]bool{"semble": true}, commands: &commands}
	report := f.doctor(t, DoctorOptions{Toolchain: true, ToolchainRunner: runner})

	if check := findCheck(t, report, "toolchain.semble", ""); check.Status != jsonout.CheckOK || !strings.Contains(check.Message, "offline model not checked") {
		t.Fatalf("semble = %+v, want ok without the model probe", check)
	}
	if check := findCheck(t, report, "toolchain.jq", ""); check.Status != jsonout.CheckMissing || check.Severity != jsonout.SeverityWarn {
		t.Fatalf("jq absent from PATH = %+v, want missing warn", check)
	}
	if check := findCheck(t, report, "toolchain.no-such-tool", ""); check.Status != jsonout.CheckFailed {
		t.Fatalf("unknown tool = %+v, want failed", check)
	}
	if slices.Contains(checkIDs(report), "toolchain."+manual) {
		t.Fatalf("manual-only tool %s was checked", manual)
	}
	for _, command := range commands {
		if command.Dir != "" || slices.Contains(command.Args, "search") {
			t.Fatalf("doctor ran the Semble model query: %+v", command)
		}
	}
}

func TestDoctorToolchainProfileStates(t *testing.T) {
	f := newDoctorSetup(t)
	if ids := checkIDs(f.doctor(t, DoctorOptions{Toolchain: true})); slices.ContainsFunc(ids, func(id string) bool {
		return strings.HasPrefix(id, "toolchain.") || id == "doctor.toolchain-profile"
	}) {
		t.Fatalf("unconfigured toolchain produced checks: %v", ids)
	}
	writeUserFile(t, toolchain.ConfiguredProfilePath(f.globalDir), "{not json")
	if check := findCheck(t, f.doctor(t, DoctorOptions{Toolchain: true}), "doctor.toolchain-profile", ""); check.Status != jsonout.CheckFailed {
		t.Fatalf("corrupt profile = %+v, want failed", check)
	}
}

func TestDoctorRepoChecks(t *testing.T) {
	f := newDoctorInit(t)
	statePath, err := repoContractActivationStatePath(f.project)
	if err != nil {
		t.Fatal(err)
	}

	settings := paths.New(f.project).ClaudeSettingsPath()
	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	if check := findCheck(t, f.doctor(t, f.fullOptions()), "doctor.repo-asset", settings); check.Status != jsonout.CheckMissing || check.Provider != "claude" {
		t.Fatalf("missing Claude settings = %+v, want missing for claude", check)
	}

	writeUserFile(t, statePath, "{not json")
	if check := findCheck(t, f.doctor(t, f.fullOptions()), "doctor.activation-state", statePath); check.Status != jsonout.CheckFailed {
		t.Fatalf("untrusted activation state = %+v, want failed", check)
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if check := findCheck(t, f.doctor(t, f.fullOptions()), "doctor.activation-state", statePath); check.Status != jsonout.CheckMissing {
		t.Fatalf("absent activation state = %+v, want missing", check)
	}

	// The contract is active at the global location; removing it leaves none.
	if err := os.Remove(filepath.Join(f.home, ".claude", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if check := findCheck(t, f.doctor(t, f.fullOptions()), "doctor.repo-contract", ""); check.Status != jsonout.CheckMissing || check.Provider != "claude" {
		t.Fatalf("no active contract = %+v, want missing for claude", check)
	}
}

func TestDoctorRepoAssetPathsCoverHookScripts(t *testing.T) {
	root := t.TempDir()
	for name, list := range map[string][]string{
		"codex":   embedded.CodexProjectAssetPaths(root),
		"copilot": embedded.CopilotProjectAssetPaths(root),
	} {
		var scripts int
		for _, path := range list {
			if strings.HasSuffix(path, ".sh") {
				scripts++
			}
		}
		if scripts == 0 || scripts == len(list) {
			t.Fatalf("%s asset paths %v must hold both the hook config and the hook scripts", name, list)
		}
	}
}

func TestDoctorIndexHooksPairing(t *testing.T) {
	f := newDoctorSetup(t)
	f.project = setupGitRepo(t)
	t.Chdir(f.project)
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	if err := InitPairingCommand(InitPairingParams{Agents: []string{"claude"}, Stdin: strings.NewReader(""), AutoConfirm: true}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if check := findCheck(t, f.doctor(t, f.fullOptions()), "doctor.index-hooks", ""); check.Status != jsonout.CheckOK {
		t.Fatalf("installed index hooks = %+v, want ok", check)
	}

	hooksDir, err := pairingindex.ResolveEffectiveHooksDir(f.project)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		t.Fatal(err)
	}
	var managed []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sample") {
			managed = append(managed, filepath.Join(hooksDir, entry.Name()))
		}
	}
	if len(managed) < 3 {
		t.Fatalf("expected the index script, dispatcher and lifecycle hooks in %s, got %v", hooksDir, managed)
	}
	for _, path := range managed {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(path, data, 0o755); err != nil {
					t.Fatal(err)
				}
			})
			check := findCheck(t, f.doctor(t, f.fullOptions()), "doctor.index-hooks", path)
			if check.Status != jsonout.CheckMissing || check.Severity != jsonout.SeverityWarn {
				t.Fatalf("removed %s = %+v, want missing warn naming it", path, check)
			}
		})
	}
}

func TestDoctorIndexHooksAbsentWhenNoIndexGate(t *testing.T) {
	f := newDoctorInit(t)
	if slices.Contains(checkIDs(f.doctor(t, f.fullOptions())), "doctor.index-hooks") {
		t.Fatal("index hooks checked although no index gate is on")
	}
}

func TestDoctorIndexHooksMASWorkspace(t *testing.T) {
	tmpDir := chdirToInitRepoForTest(t)
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	if err := InitCommandWithConfig(InitParams{Description: "Goal with Stacklit hooks", SpecRef: "specs/vision.md"}); err != nil {
		t.Fatalf("MAS init: %v", err)
	}
	home, err := paths.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	report := jsonout.NewReport()
	if err := Doctor(DoctorOptions{HomeDir: home, ProjectRoot: tmpDir}, report); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if check := findCheck(t, report, "doctor.index-hooks", ""); check.Status != jsonout.CheckOK {
		t.Fatalf("MAS index hooks = %+v, want ok", check)
	}
}

func TestDoctorWorkspaceStateMAS(t *testing.T) {
	tmpDir := chdirToInitRepoForTest(t)
	if err := InitCommandWithConfig(InitParams{Description: "Goal", SpecRef: "specs/vision.md"}); err != nil {
		t.Fatalf("MAS init: %v", err)
	}
	home, err := paths.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	statePath := paths.New(tmpDir).StatePath()
	run := func() *jsonout.Report {
		report := jsonout.NewReport()
		if err := Doctor(DoctorOptions{HomeDir: home, ProjectRoot: tmpDir}, report); err != nil {
			t.Fatalf("doctor: %v", err)
		}
		return report
	}
	// The MAS state read is lock-free: doctor still writes nothing.
	before := snapshotTrees(t, []string{tmpDir, home})
	if check := findCheck(t, run(), "doctor.workspace-state", statePath); check.Status != jsonout.CheckOK {
		t.Fatalf("after MAS init = %+v, want ok", check)
	}
	after := snapshotTrees(t, []string{tmpDir, home})
	if len(before) != len(after) {
		t.Fatalf("doctor changed the number of paths: %d → %d", len(before), len(after))
	}
	for path, state := range before {
		if after[path] != state {
			t.Fatalf("doctor changed %s", path)
		}
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	writeUserFile(t, statePath, "tasks: [unterminated\n")
	if check := findCheck(t, run(), "doctor.workspace-state", statePath); check.Status != jsonout.CheckFailed {
		t.Fatalf("unreadable state = %+v, want failed", check)
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	report := run()
	if check := findCheck(t, report, "doctor.workspace-state", statePath); check.Status != jsonout.CheckMissing || check.Severity != jsonout.SeverityWarn {
		t.Fatalf("removed state = %+v, want missing warn", check)
	}
	if !report.Degraded() {
		t.Fatal("a removed workspace state leaves the run clean")
	}
	writeUserFile(t, statePath, string(data))
}

func TestDoctorNoWorkspaceStateCheckForPairingRuntimeDir(t *testing.T) {
	f := newDoctorInit(t)
	// Pairing repositories have a runtime directory too (system prompt mode,
	// for instance), without MAS init's frozen pipeline or models file.
	writeUserFile(t, filepath.Join(paths.New(f.project).LizaDir(), "system-prompt-mode.json"), "{}\n")
	report := f.doctor(t, f.fullOptions())
	if slices.Contains(checkIDs(report), "doctor.workspace-state") {
		t.Fatal("pairing repository got a workspace-state check")
	}
	if notOK := report.NotOK(); len(notOK) != 0 {
		t.Fatalf("pairing repository with a runtime directory: %v", notOK)
	}
}

// TestDoctorWritesNothing runs doctor on a degraded machine with every check
// family active and compares the home directory, the repository with its git
// directory, and TMPDIR before and after. Directory modification times are
// part of the snapshot, so a file created and removed again is caught too.
func TestDoctorWritesNothing(t *testing.T) {
	f := newDoctorSetup(t)
	f.project = setupGitRepo(t)
	t.Chdir(f.project)
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	if err := InitPairingCommand(InitPairingParams{Agents: []string{"claude", "codex"}, Stdin: strings.NewReader(""), AutoConfirm: true}); err != nil {
		t.Fatalf("init: %v", err)
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	writeToolchainProfile(t, f.globalDir, "semble", "jq")
	writeUserFile(t, filepath.Join(f.home, ".claude", "skills", "debugging-copy"), "x\n")
	if err := os.Remove(filepath.Join(f.home, ".claude", "skills", "debugging")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.New(f.project).ClaudeSettingsPath()); err != nil {
		t.Fatal(err)
	}

	roots := []string{f.home, f.project, tmp}
	before := snapshotTrees(t, roots)
	var commands []toolchain.Command
	runner := recordingRunner{present: map[string]bool{"semble": true}, commands: &commands}
	opts := f.fullOptions()
	opts.ContractProviders = []string{"claude", "codex"}
	opts.ToolchainRunner = runner
	report := f.doctor(t, opts)
	if !report.Degraded() {
		t.Fatal("fixture is not degraded; the test would not exercise the failure paths")
	}
	after := snapshotTrees(t, roots)
	for path, state := range before {
		if after[path] != state {
			t.Fatalf("doctor changed %s: %q → %q", path, state, after[path])
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Fatalf("doctor created %s", path)
		}
	}
	for _, command := range commands {
		if command.Dir != "" {
			t.Fatalf("doctor ran a command with a working directory (fixture): %+v", command)
		}
	}
}

// snapshotTrees maps every path under roots to its type, mode, modification
// time and content hash or link target.
func snapshotTrees(t *testing.T, roots []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			state := fmt.Sprintf("%s %d", info.Mode(), info.ModTime().UnixNano())
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				state += " -> " + target
			case info.Mode().IsRegular():
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(data)
				state += " " + hex.EncodeToString(sum[:])
			}
			out[path] = state
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return out
}

func mustSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func writeUserFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
