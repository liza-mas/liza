package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupHumanNoteCLI(t *testing.T) (string, string) {
	t.Helper()
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("target", models.TaskStatusBlocked, time.Now().UTC())}
	testhelpers.WriteInitialState(t, statePath, state)
	notePath := filepath.Join(root, "operator-note.md")
	if err := os.WriteFile(notePath, []byte("Recovery guidance, not an approval.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root, notePath
}

func TestAddHumanNoteCLI(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonMode], func(t *testing.T) {
			root, notePath := setupHumanNoteCLI(t)
			args := []string{"add-human-note", "target", "--note-file", notePath}
			if jsonMode {
				args = append(args, "--json")
			}
			var stdout string
			var err error
			if jsonMode {
				stdout, err = executeRootCommandCapture(t, root, args...)
			} else {
				// The shared JSON helper deliberately discards Cobra output.
				var output bytes.Buffer
				rootCmd.SetOut(&output)
				rootCmd.SetArgs(append([]string{"-C", root}, args...))
				err = rootCmd.Execute()
				stdout = output.String()
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stdout, "Recovery guidance") {
				t.Fatal("note content echoed")
			}
			if jsonMode {
				envelope := parseEnvelope(t, stdout)
				if envelope["ok"] != true {
					t.Fatalf("missing success envelope: %s", stdout)
				}
				result := envelope["result"].(map[string]any)
				if result["target"] != "target" || result["timestamp"] == "" {
					t.Fatalf("missing acknowledgement: %v", result)
				}
			} else if !strings.Contains(stdout, "Operator note recorded for target") {
				t.Fatalf("missing acknowledgement: %s", stdout)
			}
			state, err := db.For(paths.New(root).StatePath()).Read()
			if err != nil {
				t.Fatal(err)
			}
			if len(state.HumanNotes) != 1 || state.HumanNotes[0].Message != "Recovery guidance, not an approval.\n" || state.Tasks[0].Status != models.TaskStatusBlocked {
				t.Fatal("note absent or task changed")
			}
		})
	}
}

func TestAddHumanNoteCLIInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name, note, target, want string
		missingFile              bool
	}{
		{name: "missing target", note: "input", target: "missing", want: "not found"},
		{name: "blank target", note: "input", target: " ", want: "target is required"},
		{name: "blank input", note: "\n ", target: "target", want: "must not be empty"},
		{name: "oversize", note: strings.Repeat("x", statehygiene.MaxStateTextBytes+1), target: "target", want: "exceeds"},
		{name: "missing file", target: "target", missingFile: true, want: "opening --note-file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, notePath := setupHumanNoteCLI(t)
			if tc.missingFile {
				notePath += ".missing"
			} else if err := os.WriteFile(notePath, []byte(tc.note), 0600); err != nil {
				t.Fatal(err)
			}
			statePath := paths.New(root).StatePath()
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := executeRootCommandCapture(t, root, "add-human-note", tc.target, "--note-file", notePath, "--json")
			if err == nil || !strings.Contains(stdout, tc.want) {
				t.Fatalf("error=%v output=%s; want %s", err, stdout, tc.want)
			}
			if parseEnvelope(t, stdout)["ok"] != false {
				t.Fatal("missing failure envelope")
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("invalid command changed state")
			}
		})
	}
}

func TestAddHumanNoteCLIRejectsAgentIdentity(t *testing.T) {
	for _, name := range []string{brand.EnvName("AGENT_ID"), brand.LegacyEnvName("AGENT_ID")} {
		t.Run(name, func(t *testing.T) {
			root, _ := setupHumanNoteCLI(t)
			t.Setenv(name, "orchestrator-1")
			before, err := os.ReadFile(paths.New(root).StatePath())
			if err != nil {
				t.Fatal(err)
			}
			// A missing file establishes that refusal precedes file access.
			stdout, err := executeRootCommandCapture(t, root, "add-human-note", "all", "--note-file", "missing", "--json")
			if err == nil || !strings.Contains(stdout, "operator-only") {
				t.Fatalf("agent accepted: %v, %s", err, stdout)
			}
			if parseEnvelope(t, stdout)["ok"] != false {
				t.Fatal("missing refusal envelope")
			}
			after, err := os.ReadFile(paths.New(root).StatePath())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("agent refusal changed state")
			}
		})
	}
}

func TestAddHumanNoteCLIRequiresTargetAndFile(t *testing.T) {
	for _, args := range [][]string{{"add-human-note"}, {"add-human-note", "target"}} {
		root, _ := setupHumanNoteCLI(t)
		if _, err := executeRootCommandCapture(t, root, args...); err == nil {
			t.Fatalf("accepted missing argument/flag: %v", args)
		}
	}
}
