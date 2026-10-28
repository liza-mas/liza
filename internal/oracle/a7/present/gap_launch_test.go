package present

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// TestOracleLaunchBackends pins the frozen surface of `launch` (gap G-c): the
// binary runs on a constructed PATH holding stub cmux, wezterm and provider
// executables that record argv and stdin, and the recorded invocation sequence,
// stdout, stderr and exit code are the golden. The stubs are not real
// multiplexers or provider CLIs: cmux answers every screen read at once, the
// wezterm stub only records, and a provider stub records if the launcher ever
// executes it (it must not). Cases cover both backends for the multi-agent
// (command panes) and adversarial-pairing (interactive panes) launches, live
// and dry-run, blackboard seeding, --cwd resolution and the refusals of every
// resolution and backend step.
func TestOracleLaunchBackends(t *testing.T) {
	skipUnlessLinux(t)
	for _, tc := range launchCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runLaunchCase(t, tc)
		})
	}
}

type launchCase struct {
	name string
	// args builds the CLI arguments from the project root and a scratch
	// directory outside it.
	args func(project, other string) []string
	// stdin is forwarded to the CLI; the wezterm backend hands it to the terminal.
	stdin string
	// noBackend leaves cmux and wezterm off PATH.
	noBackend bool
	// env adds variables to the child environment.
	env []string
	// prepare runs after the fixture project exists.
	prepare func(t *testing.T, project, other string)
	// board is the blackboard path, relative to the project, recorded after the run.
	board string
	// dir selects the working directory: the project (default) or "other".
	dir string
}

func fixedArgs(args ...string) func(project, other string) []string {
	return func(string, string) []string { return args }
}

func withBoard(t *testing.T, project, other string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(project, "board.md"), []byte("existing blackboard\n"), 0o644); err != nil {
		t.Fatal("write fixture blackboard")
	}
}

func launchCases() []launchCase {
	cases := []launchCase{}
	for _, backend := range []string{"wezterm", "cmux"} {
		b := backend
		add := func(c launchCase) {
			c.name = b + "-" + c.name
			cases = append(cases, c)
		}
		launch := func(sub string, rest ...string) func(string, string) []string {
			return fixedArgs(append([]string{"launch", b, sub}, rest...)...)
		}
		pairingBoard := func(rest ...string) func(string, string) []string {
			return launch("adversarial-pairing", append([]string{"board.md"}, rest...)...)
		}
		add(launchCase{name: "mas-dry-run", args: launch("mas", "--dry-run")})
		add(launchCase{name: "mas-live", args: launch("mas"), stdin: "oracle-stdin\n"})
		add(launchCase{name: "mas-options", args: launch("mas", "--preset", "functional-spec", "--cli", "codex", "--no-tui", "--class", "oracle-class", "--workspace", "oracle-workspace", "--dry-run")})
		add(launchCase{name: "mas-general-objective-class-only", args: launch("mas", "--preset", "general-objective", "--class", "oracle-class", "--dry-run")})
		add(launchCase{name: "mas-roles-and-shell", args: launch("mas", "--role", "coder", "--role", " code-reviewer ", "--dry-run"), env: []string{"SHELL=/oracle/bin/shell"}})
		add(launchCase{name: "mas-unknown-preset", args: launch("mas", "--preset", "oracle-unknown", "--dry-run")})
		add(launchCase{name: "mas-empty-role", args: launch("mas", "--role", " ", "--dry-run")})
		add(launchCase{name: "mas-invalid-cli", args: launch("mas", "--cli", "oracle-unknown", "--dry-run")})
		add(launchCase{name: "mas-missing-backend", args: launch("mas"), noBackend: true})
		add(launchCase{name: "mas-cwd-from-elsewhere", dir: "other", args: func(project, _ string) []string {
			return []string{"launch", b, "mas", "--cwd", project, "--dry-run"}
		}})
		add(launchCase{name: "mas-cwd-subdirectory", args: func(project, _ string) []string {
			return []string{"launch", b, "mas", "--cwd", filepath.Join(project, "sub"), "--dry-run"}
		}, prepare: func(t *testing.T, project, _ string) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(project, "sub"), 0o755); err != nil {
				t.Fatal("create fixture subdirectory")
			}
		}})
		add(launchCase{name: "mas-cwd-uninitialized", args: func(_, other string) []string {
			return []string{"launch", b, "mas", "--cwd", filepath.Join(other, "bare"), "--dry-run"}
		}, prepare: func(t *testing.T, _, other string) {
			t.Helper()
			bare := filepath.Join(other, "bare")
			if err := os.Mkdir(bare, 0o755); err != nil {
				t.Fatal("create bare directory")
			}
			if out, err := exec.Command("git", "-C", bare, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("initialise bare repository: %v (%d bytes of output)", err, len(out))
			}
		}})
		add(launchCase{name: "mas-cwd-not-directory", args: func(_, other string) []string {
			return []string{"launch", b, "mas", "--cwd", filepath.Join(other, "file"), "--dry-run"}
		}, prepare: func(t *testing.T, _, other string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(other, "file"), []byte("x"), 0o644); err != nil {
				t.Fatal("write fixture file")
			}
		}})
		add(launchCase{name: "mas-cwd-missing", args: func(_, other string) []string {
			return []string{"launch", b, "mas", "--cwd", filepath.Join(other, "absent"), "--dry-run"}
		}})

		add(launchCase{name: "pairing-dry-run", args: pairingBoard("--dry-run"), prepare: withBoard})
		codexPanes := []string{"--yolo"}
		if b == "wezterm" {
			// Only the wezterm backend injects prompts after a delay.
			codexPanes = append(codexPanes, "--prompt-delay", "1500ms")
		}
		add(launchCase{name: "pairing-live-codex-panes", args: pairingBoard(codexPanes...), prepare: withBoard, stdin: "oracle-stdin\n"})
		add(launchCase{name: "pairing-live-mixed-panes", args: pairingBoard("--doer-cli", "claude", "--reviewer", "codex"), prepare: withBoard, stdin: "oracle-stdin\n"})
		add(launchCase{name: "pairing-live-non-codex-panes", args: pairingBoard("--doer-cli", "claude", "--reviewer", "reviewer-two=claude"), prepare: withBoard})
		add(launchCase{name: "pairing-seeded-live", args: launch("adversarial-pairing", "notes/new-board.md", "--goal", "  Oracle goal text  ", "--doer-cli", "claude", "--reviewer", "claude", "--yolo"), board: "notes/new-board.md"})
		add(launchCase{name: "pairing-seeded-dry-run", args: launch("adversarial-pairing", "notes/new-board.md", "--goal", "Oracle goal text", "--dry-run"), board: "notes/new-board.md"})
		add(launchCase{name: "pairing-no-goal-no-board", args: launch("adversarial-pairing", "notes/new-board.md", "--dry-run"), board: "notes/new-board.md"})
		add(launchCase{name: "pairing-invalid-doer-cli", args: pairingBoard("--doer-cli", "oracle-unknown", "--dry-run"), prepare: withBoard})
		add(launchCase{name: "pairing-invalid-reviewer-cli", args: pairingBoard("--reviewer", "oracle-unknown", "--dry-run"), prepare: withBoard})
		add(launchCase{name: "pairing-empty-reviewer", args: pairingBoard("--reviewer", " ", "--dry-run"), prepare: withBoard})
		add(launchCase{name: "pairing-incomplete-reviewer", args: pairingBoard("--reviewer", "reviewer-=codex", "--dry-run"), prepare: withBoard})
		add(launchCase{name: "pairing-missing-backend", args: pairingBoard(), prepare: withBoard, noBackend: true})
		add(launchCase{name: "pairing-cwd-relative-board", dir: "other", args: func(_, other string) []string {
			return []string{"launch", b, "adversarial-pairing", "board.md", "--cwd", other, "--dry-run"}
		}, prepare: func(t *testing.T, _, other string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(other, "board.md"), []byte("existing blackboard\n"), 0o644); err != nil {
				t.Fatal("write fixture blackboard")
			}
		}})
	}
	return cases
}

func runLaunchCase(t *testing.T, tc launchCase) {
	t.Helper()
	e := newEnvironment(t, tc.env...)
	project := materialiseFixture(t, e)
	other := filepath.Join(e.root, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal("create scratch directory")
	}
	e.norm.Dir(other, "<OTHER>")
	log := filepath.Join(e.root, "launch.log")
	counter := filepath.Join(e.root, "launch.count")
	stubs := map[string]string{"codex": "", "claude": ""}
	if !tc.noBackend {
		stubs["cmux"] = cmuxStubBody(counter)
		stubs["wezterm"] = weztermStubBody()
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(e.bin, name), []byte(launchStub(name, log, body)), 0o755); err != nil {
			t.Fatalf("write %s stub", name)
		}
	}
	if tc.prepare != nil {
		tc.prepare(t, project, other)
	}
	dir := project
	if tc.dir == "other" {
		dir = other
	}
	result := runCLI(t, e, dir, []byte(tc.stdin), tc.args(project, other)...)

	var record strings.Builder
	fmt.Fprintf(&record, "exit %d\n--- stdout\n%s\n--- stderr\n%s\n--- invocations\n", result.exit, result.stdout, result.stderr)
	invocations, err := os.ReadFile(log)
	switch {
	case err == nil:
		record.Write(invocations)
	case os.IsNotExist(err):
		record.WriteString("none\n")
	default:
		t.Fatal("read recorded invocations")
	}
	if tc.board != "" {
		board, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(tc.board)))
		switch {
		case err == nil:
			fmt.Fprintf(&record, "--- blackboard %s\n%s", tc.board, board)
		case os.IsNotExist(err):
			fmt.Fprintf(&record, "--- blackboard %s\nabsent\n", tc.board)
		default:
			t.Fatal("read seeded blackboard")
		}
	}
	e.norm.Check(t, oracle.Human, "launch/"+tc.name+".txt", []byte(record.String()))
}

// launchStub is a POSIX shell script that appends its name, argv (one argument
// per entry, a multi-line argument spanning lines) and stdin to the shared log,
// then runs its body. Only shell builtins are used: PATH holds nothing else.
func launchStub(name, log, body string) string {
	return fmt.Sprintf(`#!/bin/sh
log='%s'
printf '== %%s\n' '%s' >> "$log"
i=0
for a in "$@"; do
  i=$((i+1))
  printf 'arg %%d: %%s\n' "$i" "$a" >> "$log"
done
while IFS= read -r line || [ -n "$line" ]; do
  printf 'stdin: %%s\n' "$line" >> "$log"
done
%s
exit 0
`, log, name, body)
}

// weztermStubBody only records: it prints one line on each stream so the
// launcher's stream pass-through is part of the golden.
func weztermStubBody() string {
	return `echo "wezterm-stub: started"
echo "wezterm-stub: notice" >&2`
}

// cmuxStubBody answers like cmux: workspace and surface references on creation,
// a selected surface listing, and an interactive Codex screen on every read.
func cmuxStubBody(counter string) string {
	return fmt.Sprintf(`counter='%s'
case "$1" in
new-workspace) echo "OK workspace:42" ;;
new-pane)
  n=0
  [ -f "$counter" ] && read -r n < "$counter"
  n=$((n+1))
  printf '%%s\n' "$n" > "$counter"
  echo "OK surface:$((6+n)) pane:$((6+n)) workspace:42"
  ;;
list-pane-surfaces) echo "* surface:5 'codex' [selected]" ;;
read-screen)
  echo "OpenAI Codex"
  echo "›"
  ;;
send|send-key) ;;
*) echo "cmux-stub: unknown subcommand" >&2; exit 64 ;;
esac`, counter)
}
