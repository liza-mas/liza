package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type modes uint8

const (
	jsonMode modes = 1 << iota
	humanMode
	bothModes = jsonMode | humanMode
)

type setupFile struct {
	name string
	data []byte
}

// lifecycleCase is the additive seam for L8-L15: no driver needs to edit the
// convention layer. setupFiles are ordered, relative to the fresh TMPDIR;
// invocation arguments can refer to that directory with {{TMPDIR}}.
type lifecycleCase struct {
	name, fixture, caller string
	args                  []string
	invocations           [][]string
	modes                 modes
	mutating              bool
	// L15 invokes JSON mode but freezes only its A3-owned state surface.
	stateOnly  bool
	setupFiles []setupFile
	// A stranded claim takeover advances the task boundary at run time, so the
	// receipt's expected_transition varies between runs. Only a case that needs
	// it opts in; every other receipt digest stays a digit-for-digit assertion.
	maskBoundaries bool
	// gitTranscript marks a case whose final state freezes Git's own rebase
	// conflict output, which differs between Git versions. Production-owned
	// text stays asserted; only the Git-produced remainder is normalised
	// (maskGitTranscript). Every other case compares its text unchanged.
	gitTranscript bool
	// gitShim installs the forwarding Git shim on any host, to prove that the
	// platform-neutral launch yields the committed goldens (R3). Without it a
	// case uses the platform's layout.
	gitShim bool
}

func (c lifecycleCase) gitInstall() gitInstall {
	if c.gitShim {
		return shimLayout(runtime.GOOS)
	}
	return gitLayout(runtime.GOOS)
}

// normaliseState applies the case's opt-in determinism edits to a masked state
// text.
func (c lifecycleCase) normaliseState(text string) (string, error) {
	if !c.gitTranscript {
		return text, nil
	}
	return maskGitTranscript(text)
}

func runCases(t *testing.T, command string, cases ...lifecycleCase) {
	t.Helper()
	bin := oracle.BuildCLI(t, "./cmd/liza")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runCase(t, bin, command, c) })
	}
}

func runCase(t *testing.T, bin, command string, c lifecycleCase) {
	t.Helper()
	if c.modes == 0 {
		c.modes = bothModes
	}
	var jsonAfter string
	for _, mode := range []modes{jsonMode, humanMode} {
		if c.modes&mode == 0 {
			continue
		}
		label := "json"
		if mode == humanMode {
			label = "human"
		}
		t.Run(label, func(t *testing.T) {
			f := buildFixture(t, bin, c.fixture, c.caller, c.gitInstall())
			for _, file := range c.setupFiles {
				if filepath.IsAbs(file.name) || filepath.Clean(file.name) != file.name || file.name == ".." || strings.HasPrefix(file.name, ".."+string(filepath.Separator)) {
					t.Fatal("setup file must stay under TMPDIR")
				}
				writeFile(t, filepath.Join(f.tmp, file.name), file.data)
			}
			calls := c.invocations
			if len(calls) == 0 {
				calls = [][]string{c.args}
			}
			start := time.Now().Truncate(time.Second)
			var results []commandResult
			for _, invocation := range calls {
				args := append([]string(nil), invocation...)
				for i, arg := range args {
					args[i] = strings.ReplaceAll(arg, "{{TMPDIR}}", f.tmp)
				}
				if !forceAllowed(args, f.targetDead) {
					t.Fatal("OC4: --force cannot target a live fixture agent")
				}
				if mode == jsonMode {
					args = append(args, "--json")
				}
				result := f.command(t, bin, args...)
				result.stdout, result.stderr = stripLogs(result.stdout), stripLogs(result.stderr)
				results = append(results, result)
			}
			final := string(readFile(t, filepath.Join(f.project, ".liza", "state.yaml")))
			paths := map[string]string{f.project: "<PROJECT>", f.home: "<HOME>", f.tmp: "<TMP>"}
			newRunMask := func() *mask {
				m := newMask(start, f.commits, paths, os.Getpid())
				m.discover(f.before, final)
				if c.maskBoundaries {
					m.discoverBoundaries(final)
				}
				for _, res := range results {
					m.discover(res.stdout, res.stderr)
				}
				return m
			}
			name := command + "/" + c.name
			if mode == jsonMode && !c.stateOnly {
				var runs []any
				for _, res := range results {
					var doc any
					decoder := json.NewDecoder(strings.NewReader(res.stdout))
					decoder.UseNumber()
					if err := decoder.Decode(&doc); err != nil {
						t.Fatalf("case %s: stdout is not JSON (exit %d)", name, res.exit)
					}
					var trailing any
					if decoder.Decode(&trailing) != io.EOF {
						t.Fatalf("case %s: stdout has trailing content (exit %d)", name, res.exit)
					}
					runs = append(runs, map[string]any{"exit_code": res.exit, "stdout": doc, "stderr": streamLines(res.stderr)})
				}
				masked := newRunMask().document(map[string]any{"runs": runs})
				data, err := json.Marshal(masked)
				if err != nil {
					t.Fatal("marshal masked run:", err)
				}
				oracle.Check(t, oracle.JSON, name+".json", data)
			} else if mode == humanMode && !c.stateOnly {
				var text strings.Builder
				for _, res := range results {
					fmt.Fprintf(&text, "exit %d\nstdout:\n%sstderr:\n%s", res.exit, terminated(res.stdout), terminated(res.stderr))
				}
				oracle.Check(t, oracle.Human, name+".txt", []byte(newRunMask().text(text.String())))
			}
			if !c.mutating {
				return
			}
			stateMask := newRunMask()
			before := stateMask.text(f.before)
			after := stateMask.text(final + "\n# git\n" + gitEffects(f.gitBefore, f.snapshot(t)) + "# lifecycle outcomes\n" + outcomeLines(t, f.project) + fmt.Sprintf("# exit %d\n", results[len(results)-1].exit))
			after, err := c.normaliseState(after)
			if err != nil {
				t.Fatalf("case %s: %v", name, err)
			}
			if mode == jsonMode {
				jsonAfter = after
				oracle.CheckState(t, name+".txt", []byte(before), []byte(after))
			} else if c.modes&jsonMode != 0 {
				// Some commands deliberately use a different exit convention in
				// human mode. Such read-only commands have no state golden.
				if !bytes.Equal([]byte(jsonAfter), []byte(after)) {
					t.Fatalf("case %s: human/json final state differs at line %d", name, differenceLine(jsonAfter, after))
				}
			}
		})
	}
}

func terminated(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

func streamLines(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func differenceLine(a, b string) int {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < min(len(x), len(y)); i++ {
		if x[i] != y[i] {
			return i + 1
		}
	}
	return min(len(x), len(y)) + 1
}
