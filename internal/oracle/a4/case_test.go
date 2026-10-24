package a4

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type oracleCase struct {
	name, fixture, identity string
	args                    []string
	json                    bool
	helper, summary         string
	wantExit                int
	contains                string
}

// runCase is the shared consumer boundary. J/H modes always get fresh repositories.
func runCase(t *testing.T, c oracleCase) {
	t.Helper()
	gate(t)
	var jsonState []byte
	modes := []string{"human"}
	if c.json {
		modes = []string{"json", "human"}
	}
	if c.helper != "" {
		modes = []string{"helper"}
	}
	for _, mode := range modes {
		f := newFixture(t, c.fixture)
		before := readFile(t, f.statePath())
		snapshot := projectFiles(t, f.root)
		args := append([]string(nil), c.args...)
		for i, arg := range args {
			if strings.HasPrefix(arg, "@input:") {
				args[i] = f.input(t, strings.TrimPrefix(arg, "@input:"))
			}
			if strings.HasPrefix(arg, "@transition:") {
				id, err := transitionID(readFile(t, f.statePath()), strings.TrimPrefix(arg, "@transition:"))
				if err != nil {
					t.Fatal(err)
				}
				args[i] = id
			}
		}
		var res runResult
		if mode == "helper" {
			res = f.helper(t, c.helper, "op")
			if strings.TrimSpace(string(res.stdout)) != c.summary {
				t.Fatalf("helper summary: %s; %s", res.stdout, res.stderr)
			}
		} else {
			if mode == "json" {
				args = append(args, "--json")
			}
			res = f.cli(t, c.identity, args...)
		}
		res.archive = strings.HasPrefix(c.name, "archive-terminal-tasks/")
		if res.code != c.wantExit {
			t.Fatalf("%s exit %d, want %d: %s %s", mode, res.code, c.wantExit, f.normaliser().text(string(res.stdout)), f.normaliser().text(string(res.stderr)))
		}
		if c.contains != "" && !strings.Contains(string(res.stdout)+string(res.stderr), c.contains) {
			t.Fatalf("%s did not reach named outcome %q: %s %s", mode, c.contains, res.stdout, res.stderr)
		}
		if mode == "json" {
			var envelope struct {
				OK bool `json:"ok"`
			}
			if err := json.Unmarshal(res.stdout, &envelope); err != nil || envelope.OK != (c.wantExit == 0) {
				t.Fatal("JSON envelope outcome does not match the named success/refusal")
			}
			f.harness().Check(t, oracle.JSON, c.name+".json", jsonDocument(t, f, res))
		}
		if mode == "human" {
			f.harness().Check(t, oracle.Human, c.name+".txt", humanDocument(t, f, res))
		}
		b, a := stateDocument(t, f, before, snapshot)
		if mode == "human" && c.json {
			if !bytes.Equal(jsonState, a) {
				t.Fatalf("json/human final state differs for %s:\njson:\n%s\nhuman:\n%s", c.name, jsonState, a)
			}
		} else {
			f.harness().CheckState(t, c.name+".txt", b, a)
			jsonState = a
		}
	}
}

var logLineRE = regexp.MustCompile(`(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)? [^\n]*(?:\n|$)`)

func jsonDocument(t testing.TB, f *fixture, r runResult) []byte {
	t.Helper()
	n := f.normaliser()
	var raw string
	if r.archive {
		raw = n.archiveText(string(r.stdout))
	} else {
		raw = n.text(string(r.stdout))
	}
	var stdout json.RawMessage = []byte(raw)
	if !json.Valid(stdout) {
		t.Fatal("case stdout is not JSON")
	}
	wrapped, err := json.Marshal(struct {
		ExitCode int             `json:"exit_code"`
		Stdout   json.RawMessage `json:"stdout"`
	}{r.code, stdout})
	if err != nil {
		t.Fatal(err)
	}
	form, err := f.harness().Form(oracle.JSON, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	return form
}

func humanDocument(t testing.TB, f *fixture, r runResult) []byte {
	t.Helper()
	s := fmt.Sprintf("exit_code: %d\n--- stdout\n%s\n--- stderr\n%s", r.code, logLineRE.ReplaceAll(r.stdout, nil), logLineRE.ReplaceAll(r.stderr, nil))
	n := f.normaliser()
	if r.archive {
		s = n.archiveText(s)
	} else {
		s = n.text(s)
	}
	form, err := f.harness().Form(oracle.Human, []byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return form
}

// projectFiles snapshots only files covered by D7. Git metadata and runtime logs
// are outside the frozen surface, including .git files in nested worktrees.
func projectFiles(t testing.TB, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := entry.Name()
		if rel == ".liza/state.yaml" || name == "log.yaml" || strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".lock") || strings.HasSuffix(name, ".lock.owner.json") {
			return nil
		}
		files[rel] = readFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func stateDocument(t testing.TB, f *fixture, before []byte, snapshot map[string][]byte) ([]byte, []byte) {
	t.Helper()
	n := f.normaliser()
	b := []byte(n.text(string(before)))
	a := n.text(string(readFile(t, f.statePath())))
	files := projectFiles(t, f.root)
	var paths []string
	for path, content := range files {
		if !bytes.Equal(snapshot[path], content) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	type written struct{ path, content string }
	formed := make([]written, 0, len(paths))
	// Tokenise in document order before invoking any harness form.
	for _, path := range paths {
		formed = append(formed, written{n.text(path), n.text(string(files[path]))})
	}
	sort.Slice(formed, func(i, j int) bool { return formed[i].path < formed[j].path })
	a = strings.TrimRight(a, "\n") + "\n# written files\n"
	for _, file := range formed {
		kind := oracle.Human
		switch filepath.Ext(file.path) {
		case ".json":
			kind = oracle.JSON
		case ".yaml", ".yml":
			kind = oracle.State
		}
		content := []byte(file.content)
		if kind != oracle.State {
			var err error
			content, err = f.harness().Form(kind, content)
			if err != nil {
				t.Fatalf("written file %s: %v", file.path, err)
			}
		}
		a += "## " + file.path + "\n" + strings.TrimRight(string(content), "\n") + "\n"
	}
	return b, []byte(a)
}
