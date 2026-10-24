package lifecycle

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestLifecycleEnvironment(t *testing.T) {
	linuxOnly(t)
	t.Setenv("LIFECYCLE_HOST_ONLY", "fixture-only")
	for _, caller := range []string{"", "coder-1"} {
		r := newEnvironment(t, caller)
		var names []string
		for _, pair := range r.env {
			name, _, ok := strings.Cut(pair, "=")
			if !ok {
				t.Fatal("environment pair missing separator")
			}
			names = append(names, name)
		}
		want := []string{"HOME", "PATH", "TMPDIR", "TZ", "LIZA_SKIP_AUTO_UPDATE", "LIZA_PROVIDER_CATALOG_URL", "GIT_CONFIG_NOSYSTEM", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_AUTHOR_DATE", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_DATE"}
		if caller != "" {
			want = append(want, "LIZA_AGENT_ID", "LIZA_AGENT_GENERATION")
		}
		sort.Strings(names)
		sort.Strings(want)
		if !reflect.DeepEqual(names, want) {
			t.Fatalf("environment names: got %v; want %v", names, want)
		}
		for _, dir := range []string{r.home, r.tmp, r.project} {
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("directory %s must start empty", filepath.Base(dir))
			}
		}
		entries, err := os.ReadDir(r.path)
		if err != nil || len(entries) != 1 || entries[0].Name() != "git" {
			t.Fatal("PATH must contain git only")
		}
		if _, err := os.Readlink(filepath.Join(r.path, "git")); err != nil {
			t.Fatal("git must be a symlink")
		}
	}
}

func TestLifecycleIdentityAndForceGuard(t *testing.T) {
	linuxOnly(t)
	if deadPID <= 4194304 {
		t.Fatal("dead PID must exceed Linux pid_max bound")
	}
	if forceAllowed([]string{"release-claim", "task-1", "--force"}, false) {
		t.Fatal("force against live fixture accepted")
	}
	if !forceAllowed([]string{"release-claim", "task-1", "--force"}, true) {
		t.Fatal("force against dead fixture refused")
	}
	if !forceAllowed([]string{"claim-task", "task-1", "coder-1"}, false) {
		t.Fatal("ordinary live caller refused")
	}
}

func TestLifecycleClockMask(t *testing.T) {
	start := time.Date(2026, 10, 23, 12, 0, 0, 600000000, time.UTC)
	m := newMask(start, nil, nil, 12345)
	for _, tc := range []struct{ in, want string }{
		{"2026-10-23T12:30:00.6Z", "<now+30m Z>"},
		{"2026-10-23T12:30:00Z", "<now+30m Z>"},
		{"2026-10-23T14:30:00+02:00", "<now+30m +02:00>"},
		{"2026-10-23T11:59:00Z", "<now-1m Z>"},
		{"2020-01-01T00:00:00Z", "<at 20200101T000000 Z>"},
		{"2099-01-01T00:00:00.123+02:00", "<at 20990101T000000.123 +02:00>"},
	} {
		if got := m.text(tc.in); got != tc.want {
			t.Errorf("clock: got %s; want %s", got, tc.want)
		}
	}
	if m.text("2026-10-23T12:00:00Z") == m.text("2026-10-23T14:00:00+02:00") {
		t.Fatal("UTC/local zone swap hidden")
	}
}

func TestLifecycleIdentifierMask(t *testing.T) {
	base := strings.Repeat("a", 40)
	stable := strings.Repeat("b", 64)
	x, y := strings.Repeat("c", 64), strings.Repeat("d", 64)
	m := newMask(time.Now(), map[string]string{"base": base}, nil, 12345)
	m.discover(`{"transition_id":"` + x + `","completed_transition_id":"` + x + `","instance_id":"` + y + `"}`)
	got := m.document(map[string]any{"z": x, "a": y, "same": x, "pid": json.Number("12345")}).(map[string]any)
	if got["a"] != "<id-1>" || got["same"] != "<id-2>" || got["z"] != "<id-2>" {
		t.Fatal("sorted traversal or equality masking lost")
	}
	if got["pid"] != "<live-pid>" {
		t.Fatal("JSON numeric PID not masked")
	}
	if m.text(base) != "<commit:base>" {
		t.Fatal("fixture commit name lost")
	}
	if m.text(stable) != "<hex:"+strings.TrimSuffix(strings.Repeat("bbbbbbbb.", 8), ".")+">" {
		t.Fatal("stable hex digits changed")
	}
	if m.text("PID 12345 PID 912345 PID 123456 12345") != "PID <live-pid> PID 912345 PID 123456 12345" {
		t.Fatal("PID masking must use labelled whole words")
	}
	textMask := newMask(time.Now(), nil, nil, 12345)
	textMask.discover(`key_id: 0123456789abcdef`, `transition_id: `+x)
	if textMask.text(x+" 0123456789abcdef "+x) != "<id-1> <id-2> <id-1>" {
		t.Fatal("text first appearance or 16-hex runtime-key masking lost")
	}
}

func TestLifecyclePathMask(t *testing.T) {
	m := newMask(time.Now(), nil, map[string]string{"/scratch/project": "<PROJECT>", "/scratch/project/home": "<HOME>", "/scratch/tmp": "<TMP>"}, 12345)
	if got := m.text("/scratch/project/home/key /scratch/project/file /scratch/tmp/input"); got != "<HOME>/key <PROJECT>/file <TMP>/input" {
		t.Fatal("path replacement lost longest-first directory identities")
	}
}

func TestLifecyclePIDMask(t *testing.T) {
	// D38: PID 60 collides with the fixed heartbeat interval. Only PID
	// fields and the starting CLI's labelled recovery messages are volatile.
	m := newMask(time.Now(), nil, nil, 60)
	t.Run("state", func(t *testing.T) {
		input := "agents:\n  coder-1:\n    pid: 60\n  dead:\n    pid: 99999999\nconfig:\n  heartbeat_interval: 60\nvalue: 60\n"
		want := "agents:\n  coder-1:\n    pid: <live-pid>\n  dead:\n    pid: 99999999\nconfig:\n  heartbeat_interval: 60\nvalue: 60\n"
		if m.text(input) != want {
			t.Fatal("PID field normalization changed fixed state values")
		}
	})
	t.Run("human", func(t *testing.T) {
		input := "coder agent coder-1 (PID 60) still running; agent coder-1 is still running with PID 60, use --force to recover\n60 160 600 heartbeat_interval: 60 PID 600\n"
		want := "coder agent coder-1 (PID <live-pid>) still running; agent coder-1 is still running with PID <live-pid>, use --force to recover\n60 160 600 heartbeat_interval: 60 PID 600\n"
		if m.text(input) != want {
			t.Fatal("labelled PID normalization changed unlabelled or longer numbers")
		}
	})
	t.Run("json", func(t *testing.T) {
		input := map[string]any{
			"agent":              map[string]any{"pid": json.Number("60")},
			"dead":               map[string]any{"pid": json.Number("99999999")},
			"heartbeat_interval": json.Number("60"),
			"values":             []any{json.Number("60"), "60", "heartbeat_interval: 60"},
			"message":            "coder agent coder-1 (PID 60) still running",
		}
		want := map[string]any{
			"agent":              map[string]any{"pid": "<live-pid>"},
			"dead":               map[string]any{"pid": json.Number("99999999")},
			"heartbeat_interval": json.Number("60"),
			"values":             []any{json.Number("60"), "60", "heartbeat_interval: 60"},
			"message":            "coder agent coder-1 (PID <live-pid>) still running",
		}
		if !reflect.DeepEqual(m.document(input), want) {
			t.Fatal("JSON PID normalization changed a fixed number or string")
		}
	})
}

func TestLifecycleRuntimeIdentityMask(t *testing.T) {
	base := strings.Repeat("e", 40)
	stable := strings.Repeat("b", 64)
	unknown := strings.Repeat("d", 64)
	hexToken := func(value string) string {
		var chunks []string
		for i := 0; i < len(value); i += 8 {
			chunks = append(chunks, value[i:min(i+8, len(value))])
		}
		return "<hex:" + strings.Join(chunks, ".") + ">"
	}
	for _, mode := range []string{"json", "human", "named-json-only"} {
		t.Run(mode, func(t *testing.T) {
			// Different runs have different keyed identities. Only JSON names
			// instance_id; human mode must discover it from the state ledger.
			id := strings.Repeat("a", 64)
			if mode == "human" {
				id = strings.Repeat("c", 64)
			}
			for _, indent := range []string{"  ", "    "} {
				ledger := "runtime_inputs:\n" + indent + id + ":\n" + indent + "  key_id: 0123456789abcdef\n"
				m := newMask(time.Now(), map[string]string{"base": base}, nil, 12345)
				if mode != "named-json-only" {
					m.discover(ledger)
				}
				if mode != "human" {
					m.discover(`{"instance_id":"` + id + `"}`)
				}
				// Prefix first must allocate the same token as the full map key.
				if got := m.text(id[:16] + " " + id + " " + id[:16]); got != "<id-1> <id-1> <id-1>" {
					t.Fatal("runtime full identity and displayed prefix lost equality")
				}
				m.discover(ledger) // Also register the key_id for the state check.
				want := "runtime_inputs:\n" + indent + "<id-1>:\n" + indent + "  key_id: <id-2>\n"
				if m.text(ledger) != want {
					t.Fatal("runtime ledger masking depends on output mode")
				}
				if m.text(base+" "+stable+" "+unknown+" "+unknown[:16]) != "<commit:base> "+hexToken(stable)+" "+hexToken(unknown)+" "+unknown[:16] {
					t.Fatal("runtime masking changed a fixture commit, stable digest or undeclared identifier")
				}
				fullFirst := newMask(time.Now(), nil, nil, 12345)
				fullFirst.discover(ledger)
				if fullFirst.text(id+" "+id[:16]) != "<id-1> <id-1>" {
					t.Fatal("runtime prefix allocated a second token after full identity")
				}
			}
		})
	}
	// A hex-shaped key outside the root ledger, including task declarations,
	// is not evidence of a volatile runtime instance.
	for _, source := range []string{
		"other:\n  " + unknown + ":\n    state: available\n",
		"tasks:\n  - runtime_inputs:\n      " + unknown + ":\n        state: available\n",
		"runtime_inputs:\n  " + strings.Repeat("a", 64) + ":\n    metadata:\n      " + unknown + ": value\nother:\n  " + stable + ":\n",
	} {
		m := newMask(time.Now(), nil, nil, 12345)
		m.discover(source)
		if m.text(unknown+" "+stable) != hexToken(unknown)+" "+hexToken(stable) {
			t.Fatal("non-ledger key declared volatile")
		}
	}
}

func TestLifecycleHarnessFixedPoint(t *testing.T) {
	n := oracle.NewNormaliser()
	for _, value := range []string{"<now+30m Z>", "<now-1m +02:00>", "<at 20200101T000000 Z>", "<at 20990101T000000.123 +02:00>", "<commit:base>", "<id-1>", "<hex:01234567.89abcdef.01234567.89abcdef.01234567>", "<live-pid>", "<PROJECT>", "<HOME>", "<TMP>"} {
		if n.Text(value) != value {
			t.Fatalf("harness changed mask token %s", value)
		}
	}
}

func TestLifecycleLogRemoval(t *testing.T) {
	got := stripLogs("2026/10/23 12:34:56 free log\nkeep\n2026/10/23 12:34:57 another\n")
	if got != "keep\n" {
		t.Fatalf("log removal: %q", got)
	}
	if stripLogs("prefix 2026/10/23 12:34:56 keep\n") == "" {
		t.Fatal("non-log line removed")
	}
}

func TestLifecycleOutcomes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".liza", "lifecycle-metrics")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "one.json"), []byte(`{"version":1,"counts":{"claim-task":{"COMPLETED":2,"CONFLICT":0},"mark-blocked":{"ALREADY_COMPLETED":1}}}`))
	// Valid counter-shaped decoys ensure removing either exclusion fails the
	// count assertion, rather than merely causing an unrelated parse error.
	decoy := []byte(`{"counts":{"claim-task":{"COMPLETED":99}}}`)
	writeFile(t, filepath.Join(dir, "one.json.lock"), decoy)
	writeFile(t, filepath.Join(dir, "one.json.lock.owner.json"), decoy)
	if got := outcomeLines(t, root); got != "claim-task COMPLETED 2\nmark-blocked ALREADY_COMPLETED 1\n" {
		t.Fatalf("outcome reduction: %q", got)
	}
}

func TestLifecycleGitEffects(t *testing.T) {
	before := gitSnapshot{refs: map[string]string{"old": "a", "move": "a"}, worktrees: map[string]string{"gone": "refs/heads/old"}}
	after := gitSnapshot{refs: map[string]string{"new": "b", "move": "b"}, worktrees: map[string]string{"new": "refs/heads/new"}}
	want := "ref + new b\nref - old a\nref ~ move a -> b\nworktree + new refs/heads/new\nworktree - gone refs/heads/old\n"
	if got := gitEffects(before, after); got != want {
		t.Fatalf("Git effects: %q", got)
	}
}

func TestLifecycleImportsStdlibAndHarnessOnly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal("package source files missing")
	}
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			first, _, _ := strings.Cut(name, "/")
			if strings.Contains(first, ".") && name != "github.com/liza-mas/liza/internal/testhelpers/oracle" {
				t.Fatalf("%s: forbidden import %s", file, name)
			}
		}
	}
}
