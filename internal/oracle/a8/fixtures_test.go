package a8

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type fixture struct {
	ID          string   `json:"id"`
	Role        string   `json:"role"`
	Pair        string   `json:"role_pair"`
	AgentID     string   `json:"agent_id"`
	TaskID      string   `json:"task_id"`
	Wake        string   `json:"expected_wake_trigger"`
	Gates       bool     `json:"gates"`
	InitialTask string   `json:"initial_task"`
	Markers     []string `json:"markers"`
	Commits     []string `json:"commits"`
	Branch      string   `json:"integration_branch"`
}

// Independent allocation: a manifest cannot silently trade a role/pair for
// another reachable path while retaining the inventory's count.
var inventory = [][2]string{
	{"epic-planner", "epic-planning-main-pair"}, {"epic-plan-reviewer", "epic-planning-main-pair"},
	{"epic-planner", "epic-planning-pair"}, {"epic-plan-reviewer", "epic-planning-pair"},
	{"us-writer", "us-writing-pair"}, {"us-reviewer", "us-writing-pair"},
	{"architect", "architecture-main-pair"}, {"architecture-reviewer", "architecture-main-pair"},
	{"architect", "architecture-pair"}, {"architecture-reviewer", "architecture-pair"},
	{"code-planner", "code-planning-main-pair"}, {"code-plan-reviewer", "code-planning-main-pair"},
	{"code-planner", "code-planning-pair"}, {"code-plan-reviewer", "code-planning-pair"},
	{"coder", "coding-pair"}, {"code-reviewer", "coding-pair"},
	{"integration-analyst", "slice-integration-pair"}, {"integration-reviewer", "slice-integration-pair"},
	{"integration-analyst", "integration-pair"}, {"integration-reviewer", "integration-pair"},
}

var wakeInventory = []string{"INITIAL_PLANNING", "BLOCKED_TASKS", "HYPOTHESIS_EXHAUSTED", "IMMEDIATE_DISCOVERY", "HUMAN_NOTE", "PLANNING_COMPLETE", "MANY_TO_ONE_READY", "CODING_COMPLETE", "SPRINT_COMPLETE", "INTEGRATION_WAITING"}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	entries, err := os.ReadDir("testdata/fixtures")
	if err != nil || len(entries) != 30 {
		t.Fatal("fixture inventory must contain exactly 30 case directories")
	}
	var fixtures []fixture
	for i, entry := range entries {
		wantID, wantRole, wantPair, wantWake := "", "", "", ""
		if i < 20 {
			wantID, wantRole, wantPair = fmt.Sprintf("T%02d", i+1), inventory[i][0], inventory[i][1]
		} else {
			wantID, wantRole, wantWake = fmt.Sprintf("W%02d", i-19), "orchestrator", wakeInventory[i-20]
		}
		if !entry.IsDir() || entry.Name() != wantID {
			t.Fatalf("inventory position %d must be %s", i, wantID)
		}
		var f fixture
		decoder := json.NewDecoder(strings.NewReader(string(readFile(t, filepath.Join("testdata/fixtures", wantID, "manifest.json")))))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&f); err != nil {
			t.Fatalf("%s manifest: %v", wantID, err)
		}
		if f.ID != wantID || f.Role != wantRole || f.Pair != wantPair || f.Wake != wantWake {
			t.Fatalf("%s: role/pair/wake allocation differs from inventory", wantID)
		}
		if f.AgentID == "" || f.Branch == "" || len(f.Commits) < 2 || len(f.Markers) == 0 {
			t.Fatalf("%s: missing required manifest fields", wantID)
		}
		if (f.TaskID == "") != (i >= 20) || f.Gates != (wantID == "T15") || (f.InitialTask != "") != (wantID == "T15") {
			t.Fatalf("%s: invalid task/gate/resume declaration", wantID)
		}
		for _, marker := range f.Markers {
			if strings.TrimSpace(marker) == "" {
				t.Fatalf("%s: empty branch marker", wantID)
			}
		}
		fixtures = append(fixtures, f)
	}
	return fixtures
}

func renderFixture(t *testing.T, f fixture) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("resolve case root")
	}
	project := filepath.Join(root, "project")
	home := filepath.Join(root, "home")
	for _, path := range []string{project, home} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal("create case directory")
		}
	}
	global := filepath.Join(home, "gitconfig")
	writeFile(t, global, nil, 0o644)
	gitSettings := []string{"GIT_CONFIG_GLOBAL=" + global, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=A8 Oracle", "GIT_AUTHOR_EMAIL=a8@example.invalid", "GIT_COMMITTER_NAME=A8 Oracle", "GIT_COMMITTER_EMAIL=a8@example.invalid", "GIT_AUTHOR_DATE=2026-01-02T03:04:05Z", "GIT_COMMITTER_DATE=2026-01-02T03:04:05Z"}
	for _, setting := range append([]string{"HOME=" + home}, gitSettings...) {
		name, value, _ := strings.Cut(setting, "=")
		t.Setenv(name, value)
	}
	// Semble's readiness probe owns a temporary fixture and corpus lock too.
	// Keep both inside this case so cleanup includes those runtime files.
	t.Setenv("TMPDIR", root)
	for _, name := range []string{"LIZA_ENABLE_SCIP_SEARCH", "LIZA_ENABLE_STACKLIT", "LIZA_ENABLE_FUNCTIONAL_CLUSTERS", "LIZA_ENABLE_SEMBLE"} {
		value := "0"
		if f.Gates {
			value = "1"
		}
		t.Setenv(name, value)
	}
	if f.Gates {
		bin := filepath.Join(root, "bin")
		writeFile(t, filepath.Join(bin, "semble"), []byte("#!/bin/sh\nprintf 'A8 prepared corpus ready\\n'\n"), 0o755)
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	env := childEnvironment(t, oracle.EnvBase{Home: home, Path: os.Getenv("PATH"), Tmp: root}, gitSettings...)
	gitCommand(t, project, env, "init", "--quiet", "--initial-branch="+f.Branch)
	gitCommand(t, project, env, "config", "core.autocrlf", "false")
	gitCommand(t, project, env, "config", "commit.gpgsign", "false")
	labels := map[string]string{"<ROOT>": root}
	for _, label := range f.Commits {
		copyTree(t, filepath.Join("testdata/fixtures", f.ID, "trees", label), project, labels)
		gitCommand(t, project, env, "add", "--", ".")
		gitCommand(t, project, env, "commit", "--quiet", "--allow-empty", "-m", label)
		labels["<"+label+">"] = strings.TrimSpace(gitCommand(t, project, env, "rev-parse", "HEAD"))
	}
	state := substitute(string(readFile(t, filepath.Join("testdata/fixtures", f.ID, "state.yaml"))), labels)
	if strings.Contains(state, "<base>") || strings.Contains(state, "<review>") {
		t.Fatalf("%s: unresolved commit label", f.ID)
	}
	prompt, err := renderPrompt(project, state, f)
	if err != nil {
		t.Fatalf("%s %s: render: %v", f.ID, goldenPath(f.ID), err)
	}
	return oracle.ReplaceRoot(prompt, root, "<ROOT>")
}

func substitute(text string, labels map[string]string) string {
	// Labels are disjoint and replacement values contain no label tokens.
	for label, value := range labels {
		text = strings.ReplaceAll(text, label, value)
	}
	return text
}

func copyTree(t *testing.T, source, target string, labels map[string]string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		// Strict-carrier fixtures contain commit-label placeholders until this
		// copy. Store them as templates so admission cannot mistake them for
		// reviewed project carriers before those labels have been resolved.
		rel = strings.TrimSuffix(rel, ".in")
		writeFile(t, filepath.Join(target, rel), []byte(substitute(string(readFile(t, path)), labels)), 0o644)
		return nil
	})
	if err != nil {
		t.Fatal("copy fixture tree")
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s", path)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal("create fixture parent")
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal("write fixture file")
	}
}

func gitCommand(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = dir, env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %s failed", args[0])
	}
	return string(output)
}

func childEnvironment(t *testing.T, base oracle.EnvBase, extra ...string) []string {
	t.Helper()
	env, err := oracle.ChildEnv(base, extra...)
	if err != nil {
		t.Fatal("construct explicit child environment")
	}
	return env
}
