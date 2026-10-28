package a4

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
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

func init() {
	helperOps["environment"] = func(string) (string, error) {
		var names []string
		for _, pair := range os.Environ() {
			name, _, _ := strings.Cut(pair, "=")
			names = append(names, name)
		}
		sort.Strings(names)
		b, err := json.Marshal(names)
		return string(b), err
	}
}

func TestA4Environment(t *testing.T) {
	gate(t)
	t.Setenv("A4_HOST_ONLY", "excluded")
	for _, identity := range []string{"op", "orch", "arch"} {
		t.Run(identity, func(t *testing.T) {
			f := newFixture(t, "base")
			entries, err := os.ReadDir(f.home)
			if err != nil || len(entries) != 0 {
				t.Fatal("HOME must start empty")
			}
			res := f.helper(t, "environment", identity)
			var names []string
			if err := json.Unmarshal(res.stdout, &names); err != nil {
				t.Fatal("environment probe did not return variable names")
			}
			want := []string{"GIT_CONFIG_NOSYSTEM", "HOME", "PATH", "TMPDIR", "TZ"}
			if identity != "op" {
				want = append(want, "LIZA_AGENT_ID", "LIZA_AGENT_GENERATION")
			}
			sort.Strings(want)
			if res.code != 0 || !reflect.DeepEqual(names, want) {
				t.Fatalf("child environment names: got %v, want %v", names, want)
			}
		})
	}
}

func TestA4PlatformGate(t *testing.T) {
	load := func(string) (*time.Location, error) { return nil, errors.New("missing") }
	skip, fail := platformGate("windows", load)
	if !strings.Contains(skip, "D3 platform gate") || fail != "" {
		t.Fatal("Windows must skip at D3 platform gate")
	}
	skip, fail = platformGate("linux", load)
	if skip != "" || !strings.Contains(fail, "Etc/GMT-2") {
		t.Fatal("Unix missing zone must fail naming Etc/GMT-2")
	}
	skip, fail = platformGate("darwin", time.LoadLocation)
	if skip != "" || fail != "" {
		t.Fatal("available Unix zone must proceed")
	}
}

func TestA4FixturesValidate(t *testing.T) {
	gate(t)
	files, err := filepath.Glob("testdata/fixtures/state/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatal("fixture catalogue is empty")
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		if name == d35Fixture {
			continue // D35's one deliberate defect is pinned by the D4 case.
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, name)
			before := readFile(t, f.statePath())
			r := f.cli(t, "op", "validate", "--json")
			var envelope struct {
				OK     bool `json:"ok"`
				Result struct {
					Valid bool `json:"valid"`
				} `json:"result"`
			}
			if err := json.Unmarshal(r.stdout, &envelope); err != nil || r.code != 0 || !envelope.OK || !envelope.Result.Valid {
				t.Fatalf("fixture %s invalid: exit %d; %s", name, r.code, f.normaliser().text(string(r.stdout)+string(r.stderr)))
			}
			if !bytes.Equal(before, readFile(t, f.statePath())) {
				t.Fatal("validate changed fixture state")
			}
		})
	}
}

func TestA4FixtureBuilder(t *testing.T) {
	gate(t)
	f := newFixture(t, "architecting")
	state := string(readFile(t, f.statePath()))
	if strings.Contains(state, "{{") || !strings.Contains(state, f.commits["base"]) {
		t.Fatal("fixture commit tokens not filled")
	}
	if f.git(t, "status", "--porcelain") != "" {
		t.Fatal("fixture must be committed")
	}
	if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "arch-1", "specs", "vision.md")); err != nil {
		t.Fatal("declared worktree not built")
	}
	if f.git(t, "rev-parse", "integration") != f.commits["base"] {
		t.Fatal("integration must point to base")
	}
	if _, err := renderState("{{COMMIT:missing}}", f.commits); err == nil || !strings.Contains(err.Error(), "unfilled") {
		t.Fatal("unknown token must be refused")
	}
	recipe := []gitStep{{Kind: "branch", Label: "task/recipe"}, {Kind: "commit", Label: "plan", Path: "recipe.md", Source: "testdata/fixtures/project/specs/vision.md"}, {Kind: "merge", Label: "merge-cp-1", Branch: "task/recipe"}}
	r := newFixtureWithRecipe(t, "base", recipe)
	if r.commits["plan"] == "" || r.commits["merge-cp-1"] == "" || r.commits["plan"] == r.commits["merge-cp-1"] {
		t.Fatal("recipe must record distinct commit and merge labels")
	}
	if r.git(t, "branch", "--show-current") != "main" {
		t.Fatal("recipe must return to main")
	}
}

func TestA4GoldenForms(t *testing.T) {
	gate(t)
	f := newFixture(t, "base")
	before := readFile(t, f.statePath())
	snapshot := projectFiles(t, f.root)
	sha := strings.Repeat("a", 40)
	stamp := "2020-01-01T00:00:00Z 2020-01-01T02:00:00+02:00"
	writeFile(t, f.statePath(), append(before, []byte("proof: "+sha+"\n")...))
	writeFile(t, filepath.Join(f.root, "proof.json"), []byte(`{"time":"`+stamp+`","sha":"`+sha+`"}`))
	writeFile(t, filepath.Join(f.root, "proof.md"), []byte(stamp+" "+sha+"\n"))
	for _, name := range []string{".git/ignored", ".liza/log.yaml", "ignored.log", "ignored.lock", "ignored.lock.owner.json"} {
		writeFile(t, filepath.Join(f.root, name), []byte("must not appear"))
	}
	b, a := stateDocument(t, f, before, snapshot)
	form := f.harness().StateForm(b, a)
	for _, want := range []string{"## proof.json", "## proof.md", "<INSTANT Z>", "<INSTANT +02:00>", "<SHA 1>"} {
		if !bytes.Contains(form, []byte(want)) {
			t.Fatalf("complete state manifest lost %s", want)
		}
	}
	if bytes.Contains(form, []byte("must not appear")) || bytes.Count(form, []byte("<SHA 1>")) != 3 {
		t.Fatal("manifest exclusions or shared state/file SHA mapping failed")
	}
	for _, section := range []string{"## proof.json", "## proof.md"} {
		part := strings.SplitN(string(form), section, 2)[1]
		part, _, _ = strings.Cut(part, "\n## ")
		if !strings.Contains(part, "<INSTANT Z>") || !strings.Contains(part, "<INSTANT +02:00>") {
			t.Fatal("written JSON/Markdown lost timestamp zone classes")
		}
	}
	r := runResult{code: 1, stdout: []byte(`{"ok":false}`), stderr: []byte("2020/01/01 00:00:00 log-only\nvisible\n")}
	j := jsonDocument(t, f, r)
	if !bytes.Contains(j, []byte(`"exit_code": 1`)) || !bytes.Contains(j, []byte(`"stdout": {`)) {
		t.Fatal("JSON wrapper lost exit code or parsed document")
	}
	h := humanDocument(t, f, r)
	if bytes.Contains(h, []byte("log-only")) || !bytes.Contains(h, []byte("visible")) || !bytes.Contains(h, []byte("exit_code: 1")) {
		t.Fatal("human transcript lost content or retained free log lines")
	}
	archiveJSON := jsonDocument(t, f, runResult{archive: true, stdout: []byte(`{"bytes":2010}`)})
	archiveHuman := humanDocument(t, f, runResult{archive: true, stdout: []byte("Archived tasks (2010 bytes)")})
	if !bytes.Contains(archiveJSON, []byte(`"bytes": "<BYTES>"`)) || !bytes.Contains(archiveHuman, []byte("(<BYTES> bytes)")) {
		t.Fatal("archive transcript byte counts must be tokenised")
	}
}

func TestA4ImportsAndSleep(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(file, "adapter_") || path == "github.com/liza-mas/liza/internal/testhelpers/oracle" {
				continue
			}
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") {
				t.Fatalf("driver %s imports non-stdlib %s", file, path)
			}
		}
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Sleep" {
				return true
			}
			for _, imp := range f.Imports {
				if imp.Path.Value == `"time"` {
					t.Errorf("no time.Sleep allowed: %s", file)
				}
			}
			return true
		})
	}
}

func TestA4HarnessFixedPoint(t *testing.T) {
	s := "<INSTANT Z> <INSTANT +02:00> <SHA base> <SHA 1> <DIGEST 1> <DIGEST 1:2> <DURATION> <BYTES> <FIXTURE>"
	if oracle.NewNormaliser().Text(s) != s {
		t.Fatal("A4 tokens are not harness fixed points")
	}
}
