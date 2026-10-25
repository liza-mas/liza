package lifecycle

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// The tests below carry the Git-version and Windows-launch portability of the
// lifecycle variant drivers (D53). They assert names and booleans only: a
// failing message never carries a host environment value (G1.4).

// fixtureExtras are the fixture variables that follow the harness policy names,
// in the order newEnvironment appends them.
var fixtureExtras = []string{"TZ", "LIZA_SKIP_AUTO_UPDATE", "LIZA_PROVIDER_CATALOG_URL", "GIT_CONFIG_NOSYSTEM", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_AUTHOR_DATE", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_DATE"}

func envNames(t *testing.T, env []string) []string {
	t.Helper()
	var names []string
	for _, pair := range env {
		name, _, ok := strings.Cut(pair, "=")
		if !ok {
			t.Fatal("environment pair missing separator")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func envValue(env []string, name string) (string, bool) {
	for _, pair := range env {
		if value, ok := strings.CutPrefix(pair, name+"="); ok {
			return value, true
		}
	}
	return "", false
}

// R5: the fixture environment is exactly the harness policy for this platform
// plus the existing extras and the caller names, with the disposable
// directories in every home and temporary-directory variable, and a PATH
// directory holding one entry.
func TestLifecycleEnvironmentPolicy(t *testing.T) {
	t.Setenv("LIFECYCLE_HOST_ONLY", "fixture-only")
	policy, err := oracle.ChildEnv(oracle.EnvBase{Home: "home", Path: "path", Tmp: "tmp"})
	if err != nil {
		t.Fatal("harness environment policy:", err)
	}
	for _, caller := range []string{"", "coder-1"} {
		r := newEnvironment(t, caller)
		want := append(envNames(t, policy), fixtureExtras...)
		if caller != "" {
			want = append(want, "LIZA_AGENT_ID", "LIZA_AGENT_GENERATION")
		}
		sort.Strings(want)
		if got := envNames(t, r.env); !slices.Equal(got, want) {
			t.Fatalf("environment names differ from the %s policy: got %v; want %v", runtime.GOOS, got, want)
		}
		homes := []string{"HOME"}
		temps := []string{"TMPDIR"}
		if runtime.GOOS == "windows" {
			homes, temps = append(homes, "USERPROFILE"), append(temps, "TMP", "TEMP")
		}
		for _, name := range homes {
			if value, _ := envValue(r.env, name); value != r.home {
				t.Fatalf("%s is not the fixture home", name)
			}
		}
		for _, name := range temps {
			if value, _ := envValue(r.env, name); value != r.tmp {
				t.Fatalf("%s is not the fixture temporary directory", name)
			}
		}
		if value, _ := envValue(r.env, "PATH"); value != r.path {
			t.Fatal("PATH is not the fixture bin directory")
		}
		if entries, err := os.ReadDir(r.path); err != nil || len(entries) != 1 {
			t.Fatal("PATH directory must hold exactly one entry")
		}
	}
}

// R1: the layout of the PATH directory per platform. Windows launches only an
// executable with an extension of the default PATHEXT set, and never a
// .cmd/.bat shim (D57); Linux and macOS keep the symlink named git.
func TestLifecycleGitLayout(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		if got := gitLayout(goos); got != (gitInstall{entry: "git"}) {
			t.Errorf("%s: entry must be the symlink named git", goos)
		}
	}
	got := gitLayout("windows")
	ext := strings.ToLower(path.Ext(got.entry))
	if !slices.Contains([]string{".com", ".exe"}, ext) {
		t.Errorf("windows: entry must carry an executable PATHEXT extension that is not .cmd/.bat")
	}
	if !got.shim || strings.TrimSuffix(got.entry, path.Ext(got.entry)) != "git" {
		t.Errorf("windows: entry must be the forwarding shim named git")
	}
}

const shimPackage = "./internal/oracle/a3/lifecycle/testdata/gitshim"

// pathSpellings lists the ways Git can print a directory: as given, with
// slashes (Git for Windows), and with symlinks resolved (a symlinked temporary
// root on macOS, or an alias), longest first so that a spelling that contains
// another is replaced whole.
func pathSpellings(dir string) []string {
	seen := map[string]bool{}
	var spellings []string
	add := func(candidate string) {
		for _, spelling := range []string{candidate, filepath.ToSlash(candidate)} {
			if spelling != "" && !seen[spelling] {
				seen[spelling] = true
				spellings = append(spellings, spelling)
			}
		}
	}
	add(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		add(resolved)
	}
	sort.SliceStable(spellings, func(i, j int) bool { return len(spellings[i]) > len(spellings[j]) })
	return spellings
}

// maskPaths replaces every spelling of the disposable project with a token,
// ignoring case on Windows, where paths are case-insensitive.
func maskPaths(text string, spellings []string) string {
	for _, spelling := range spellings {
		pattern := regexp.QuoteMeta(spelling)
		if runtime.GOOS == "windows" {
			pattern = "(?i)" + pattern
		}
		text = regexp.MustCompile(pattern).ReplaceAllLiteralString(text, "<PROJECT>")
	}
	return text
}

// shimResults runs a Git sequence with bin as the fixture's Git in a fresh
// environment and returns each probe's result, keyed by probe name. The
// sequence covers what the lifecycle drivers need from Git through the shim:
// argv, stdin, exit status and stderr, working directory, the fixture
// identity and dates (commit ids are deterministic) and a conflicting rebase.
// The disposable project path is masked, so two environments compare equal.
func shimResults(t *testing.T, r environment, bin string) map[string]commandResult {
	t.Helper()
	results := map[string]commandResult{}
	spellings := pathSpellings(r.project)
	run := func(name, dir, stdin string, args ...string) {
		t.Helper()
		at := r
		if dir != "" {
			at.project = dir
		}
		res := at.commandStdin(t, bin, stdin, args...)
		res.stdout, res.stderr = maskPaths(res.stdout, spellings), maskPaths(res.stderr, spellings)
		results[name] = res
	}
	readme := filepath.Join(r.project, "README")
	run("init", "", "", "init", "-b", "main")
	writeFile(t, readme, []byte("base\n"))
	run("add", "", "", "add", "README")
	run("commit", "", "", "commit", "-m", "base message")
	run("argv", "", "", "rev-parse", "--sq-quote", "with space", "it's", `back\slash`, "", `say "hi"`)
	run("stdin", "", "shim stdin\n", "hash-object", "--stdin")
	run("missing", "", "", "rev-parse", "--verify", "refs/heads/missing")
	sub := filepath.Join(r.project, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal("create subdirectory:", err)
	}
	run("toplevel", sub, "", "rev-parse", "--show-toplevel")
	run("ident", "", "", "var", "GIT_COMMITTER_IDENT")
	run("side", "", "", "checkout", "-b", "side")
	writeFile(t, readme, []byte("side\n"))
	run("side-commit", "", "", "commit", "-a", "-m", "side")
	run("main", "", "", "checkout", "main")
	writeFile(t, readme, []byte("main\n"))
	run("main-commit", "", "", "commit", "-a", "-m", "main")
	run("conflict", "", "", "rebase", "side")
	return results
}

// shimDivergences names the probes on which the shim differs from direct host
// Git, and returns the direct results.
func shimDivergences(t *testing.T, shimBin string, aliased bool) ([]string, map[string]commandResult) {
	t.Helper()
	host, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("host git unavailable")
	}
	direct := newEnvironment(t, "")
	viaShim := newEnvironmentWith(t, "", shimLayout(runtime.GOOS), shimBin)
	if aliased {
		// Git prints the resolved path of a project reached through a symlink.
		for _, r := range []*environment{&direct, &viaShim} {
			alias := filepath.Join(filepath.Dir(r.project), "project-alias")
			if err := os.Symlink(r.project, alias); err != nil {
				t.Fatal("create project alias:", err)
			}
			r.project = alias
		}
	}
	want := shimResults(t, direct, host)
	got := shimResults(t, viaShim, viaShim.gitBin)
	var names []string
	for name, res := range want {
		if got[name] != res {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, want
}

// buildShimVariant builds the shim source with one textual defect. The anchor
// must exist, so a rewritten shim cannot silently disarm the control.
func buildShimVariant(t *testing.T, anchor, defect string) string {
	t.Helper()
	src := string(readFile(t, "testdata/gitshim/main.go"))
	if !strings.Contains(src, anchor) {
		t.Fatal("shim defect anchor missing from the shim source")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), []byte(strings.Replace(src, anchor, defect, 1)))
	home, err := os.UserHomeDir()
	if err != nil {
		home = dir
	}
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: home, Path: os.Getenv("PATH"), Tmp: dir, Go: true})
	if err != nil {
		t.Fatal("shim variant environment:", err)
	}
	bin := filepath.Join(dir, "variant")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, "main.go")
	cmd.Dir, cmd.Env = dir, env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("build shim variant: %v\n%s", err, out.String())
	}
	return bin
}

// R2: the shim is transparent. Run as the fixture's PATH Git it gives the same
// stdout, stderr and exit status as direct host Git for every probe.
func TestLifecycleGitShimFidelity(t *testing.T) {
	shim := oracle.BuildCLI(t, shimPackage)
	diverged, direct := shimDivergences(t, shim, false)
	if len(diverged) != 0 {
		t.Fatalf("shim differs from host git on probes %v", diverged)
	}
	// The probes must observe something, or equality proves nothing.
	if !strings.Contains(direct["argv"].stdout, "''") || !strings.Contains(direct["argv"].stdout, "with space") {
		t.Error("argv probe did not round-trip an empty and a spaced argument")
	}
	if direct["stdin"].stdout == "" {
		t.Error("stdin probe produced no object id")
	}
	if direct["missing"].exit == 0 || direct["missing"].stderr == "" {
		t.Error("failure probe lacks a non-zero exit and stderr")
	}
	if direct["toplevel"].stdout != "<PROJECT>\n" {
		t.Error("working directory probe did not resolve the fixture project")
	}
	if !strings.Contains(direct["ident"].stdout, "Oracle <oracle@example.invalid> 1577836800") {
		t.Error("identity probe does not show the fixture identity and date")
	}
	if conflict := direct["conflict"]; conflict.exit != 1 || !strings.Contains(conflict.stdout+conflict.stderr, "CONFLICT") {
		t.Error("conflict probe did not conflict")
	}
}

// R2/R3 negative controls: shims that drop an argument or the exit status
// diverge from host Git on the probes that observe them, so a faulty shim
// cannot pass the fidelity test or carry a scenario.
func TestLifecycleGitShimDefectsDetected(t *testing.T) {
	for name, tc := range map[string]struct{ anchor, defect, probe string }{
		"dropped argument":    {"os.Args[1:]", "os.Args[1:len(os.Args)-1]", "argv"},
		"dropped exit status": {"return exit.ExitCode()", "return 0", "missing"},
	} {
		t.Run(name, func(t *testing.T) {
			diverged, _ := shimDivergences(t, buildShimVariant(t, tc.anchor, tc.defect), false)
			if !slices.Contains(diverged, tc.probe) {
				t.Fatalf("defect went undetected by probe %s (diverged: %v)", tc.probe, diverged)
			}
		})
	}
}

// R2 on a project reached through a symlink: Git prints the resolved path, so
// fidelity must hold for every spelling of the fixture root. Creating a symlink
// may be refused to an unprivileged Windows user; the spellings Windows Git
// prints are covered by TestLifecyclePathSpellingsMask on every platform.
func TestLifecycleGitShimFidelityAliasedRoot(t *testing.T) {
	probe := filepath.Join(t.TempDir(), "probe")
	if err := os.Symlink(t.TempDir(), probe); err != nil {
		if runtime.GOOS == "windows" {
			t.Log("symlinks unavailable to this user; aliased root not exercised")
			return
		}
		t.Fatal("create symlink:", err)
	}
	shim := oracle.BuildCLI(t, shimPackage)
	if diverged, _ := shimDivergences(t, shim, true); len(diverged) != 0 {
		t.Fatalf("shim differs from host git on probes %v for an aliased project", diverged)
	}
	for name, tc := range map[string]struct{ anchor, defect, probe string }{
		"dropped argument":    {"os.Args[1:]", "os.Args[1:len(os.Args)-1]", "argv"},
		"dropped exit status": {"return exit.ExitCode()", "return 0", "missing"},
	} {
		diverged, _ := shimDivergences(t, buildShimVariant(t, tc.anchor, tc.defect), true)
		if !slices.Contains(diverged, tc.probe) {
			t.Errorf("%s: defect went undetected for an aliased project (diverged: %v)", name, diverged)
		}
	}
}

func TestLifecyclePathSpellingsMask(t *testing.T) {
	// A resolved root that contains the alias as a suffix, as on macOS where
	// /var is a link to /private/var, and Git for Windows' slash spelling.
	spellings := []string{"/private/var/T/p", "/var/T/p", `C:\Users\R\p`, "C:/Users/R/p"}
	sort.SliceStable(spellings, func(i, j int) bool { return len(spellings[i]) > len(spellings[j]) })
	for in, want := range map[string]string{
		"Initialized in /private/var/T/p/.git/":   "Initialized in <PROJECT>/.git/",
		"/var/T/p":                                "<PROJECT>",
		"C:/Users/R/p\nfatal at C:/Users/R/p/sub": "<PROJECT>\nfatal at <PROJECT>/sub",
		"/private/var/T/other":                    "/private/var/T/other",
	} {
		if runtime.GOOS == "windows" && strings.Contains(in, "/var/") {
			continue // case folding applies only to Windows spellings
		}
		if got := maskPaths(in, spellings); got != want {
			t.Errorf("masking %q: got %q; want %q", in, got, want)
		}
	}
	dir := t.TempDir()
	got := pathSpellings(dir)
	if !slices.Contains(got, dir) || !slices.Contains(got, filepath.ToSlash(dir)) {
		t.Error("spellings omit the native or slash form")
	}
	for i := 1; i < len(got); i++ {
		if len(got[i]) > len(got[i-1]) {
			t.Error("spellings must be ordered longest first")
		}
	}
}

// R3: the shim, forced on this host, runs a conflicting-rebase scenario and an
// assess scenario to the committed goldens, so the Windows layout cannot change
// a verdict.
func TestLifecycleGitShimScenarios(t *testing.T) {
	unblock := []string{"unblock-task", "task-1", "--reason", "oracle-reason", "--rebase-on", "integration"}
	assess := []string{"assess-blocked", "task-1", "--note", "oracle-note"}
	runCases(t, "unblock-task",
		lifecycleCase{name: "rebase-on-refusal-conflict", fixture: "blocked+integration-conflict", args: unblock, mutating: true, gitTranscript: true, gitShim: true})
	runCases(t, "assess-blocked",
		lifecycleCase{name: "unchanged-no-change", fixture: "blocked", caller: "orchestrator-1", invocations: [][]string{assess, assess}, mutating: true, gitShim: true})
}
