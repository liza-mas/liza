package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const cliRepairArchRef = "specs/architecture/scope-arch.md#Scope 0: Reads"

func setupRepairArchRefCLI(t *testing.T) string {
	t.Helper()
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	testhelpers.MustGit(t, root, "checkout", "-q", "integration")
	if err := os.MkdirAll(root+"/specs/architecture", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/specs/architecture/scope-arch.md", []byte("# Architecture\n\n## Scope 0: Reads\n\nReads.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs")
	testhelpers.MustGit(t, root, "commit", "-q", "-m", "add architecture")
	testhelpers.MustGit(t, root, "checkout", "-q", "main")
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("cp", models.TaskStatusDraftCodingPlan, time.Now().UTC())
	task.RolePair = "code-planning-pair"
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)
	return root
}

func TestRepairArchRefCLI(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonMode], func(t *testing.T) {
			root := setupRepairArchRefCLI(t)
			args := []string{"repair-arch-ref", "cp", "--arch-ref", cliRepairArchRef, "--reason", "restore scope"}
			var stdout string
			var err error
			if jsonMode {
				stdout, err = executeRootCommandCapture(t, root, append(args, "--json")...)
			} else {
				var output bytes.Buffer
				rootCmd.SetOut(&output)
				rootCmd.SetArgs(append([]string{"-C", root}, args...))
				err = rootCmd.Execute()
				stdout = output.String()
			}
			if err != nil {
				t.Fatal(err)
			}
			if jsonMode {
				envelope := parseEnvelope(t, stdout)
				result, _ := envelope["result"].(map[string]any)
				if envelope["ok"] != true || result["task_id"] != "cp" || result["arch_ref"] != cliRepairArchRef {
					t.Fatalf("missing success envelope: %s", stdout)
				}
			} else if !strings.Contains(stdout, "arch_ref set for cp") {
				t.Fatalf("missing acknowledgement: %s", stdout)
			}
			state, err := db.For(paths.New(root).StatePath()).Read()
			if err != nil {
				t.Fatal(err)
			}
			if got := state.FindTask("cp").ArchRef; got != cliRepairArchRef {
				t.Fatalf("arch_ref = %q, want %q", got, cliRepairArchRef)
			}
		})
	}
}

func TestRepairArchRefCLIRejectsAgentIdentity(t *testing.T) {
	for _, name := range []string{brand.EnvName("AGENT_ID"), brand.LegacyEnvName("AGENT_ID")} {
		t.Run(name, func(t *testing.T) {
			root := setupRepairArchRefCLI(t)
			t.Setenv(name, "orchestrator-1")
			before, err := os.ReadFile(paths.New(root).StatePath())
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := executeRootCommandCapture(t, root, "repair-arch-ref", "cp", "--arch-ref", cliRepairArchRef, "--reason", "restore scope", "--json")
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

func TestRepairArchRefCLIRequiresFlags(t *testing.T) {
	for _, args := range [][]string{
		{"repair-arch-ref", "cp", "--reason", "r"},
		{"repair-arch-ref", "cp", "--arch-ref", cliRepairArchRef},
	} {
		root := setupRepairArchRefCLI(t)
		if _, err := executeRootCommandCapture(t, root, args...); err == nil {
			t.Fatalf("accepted missing flag: %v", args)
		}
	}
}
