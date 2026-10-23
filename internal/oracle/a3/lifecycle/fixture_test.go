package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type fixture struct {
	environment
	commits    map[string]string
	before     string
	gitBefore  gitSnapshot
	targetDead bool
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", filepath.Base(path), err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return data
}

func (r environment) git(t *testing.T, args ...string) string {
	t.Helper()
	res := r.command(t, filepath.Join(r.path, "git"), args...)
	if res.exit != 0 {
		t.Fatalf("fixture git %s: exit %d: %s", args[0], res.exit, res.stderr)
	}
	return strings.TrimSpace(res.stdout)
}

func buildFixture(t *testing.T, bin, name, caller string) fixture {
	t.Helper()
	f := fixture{environment: newEnvironment(t, caller), commits: map[string]string{}}
	f.git(t, "init", "-b", "main")
	writeFile(t, filepath.Join(f.project, "README"), []byte("Lifecycle oracle fixture\n"))
	writeFile(t, filepath.Join(f.project, "specs", "vision.md"), []byte("# Lifecycle oracle\n"))
	writeFile(t, filepath.Join(f.project, ".liza", "pipeline.yaml"), readFile(t, "testdata/fixtures/pipeline.yaml"))
	f.git(t, "add", "README", "specs/vision.md", ".liza/pipeline.yaml")
	f.git(t, "commit", "-m", "base")
	f.commits["base"] = f.git(t, "rev-parse", "HEAD")
	f.git(t, "branch", "integration")
	state := string(readFile(t, filepath.Join("testdata/fixtures/state", name+".yaml")))
	f.targetDead = strings.Contains(state, "{{DEAD_PID}}")
	// Quote PID tokens in the source so fixtures are valid YAML before
	// rendering; replace the quoted token with a numeric YAML scalar.
	state = strings.NewReplacer("'{{LIVE_PID}}'", strconv.Itoa(os.Getpid()), "'{{DEAD_PID}}'", strconv.Itoa(deadPID), "{{COMMIT:base}}", f.commits["base"]).Replace(state)
	if strings.Contains(state, "{{") {
		t.Fatalf("fixture %s has unfilled tokens", name)
	}
	statePath := filepath.Join(f.project, ".liza", "state.yaml")
	writeFile(t, statePath, []byte(state))
	f.git(t, "add", ".liza/state.yaml")
	f.git(t, "commit", "-m", "state")
	f.commits["state"] = f.git(t, "rev-parse", "HEAD")
	if strings.Contains(state, "worktree: .worktrees/task-1") {
		f.git(t, "worktree", "add", "-b", "task/task-1", ".worktrees/task-1", f.commits["base"])
	}
	// Validate with the actual CLI for every repository, including every mode.
	// A dead PID is structural fixture data; validate itself must not repair it.
	result := f.command(t, bin, "validate", "--json")
	var doc struct {
		OK     bool `json:"ok"`
		Result struct {
			Valid bool `json:"valid"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &doc); err != nil || result.exit != 0 || !doc.OK || !doc.Result.Valid {
		t.Fatalf("fixture %s setup validate failed (exit %d): %s %s", name, result.exit, result.stdout, result.stderr)
	}
	if !bytes.Equal(readFile(t, statePath), []byte(state)) {
		t.Fatalf("fixture %s setup validate changed state.yaml", name)
	}
	f.before = state
	f.gitBefore = f.snapshot(t)
	return f
}

type gitSnapshot struct{ refs, worktrees map[string]string }

func (f fixture) snapshot(t *testing.T) gitSnapshot {
	t.Helper()
	s := gitSnapshot{refs: map[string]string{}, worktrees: map[string]string{}}
	for _, line := range strings.Split(f.git(t, "for-each-ref", "--format=%(refname) %(objectname)"), "\n") {
		name, sha, ok := strings.Cut(line, " ")
		if ok {
			s.refs[name] = sha
		}
	}
	var path string
	for _, line := range strings.Split(f.git(t, "worktree", "list", "--porcelain"), "\n") {
		if value, ok := strings.CutPrefix(line, "worktree "); ok {
			rel, err := filepath.Rel(f.project, value)
			if err != nil {
				t.Fatal("relative worktree:", err)
			}
			path = filepath.ToSlash(rel)
			s.worktrees[path] = "detached"
		}
		if value, ok := strings.CutPrefix(line, "branch "); ok {
			s.worktrees[path] = value
		}
	}
	return s
}

func gitEffects(before, after gitSnapshot) string {
	var lines []string
	for name, sha := range before.refs {
		if next, ok := after.refs[name]; !ok {
			lines = append(lines, fmt.Sprintf("ref - %s %s\n", name, sha))
		} else if next != sha {
			lines = append(lines, fmt.Sprintf("ref ~ %s %s -> %s\n", name, sha, next))
		}
	}
	for name, sha := range after.refs {
		if _, ok := before.refs[name]; !ok {
			lines = append(lines, fmt.Sprintf("ref + %s %s\n", name, sha))
		}
	}
	for path, branch := range before.worktrees {
		if after.worktrees[path] != branch {
			lines = append(lines, fmt.Sprintf("worktree - %s %s\n", path, branch))
		}
	}
	for path, branch := range after.worktrees {
		if before.worktrees[path] != branch {
			lines = append(lines, fmt.Sprintf("worktree + %s %s\n", path, branch))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "")
}
