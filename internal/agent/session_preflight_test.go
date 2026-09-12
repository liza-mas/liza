package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/sessionvalidation"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// Exercise the real supervisor-to-provider boundary. YAML declarations keep the
// regression runnable before the typed prerequisite model exists.
func TestSessionPreflightBlocksInvalidLaunchAndAllowsRepair(t *testing.T) {
	for _, kind := range []string{"missing environment", "missing executable", "wrong interpreter", "undeclared task"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newProviderGenerationFixture(t)
			taskID := "task-validation"
			testhelpers.CreateTestWorktree(t, fixture.projectRoot, taskID)
			worktree := filepath.Join(fixture.projectRoot, ".worktrees", taskID)
			binDir := t.TempDir()
			marker := filepath.Join(t.TempDir(), "provider-started")
			testhelpers.WriteShellStub(t, filepath.Join(binDir, "gemini"), fmt.Sprintf("#!/bin/sh\nprintf started >> %q\n", marker))
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("PREFLIGHT_TEST_DATABASE_URL", "")

			var declaration string
			var repair func()
			switch kind {
			case "missing environment":
				declaration = "env: [PREFLIGHT_TEST_DATABASE_URL]"
				repair = func() { t.Setenv("PREFLIGHT_TEST_DATABASE_URL", "test-only-ready-value") }
			case "missing executable":
				declaration = "executables: [./validation-runtime]"
				repair = func() {
					testhelpers.WriteShellStub(t, filepath.Join(worktree, "validation-runtime"), "#!/bin/sh\nexit 0\n")
					// Reclaim intentionally preserves the dirty-worktree guard.
					// Record the repaired fixture, including platform relay files.
					testhelpers.MustGit(t, worktree, "add", "--", "validation-runtime*")
					testhelpers.MustGit(t, worktree, "commit", "-m", "test: install prerequisite runtime", "-m", "Record the synthetic executable so repaired reclaim verifies readiness from a clean worktree.")
				}
			case "wrong interpreter":
				declaration = "probes: [[validation-python, -c, 'import project_dependency']]"
				testhelpers.WriteShellStub(t, filepath.Join(binDir, "validation-python"), "#!/bin/sh\necho PREFLIGHT_PRIVATE_PROBE_OUTPUT >&2\nexit 1\n")
				repair = func() {
					preparedBin := t.TempDir()
					testhelpers.WriteShellStub(t, filepath.Join(preparedBin, "validation-python"), "#!/bin/sh\nexit 0\n")
					t.Setenv("PATH", preparedBin+string(os.PathListSeparator)+os.Getenv("PATH"))
				}
			}

			task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, time.Now().UTC())
			commit := strings.TrimSpace(testhelpers.MustGit(t, worktree, "rev-parse", "HEAD"))
			task.BaseCommit = &commit
			contract := "validation: ['project-validation']\n"
			if declaration != "" {
				contract += "validation_prerequisites:\n  - command: project-validation\n    " + declaration + "\n"
			}
			if err := yaml.Unmarshal([]byte(contract), &task); err != nil {
				t.Fatal(err)
			}
			var runtimeConfig models.Config
			if declaration != "" {
				if err := yaml.Unmarshal([]byte("agent_tools:\n  gemini:\n    validation_execution: local\n"), &runtimeConfig); err != nil {
					t.Fatal(err)
				}
			}
			if err := fixture.bb.Modify(func(state *models.State) error {
				state.Tasks = []models.Task{task}
				state.Config.AgentTools = runtimeConfig.AgentTools
				agent := state.Agents[fixture.agentID]
				agent.Status = models.AgentStatusWorking
				agent.CurrentTask = &taskID
				state.Agents[fixture.agentID] = agent
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			// A successful setup child does not make the supervisor session ready.
			if err := ops.RunPostWorktreeCmd("exit 0", worktree); err != nil {
				t.Fatalf("setup prerequisite: %v", err)
			}
			config := fixture.config(fixture.authorityA)
			config.LLMAgent = NewCLIAgent("")
			launchCase := "undeclared task starts provider"
			if repair != nil {
				t.Run("invalid session cannot start provider", func(t *testing.T) {
					_, output, err := executeAgent(context.Background(), config, "test prompt", []string{worktree}, taskID, runtimeConfig)
					if !errors.Is(err, sessionvalidation.ErrPreflight) {
						t.Errorf("launch error = %v, want a prerequisite preflight failure", err)
					}
					if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
						t.Errorf("invalid session started provider: marker stat = %v", statErr)
					}
					if strings.Contains(fmt.Sprint(err, output), "PREFLIGHT_PRIVATE_PROBE_OUTPUT") {
						t.Error("probe output escaped into launch diagnostics")
					}
				})
				repair()
				// Failed preflight released ownership without losing the worktree.
				// An explicit repaired claim must freshly verify its prerequisites.
				session, err := prepareSupervisorSession(config, runtimeConfig)
				if err != nil {
					t.Fatal(err)
				}
				session.ForceCheck = true
				if _, err := ops.ClaimTaskWithAuthority(fixture.projectRoot, taskID, fixture.authorityA, session); err != nil {
					t.Fatalf("reclaim repaired session: %v", err)
				}
				launchCase = "repaired session starts provider"
			}
			t.Run(launchCase, func(t *testing.T) {
				before, _ := os.ReadFile(marker)
				code, _, err := executeAgent(context.Background(), config, "test prompt", []string{worktree}, taskID, runtimeConfig)
				if err != nil || code != 0 {
					t.Fatalf("launch = (%d, %v), want success", code, err)
				}
				after, err := os.ReadFile(marker)
				if err != nil || string(after) != string(before)+"started" {
					t.Fatalf("provider did not start exactly once: before=%q after=%q error=%v", before, after, err)
				}
			})
		})
	}
}
