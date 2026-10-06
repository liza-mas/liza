package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const providerAddTaskYAML = `id: consumer
description: Consume the selected provider plans
spec_ref: specs/vision.md
done_when: Consumer plan reviewed
scope: bounded consumer
role_pair: architecture-pair
priority: 1
provider_dependencies:
  - provider_task: provider
    transition: architecture-to-code-plan
    outputs: [2, 0]
`

func TestAddTaskFileProviderDependenciesPersistInTextAndJSONModes(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		name := "text"
		if jsonMode {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(brand.EnvName("AGENT_ID"), "")
			t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
			root, statePath := setupMutationTestProject(t, func(state *models.State) {
				provider := testhelpers.BuildTaskByStatus("provider", models.TaskStatus("DRAFT_ARCHITECTURE"), time.Now().UTC())
				provider.RolePair = "architecture-pair"
				provider.Type = models.TaskType("architecture")
				provider.SpecRef = "specs/vision.md"
				state.Tasks = []models.Task{provider}
			})
			testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
			file := filepath.Join(root, "consumer.yaml")
			if err := os.WriteFile(file, []byte(providerAddTaskYAML), 0o644); err != nil {
				t.Fatal(err)
			}
			args := []string{"add-task", "--file", file, "--agent-id", "orchestrator-1"}
			if jsonMode {
				args = append(args, "--json")
			}
			stdout, err := executeRootCommandCapture(t, root, args...)
			if err != nil {
				t.Fatalf("add-task --file: %v (%s)", err, stdout)
			}
			if jsonMode && parseEnvelope(t, stdout)["ok"] != true {
				t.Fatalf("JSON command did not report success: %s", stdout)
			}
			consumer := mustFindTask(t, readState(t, statePath), "consumer")
			want := []models.ProviderDependency{{
				ProviderTask: "provider", Transition: "architecture-to-code-plan", Outputs: []int{2, 0},
			}}
			if !reflect.DeepEqual(consumer.ProviderDependencies, want) {
				t.Fatalf("%s CLI dropped or retargeted YAML provider intent: got %+v, want %+v", name, consumer.ProviderDependencies, want)
			}
			if len(consumer.DependsOn) != 0 {
				t.Fatalf("provider wait was converted into ordinary direct edges: %v", consumer.DependsOn)
			}
		})
	}
}

func TestAddTaskFileProviderDependenciesRejectInvalidIndexesAtomically(t *testing.T) {
	for _, outputs := range []string{"[null]", "[0.5]", "[1.0]", `["0"]`, "[-1]", "[0, 0]", "[]"} {
		for _, jsonMode := range []bool{false, true} {
			name := "text/" + outputs
			if jsonMode {
				name = "json/" + outputs
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv(brand.EnvName("AGENT_ID"), "")
				t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
				root, statePath := setupMutationTestProject(t, func(state *models.State) {
					provider := testhelpers.BuildTaskByStatus("provider", models.TaskStatus("DRAFT_ARCHITECTURE"), time.Now().UTC())
					provider.RolePair = "architecture-pair"
					provider.Type = models.TaskType("architecture")
					provider.SpecRef = "specs/vision.md"
					state.Tasks = []models.Task{provider}
				})
				testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
				file := filepath.Join(root, "consumer.yaml")
				content := strings.Replace(providerAddTaskYAML, "[2, 0]", outputs, 1)
				if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"add-task", "--file", file, "--agent-id", "orchestrator-1"}
				if jsonMode {
					args = append(args, "--json")
				}
				stdout, err := executeRootCommandCapture(t, root, args...)
				if err == nil {
					t.Fatalf("invalid output selection %s accepted: %s", outputs, stdout)
				}
				if !strings.Contains(err.Error()+stdout, "provider") && !strings.Contains(err.Error()+stdout, "parse task file") {
					t.Fatalf("unexpected refusal: %v (%s)", err, stdout)
				}
				after, readErr := os.ReadFile(statePath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if !bytes.Equal(before, after) {
					t.Fatal("refused file-based add-task changed state")
				}
			})
		}
	}
}
