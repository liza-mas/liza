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
	"github.com/liza-mas/liza/internal/sessionvalidation"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

type preflightBackendCase struct {
	name, tool  string
	interactive bool
}

func preflightBackendCases() []preflightBackendCase {
	return []preflightBackendCase{
		{"cli", "gemini", false}, {"cli interactive", "gemini", true},
		{"acpx", "codex-acp", false}, {"acpx interactive", "codex-acp", true},
	}
}

func (c preflightBackendCase) adapter() LLMAgent {
	if c.tool == "codex-acp" {
		return NewACPXAgent("")
	}
	return NewCLIAgent("")
}

// All subprocesses, including ACPX session lookup/creation, write the marker.
// Removing preflight enforcement therefore fails the negative assertion; the
// paired successful launch rules out a broken stub or unsupported adapter.
func TestSessionPreflightEveryBackendBlocksAndRepairs(t *testing.T) {
	for _, backend := range preflightBackendCases() {
		t.Run(backend.name, func(t *testing.T) {
			for _, declared := range []bool{true, false} {
				t.Run(fmt.Sprintf("declared=%t", declared), func(t *testing.T) {
					fixture, config, runtimeConfig, worktree := backendPreflightFixture(t, backend, declared)
					marker := filepath.Join(t.TempDir(), "started")
					bin := t.TempDir()
					writeBackendPreflightStubs(t, bin, marker, "selected")
					t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
					t.Setenv("BACKEND_PREFLIGHT_VALUE", "")
					launch := func() error {
						code, _, err := executeAgent(context.Background(), config, "test prompt", []string{worktree}, "task-backend", runtimeConfig)
						if err == nil && code != 0 {
							t.Fatalf("unexpected provider exit %d", code)
						}
						return err
					}
					if declared {
						if err := launch(); !errors.Is(err, sessionvalidation.ErrPreflight) {
							t.Fatalf("invalid session error = %v, want typed preflight failure", err)
						}
						if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("invalid session launched a provider subprocess: %v", err)
						}
						t.Setenv("BACKEND_PREFLIGHT_VALUE", "repaired-test-value")
						// Reclaim after the failed launch released executable ownership.
						if err := fixture.bb.Modify(func(state *models.State) error {
							task := state.FindTask("task-backend")
							task.Status = models.TaskStatusImplementing
							task.AssignedTo = &fixture.agentID
							return nil
						}); err != nil {
							t.Fatal(err)
						}
					}
					if err := launch(); err != nil {
						t.Fatalf("ready or legacy launch failed: %v", err)
					}
					data, err := os.ReadFile(marker)
					if err != nil || !strings.Contains(string(data), "selected|") {
						t.Fatalf("provider did not launch: marker=%q error=%v", data, err)
					}
				})
			}
		})
	}
}

func TestSessionPreflightAdapterEnvFilesAndSelectedPath(t *testing.T) {
	for _, backend := range preflightBackendCases() {
		t.Run(backend.name, func(t *testing.T) {
			root, selected, ambient := t.TempDir(), t.TempDir(), t.TempDir()
			marker := filepath.Join(root, "children")
			writeBackendPreflightStubs(t, selected, marker, "selected")
			writeBackendPreflightStubs(t, ambient, marker, "ambient")
			t.Setenv("PATH", ambient+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("BACKEND_PREFLIGHT_VALUE", "parent-test-value")
			config := models.Config{AgentTools: map[string]models.AgentToolConfig{
				backend.tool: {EnvFiles: []string{"runtime.conf"}},
			}}
			adapter := backend.adapter()
			for _, value := range []string{"first-overlay", "refreshed-overlay"} {
				content := "BACKEND_PREFLIGHT_VALUE=" + value + "\nPATH=" + selected + string(os.PathListSeparator) + os.Getenv("PATH") + "\n"
				if err := os.WriteFile(filepath.Join(root, "runtime.conf"), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				before, _ := os.ReadFile(marker)
				var code int
				var err error
				if backend.interactive {
					code, err = adapter.RunInteractive(context.Background(), LLMAgentInteractiveRequest{BackendName: backend.tool, AgentID: "coder-1", Generation: "test-generation", ProjectRoot: root, RuntimeConfig: config, LaunchGate: immediateLaunchGate})
				} else {
					result, runErr := adapter.Run(context.Background(), LLMAgentRunRequest{BackendName: backend.tool, AgentID: "coder-1", Generation: "test-generation", ProjectRoot: root, RuntimeConfig: config, Prompt: "test prompt", LaunchGate: immediateLaunchGate})
					code, err = result.ExitCode, runErr
				}
				if err != nil || code != 0 {
					t.Fatalf("adapter launch = (%d, %v)", code, err)
				}
				after, err := os.ReadFile(marker)
				if err != nil || len(after) <= len(before) {
					t.Fatalf("no child evidence: %v", err)
				}
				for _, line := range strings.Split(strings.TrimSpace(string(after[len(before):])), "\n") {
					if !strings.HasPrefix(line, "selected|"+value+"|") {
						t.Fatalf("subprocess used ambient executable or environment: %q", line)
					}
				}
				if os.Getenv("BACKEND_PREFLIGHT_VALUE") != "parent-test-value" {
					t.Fatal("overlay mutated parent environment")
				}
			}
		})
	}
}

func TestSessionPreflightACPXContextChangesSession(t *testing.T) {
	backend := preflightBackendCase{name: "acpx", tool: "codex-acp"}
	fixture, config, runtimeConfig, worktree := backendPreflightFixture(t, backend, true)
	marker, bin := filepath.Join(t.TempDir(), "children"), t.TempDir()
	writeBackendPreflightStubs(t, bin, marker, "selected")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BACKEND_PREFLIGHT_VALUE", "initial-test-value")
	launchScope := func() string {
		before, _ := os.ReadFile(marker)
		code, _, err := executeAgent(context.Background(), config, "test prompt", []string{worktree}, "task-backend", runtimeConfig)
		if err != nil || code != 0 {
			t.Fatalf("ACPX launch = (%d, %v)", code, err)
		}
		after, err := os.ReadFile(marker)
		if err != nil || len(after) <= len(before) {
			t.Fatalf("no ACPX invocation: %v", err)
		}
		for _, line := range strings.Split(string(after[len(before):]), "\n") {
			if !strings.Contains(line, " prompt ") {
				continue
			}
			args := strings.Fields(strings.SplitN(line, "|", 3)[2])
			for i, arg := range args {
				if arg == "-s" && i+1 < len(args) {
					return args[i+1]
				}
			}
		}
		t.Fatal("missing scoped ACPX prompt invocation")
		return ""
	}
	previous := launchScope()
	if again := launchScope(); again != previous {
		t.Fatal("unchanged context did not preserve session identity")
	}
	changes := []struct {
		name   string
		change func()
	}{
		{"environment", func() { t.Setenv("BACKEND_PREFLIGHT_VALUE", "changed-test-value") }},
		{"commit", func() {
			testhelpers.MustGit(t, worktree, "commit", "--allow-empty", "-m", "test: change validation commit")
		}},
		{"generation", func() { fixture.expireCurrent(t); config.Authority = fixture.registerReplacement(t) }},
	}
	for _, change := range changes {
		change.change()
		current := launchScope()
		if current == previous {
			t.Fatalf("%s change reused old ACPX session", change.name)
		}
		previous = current
	}
}

func backendPreflightFixture(t *testing.T, backend preflightBackendCase, declared bool) (providerGenerationFixture, SupervisorConfig, models.Config, string) {
	t.Helper()
	fixture := newProviderGenerationFixture(t)
	taskID := "task-backend"
	testhelpers.CreateTestWorktree(t, fixture.projectRoot, taskID)
	worktree := filepath.Join(fixture.projectRoot, ".worktrees", taskID)
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, time.Now().UTC())
	commit := strings.TrimSpace(testhelpers.MustGit(t, worktree, "rev-parse", "HEAD"))
	task.BaseCommit = &commit
	task.AssignedTo = &fixture.agentID
	declaration := "validation: [project-validation]\n"
	var runtimeConfig models.Config
	if declared {
		declaration += "validation_prerequisites:\n  - command: project-validation\n    env: [BACKEND_PREFLIGHT_VALUE]\n"
		if err := yaml.Unmarshal([]byte("agent_tools:\n  "+backend.tool+":\n    validation_execution: local\n"), &runtimeConfig); err != nil {
			t.Fatal(err)
		}
	}
	if err := yaml.Unmarshal([]byte(declaration), &task); err != nil {
		t.Fatal(err)
	}
	if err := fixture.bb.Modify(func(state *models.State) error {
		state.Tasks = []models.Task{task}
		state.Config.AgentTools = runtimeConfig.AgentTools
		agent := state.Agents[fixture.agentID]
		agent.Provider = backend.tool
		state.Agents[fixture.agentID] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	config := fixture.config(fixture.authorityA)
	config.CLIName, config.Interactive, config.LLMAgent = backend.tool, backend.interactive, backend.adapter()
	return fixture, config, runtimeConfig, worktree
}

func writeBackendPreflightStubs(t *testing.T, bin, marker, label string) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s|%%s|%%s\\n' %q \"$BACKEND_PREFLIGHT_VALUE\" \"$*\" >> %q\n", label, filepath.ToSlash(marker))
	for _, executable := range []string{"gemini", "codex", "acpx"} {
		body := script
		if executable == "acpx" {
			body += "case \"$*\" in *\" prompt \"*) printf '%s\\n' '{\"result\":{}}';; esac\n"
		}
		testhelpers.WriteShellStub(t, filepath.Join(bin, executable), body)
	}
}
