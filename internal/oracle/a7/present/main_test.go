package present

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) { oracle.RunMain(m) }

func skipUnlessLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("A7 oracle fixtures require Linux process identity and Git symlinks; goldens are generated on Linux")
	}
}

type environment struct {
	root, home, project, tmp, bin string
	vars                          []string
	norm                          *oracle.Normaliser
}

func newEnvironment(t *testing.T, extra ...string) *environment {
	t.Helper()
	e := &environment{root: t.TempDir(), norm: oracle.NewNormaliser()}
	e.home, e.project = filepath.Join(e.root, "home"), filepath.Join(e.root, "project")
	e.tmp, e.bin = filepath.Join(e.root, "tmp"), filepath.Join(e.root, "bin")
	for _, dir := range []string{e.home, e.project, e.tmp, e.bin} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("create fixture directory %s", filepath.Base(dir))
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("fixture requires host git")
	}
	if err := os.Symlink(git, filepath.Join(e.bin, "git")); err != nil {
		t.Fatal("create fixture git symlink")
	}
	pairs := []string{
		"LIZA_PROVIDER_CATALOG_URL=http://catalog.invalid/", "LIZA_SKIP_AUTO_UPDATE=1",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Oracle", "GIT_AUTHOR_EMAIL=oracle@example.invalid",
		"GIT_COMMITTER_NAME=Oracle", "GIT_COMMITTER_EMAIL=oracle@example.invalid",
		"GIT_AUTHOR_DATE=2001-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2001-01-01T00:00:00Z",
	}
	e.vars, err = oracle.ChildEnv(oracle.EnvBase{Home: e.home, Path: e.bin, Tmp: e.tmp}, append(pairs, extra...)...)
	if err != nil {
		t.Fatal("construct fixture environment (variable names only)")
	}
	e.norm.Dir(e.home, oracle.TokenHome)
	e.norm.Dir(e.project, oracle.TokenProject)
	e.norm.Dir(e.tmp, oracle.TokenTmp)
	e.norm.Dir(e.root, oracle.TokenTmp)
	return e
}

type cliResult struct {
	stdout, stderr []byte
	exit           int
}

func runCLI(t *testing.T, e *environment, dir string, stdin []byte, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, oracle.BuildCLI(t, "./cmd/liza"), args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, e.vars, bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	if err != nil {
		if !errors.As(err, &exit) || ctx.Err() != nil {
			t.Fatalf("CLI invocation %s failed to execute", strings.Join(args, " "))
		}
		code = exit.ExitCode()
	}
	return cliResult{stdout.Bytes(), stderr.Bytes(), code}
}

// This is the only A7-specific mask table. Keys and labels identify relative
// clocks; no status, count, error code, identity or ordinary numeric value is
// masked. Table columns are addressed by their printed headers.
var a7Masks = struct {
	keys    map[string]bool
	labels  *regexp.Regexp
	columns map[string]bool
	goal    *regexp.Regexp
	stall   *regexp.Regexp
	scalar  *regexp.Regexp
}{
	keys: map[string]bool{
		"age": true, "time_in_status": true, "elapsed": true, "lease_expires": true,
		"time_on_task": true, "time_since_heartbeat": true, "last_progress_age": true,
	},
	labels:  regexp.MustCompile(`(?im)^([ \t]*(?:age|time_in_status|elapsed|lease_expires|time_on_task|time_since_heartbeat|timesinceheartbeat|last_progress_age|Time in Status|Time on Task|Last Heartbeat|Lease Expires):[ \t]*).*$`),
	columns: map[string]bool{"AGE": true, "TIME_IN_STATUS": true, "TIME_ON_TASK": true, "HEARTBEAT": true, "LEASE": true},
	goal:    regexp.MustCompile(`goal-[0-9]+`),
	stall:   regexp.MustCompile(`(no task progress for )[0-9]+( minutes)`),
	scalar:  regexp.MustCompile(`(?m)^-?[0-9]+[dhms](?: [0-9]+[dhms])*$`),
}

func maskJSON(v any) any {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			if a7Masks.keys[key] {
				value[key] = "<REL>"
			} else {
				value[key] = maskJSON(child)
			}
		}
	case []any:
		for i := range value {
			value[i] = maskJSON(value[i])
		}
	}
	return v
}

var headerCell = regexp.MustCompile(`\S+`)

func maskHuman(text string) string {
	text = a7Masks.goal.ReplaceAllString(text, "goal-<ID>")
	text = a7Masks.labels.ReplaceAllString(text, "${1}<REL>")
	text = a7Masks.stall.ReplaceAllString(text, "${1}<REL>${2}")
	lines := strings.Split(text, "\n")
	var headers []string
	var offsets [][]int
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 1 && (fields[0] == "ID" || fields[0] == "AGE") {
			headers, offsets = fields, headerCell.FindAllStringIndex(line, -1)
			continue
		}
		if strings.TrimSpace(line) == "" {
			headers, offsets = nil, nil
		}
		// FormatTable pads in bytes. Header offsets preserve empty cells and
		// multiword values (including the PID's Unicode process marker).
		var cells []string
		masked := false
		for j, header := range headers {
			start, end := min(offsets[j][0], len(line)), len(line)
			if j+1 < len(offsets) {
				end = min(offsets[j+1][0], len(line))
			}
			cell := strings.TrimSpace(line[start:end])
			if a7Masks.columns[strings.ToUpper(header)] && cell != "" && cell != "-" {
				cell, masked = "<REL>", true
			}
			cells = append(cells, cell)
		}
		if masked {
			lines[i] = strings.Join(cells, "  ")
		}
	}
	return strings.Join(lines, "\n")
}

func checkResult(t *testing.T, e *environment, kind oracle.Kind, name string, result cliResult) {
	t.Helper()
	if kind == oracle.Artifact {
		if result.exit != 0 || len(result.stderr) != 0 {
			t.Fatalf("artifact %s: exit %d, stderr present=%t", name, result.exit, len(result.stderr) != 0)
		}
		e.norm.Check(t, kind, name, result.stdout)
		return
	}
	var record []byte
	if kind == oracle.JSON {
		var out any
		decoder := json.NewDecoder(bytes.NewReader(result.stdout))
		decoder.UseNumber()
		if err := decoder.Decode(&out); err != nil {
			out = string(result.stdout)
		}
		status := strings.HasPrefix(name, "status/")
		if status {
			sortStatusAgentsJSON(out)
		}
		stderr := maskHuman(string(result.stderr))
		// --format json is printed on stderr by the starting CLI. Keep that
		// stream as a string, but normalize its JSON keys and D28 agent order.
		if status && json.Valid(result.stderr) {
			decoder := json.NewDecoder(bytes.NewReader(result.stderr))
			decoder.UseNumber()
			var value any
			if err := decoder.Decode(&value); err != nil {
				t.Fatalf("decode status stderr for %s", name)
			}
			sortStatusAgentsJSON(value)
			data, err := json.MarshalIndent(maskJSON(value), "", "  ")
			if err != nil {
				t.Fatalf("encode status stderr for %s", name)
			}
			stderr = string(data) + "\n"
		}
		var err error
		record, err = json.Marshal(map[string]any{"exit": result.exit, "stdout": maskJSON(out), "stderr": stderr})
		if err != nil {
			t.Fatalf("encode golden record %s", name)
		}
		record = a7Masks.goal.ReplaceAll(record, []byte("goal-<ID>"))
	} else {
		text := fmt.Sprintf("exit %d\n--- stdout\n%s\n--- stderr\n%s", result.exit, result.stdout, result.stderr)
		if strings.HasPrefix(name, "status/") {
			text = sortStatusAgentsHuman(text)
		}
		if name == "get/sprint-elapsed.txt" {
			text = a7Masks.scalar.ReplaceAllString(text, "<REL>")
		}
		record = []byte(maskHuman(text))
	}
	e.norm.Check(t, kind, name, record)
}

type invocation struct {
	name string
	kind oracle.Kind
	args []string
}

func checkInvocations(t *testing.T, surface string, cases []invocation) {
	t.Helper()
	e := newEnvironment(t)
	project := materialiseFixture(t, e)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkResult(t, e, tc.kind, surface+"/"+tc.name, runCLI(t, e, project, nil, tc.args...))
		})
	}
}

func TestA7ClockMasks(t *testing.T) {
	skipUnlessLinux(t)
	cases := []struct{ name, input, want string }{
		{"empty task cell", "ID           Task    Heartbeat     Process\narchitect-1          10d 20h 30m   stopped\n", "ID           Task    Heartbeat     Process\narchitect-1    <REL>  stopped\n"},
		{"anomaly age", "AGE          TYPE     TASK\n10d 20h 30m  blocked  task-blocked\n", "AGE          TYPE     TASK\n<REL>  blocked  task-blocked\n"},
		{"yaml heartbeat", "    timesinceheartbeat: -10d 20h 30m\n    currenttask: task-review\n", "    timesinceheartbeat: <REL>\n    currenttask: task-review\n"},
		{"watch stall", "STALLED: no task progress for 1234 minutes — 2 tasks waiting", "STALLED: no task progress for <REL> minutes — 2 tasks waiting"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maskHuman(tc.input); got != tc.want {
				t.Fatalf("clock normalization differs:\ngot  %q\nwant %q", got, tc.want)
			}
		})
	}
}
