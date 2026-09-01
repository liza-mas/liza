package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

var dependencyRepairCLIBuild struct {
	sync.Once
	binary  string
	tempDir string
	output  []byte
	err     error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if dependencyRepairCLIBuild.tempDir != "" {
		if err := os.RemoveAll(dependencyRepairCLIBuild.tempDir); err != nil {
			fmt.Fprintf(os.Stderr, "remove dependency-repair test binary: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}

func TestDependencyRepairWorkflow(t *testing.T) {
	cliPath := buildDependencyRepairCLI(t)

	t.Run("applies a command-free multi-task request", func(t *testing.T) {
		request := dependencyRepairWorkflowRequest([]models.DependencyUpdate{
			{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"new-source"}},
			{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"new-consumer"}},
		})
		projectRoot, blackboard, requestPath := setupDependencyRepairWorkflow(t, request)

		runDependencyRepairCLI(t, cliPath, projectRoot,
			"mark-blocked", "repair-source",
			"--agent-id", "coder-1",
			"--reason", "Dependency graph requires an atomic repair",
			"--questions", "Can the orchestrator apply the stored dependency repair?",
			"--repair-request-file", requestPath,
		)

		blocked := readDependencyRepairState(t, blackboard)
		blockedSource := mustFindDependencyRepairTask(t, blocked, "repair-source")
		if blockedSource.RepairRequest == nil || blockedSource.RepairRequest.Command != "" {
			t.Fatalf("stored repair request = %#v, want command-free request", blockedSource.RepairRequest)
		}
		if len(blockedSource.RepairRequest.DependencyUpdates) != 2 {
			t.Fatalf("stored dependency updates = %#v, want two updates", blockedSource.RepairRequest.DependencyUpdates)
		}

		runDependencyRepairCLI(t, cliPath, projectRoot,
			"apply-dependency-repair", "repair-source",
			"--agent-id", "orchestrator-1",
			"--reason", "Apply the stored dependency graph repair",
		)

		persisted := readDependencyRepairState(t, blackboard)
		persistedSource := mustFindDependencyRepairTask(t, persisted, "repair-source")
		persistedConsumer := mustFindDependencyRepairTask(t, persisted, "consumer")
		if !slices.Equal(persistedSource.DependsOn, []string{"new-source"}) ||
			!slices.Equal(persistedConsumer.DependsOn, []string{"new-consumer"}) {
			t.Fatalf("persisted dependencies = source:%v consumer:%v, want [new-source] and [new-consumer]",
				persistedSource.DependsOn, persistedConsumer.DependsOn)
		}
		if persistedSource.Status != models.TaskStatusBlocked || persistedSource.RepairRequest != nil {
			t.Fatalf("source status/request = %s/%#v, want BLOCKED/nil", persistedSource.Status, persistedSource.RepairRequest)
		}
	})

	t.Run("invalid later update leaves no partial state", func(t *testing.T) {
		request := dependencyRepairWorkflowRequest([]models.DependencyUpdate{
			{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"new-source"}},
			{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"missing-dependency"}},
		})
		projectRoot, blackboard, requestPath := setupDependencyRepairWorkflow(t, request)

		runDependencyRepairCLI(t, cliPath, projectRoot,
			"mark-blocked", "repair-source",
			"--agent-id", "coder-1",
			"--reason", "Dependency graph requires an atomic repair",
			"--questions", "Can the orchestrator apply the stored dependency repair?",
			"--repair-request-file", requestPath,
		)
		before := readDependencyRepairState(t, blackboard)

		output, err := executeDependencyRepairCLI(cliPath, projectRoot,
			"apply-dependency-repair", "repair-source",
			"--agent-id", "orchestrator-1",
			"--reason", "Apply the stored dependency graph repair",
		)
		if err == nil {
			t.Fatalf("apply-dependency-repair succeeded for invalid batch\n%s", output)
		}
		if !strings.Contains(output, `desired dependency "missing-dependency" for task consumer does not exist`) {
			t.Fatalf("apply-dependency-repair output = %q, want missing dependency error", output)
		}

		after := readDependencyRepairState(t, blackboard)
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("state changed after invalid batch\nbefore: %#v\nafter:  %#v", before, after)
		}
	})
}

func TestDependencyRepairWorkflow_ResumesFromConsumedRequest(t *testing.T) {
	cliPath := buildDependencyRepairCLI(t)
	request := dependencyRepairWorkflowRequest([]models.DependencyUpdate{
		{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"new-consumer"}},
	})
	projectRoot, blackboard, requestPath := setupDependencyRepairWorkflow(t, request)

	runDependencyRepairCLI(t, cliPath, projectRoot,
		"mark-blocked", "repair-source",
		"--agent-id", "coder-1",
		"--reason", "Dependency graph requires an atomic repair",
		"--questions", "Can the orchestrator apply the stored dependency repair?",
		"--repair-request-file", requestPath,
	)
	runDependencyRepairCLI(t, cliPath, projectRoot,
		"apply-dependency-repair", "repair-source",
		"--agent-id", "orchestrator-1",
		"--reason", "Apply the stored dependency graph repair",
	)

	persisted := readDependencyRepairState(t, blackboard)
	persistedSource := mustFindDependencyRepairTask(t, persisted, "repair-source")
	if persistedSource.RepairRequest != nil {
		t.Fatalf("source repair request = %#v, want cleared", persistedSource.RepairRequest)
	}
	if !slices.Equal(persistedSource.DependsOn, []string{"old-source"}) {
		t.Fatalf("source depends_on = %v, want unchanged [old-source]", persistedSource.DependsOn)
	}

	inspection := runDependencyRepairCLI(t, cliPath, projectRoot, "get", "repair-source", "--json")
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			RepairRequest           *models.RepairRequest `json:"repair_request"`
			DependencyRepairReceipt *struct {
				AffectedTaskIDs []string `json:"affected_task_ids"`
				Validation      []string `json:"validation"`
			} `json:"dependency_repair_receipt"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(inspection), &envelope); err != nil {
		t.Fatalf("decode source inspection: %v\n%s", err, inspection)
	}
	if !envelope.OK {
		t.Fatalf("source inspection not ok: %s", inspection)
	}
	if envelope.Result.RepairRequest != nil {
		t.Fatalf("inspected repair_request = %#v, want cleared", envelope.Result.RepairRequest)
	}
	if envelope.Result.DependencyRepairReceipt == nil {
		t.Fatalf("source inspection missing dependency_repair_receipt: %s", inspection)
	}
	if !slices.Equal(envelope.Result.DependencyRepairReceipt.AffectedTaskIDs, []string{"consumer"}) {
		t.Fatalf("receipt affected_task_ids = %v, want [consumer]", envelope.Result.DependencyRepairReceipt.AffectedTaskIDs)
	}
	if !slices.Equal(envelope.Result.DependencyRepairReceipt.Validation, request.Validation) {
		t.Fatalf("receipt validation = %v, want %v", envelope.Result.DependencyRepairReceipt.Validation, request.Validation)
	}

	consumerInspection := runDependencyRepairCLI(t, cliPath, projectRoot, "get", "consumer", "--json")
	if strings.Contains(consumerInspection, `"dependency_repair_receipt"`) {
		t.Fatalf("consumer inspection leaked source dependency repair receipt: %s", consumerInspection)
	}
}

func buildDependencyRepairCLI(t *testing.T) string {
	t.Helper()
	dependencyRepairCLIBuild.Do(func() {
		_, sourceFile, _, ok := runtime.Caller(0)
		if !ok {
			dependencyRepairCLIBuild.err = fmt.Errorf("locate dependency repair integration test")
			return
		}
		repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
		dependencyRepairCLIBuild.tempDir, dependencyRepairCLIBuild.err = os.MkdirTemp("", "liza-dependency-repair-")
		if dependencyRepairCLIBuild.err != nil {
			return
		}
		dependencyRepairCLIBuild.binary = filepath.Join(dependencyRepairCLIBuild.tempDir, "liza")
		if runtime.GOOS == "windows" {
			dependencyRepairCLIBuild.binary += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", dependencyRepairCLIBuild.binary, "./cmd/liza")
		cmd.Dir = repoRoot
		dependencyRepairCLIBuild.output, dependencyRepairCLIBuild.err = cmd.CombinedOutput()
	})
	if dependencyRepairCLIBuild.err != nil {
		t.Fatalf("build CLI: %v\n%s", dependencyRepairCLIBuild.err, dependencyRepairCLIBuild.output)
	}
	return dependencyRepairCLIBuild.binary
}

func setupDependencyRepairWorkflow(t *testing.T, request models.RepairRequest) (string, *db.Blackboard, string) {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	now := time.Now().UTC()
	source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusImplementing, now)
	source.DependsOn = []string{"old-source"}
	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusReady, now)
	consumer.DependsOn = []string{"old-consumer"}
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	state.Tasks = []models.Task{
		source,
		consumer,
		testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("old-consumer", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("new-source", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("new-consumer", models.TaskStatusMerged, now),
	}
	blackboard := testhelpers.WriteInitialState(t, statePath, state)

	requestData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal repair request: %v", err)
	}
	requestPath := filepath.Join(projectRoot, "repair-request.json")
	if err := os.WriteFile(requestPath, requestData, 0o600); err != nil {
		t.Fatalf("write repair request: %v", err)
	}
	return projectRoot, blackboard, requestPath
}

func dependencyRepairWorkflowRequest(updates []models.DependencyUpdate) models.RepairRequest {
	return models.RepairRequest{
		Operation:         models.RepairOperationApplyDependencyRepair,
		Target:            "repair-source",
		DependencyUpdates: updates,
		Evidence:          []string{"error=dependency graph requires atomic repair"},
		Validation:        []string{"inspect both dependency lists after repair"},
	}
}

func runDependencyRepairCLI(t *testing.T, cliPath, projectRoot string, args ...string) string {
	t.Helper()
	output, err := executeDependencyRepairCLI(cliPath, projectRoot, args...)
	if err != nil {
		t.Fatalf("CLI %s failed: %v\n%s", args[0], err, output)
	}
	return output
}

func executeDependencyRepairCLI(cliPath, projectRoot string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", projectRoot}, args...)
	cmd := exec.Command(cliPath, commandArgs...)
	cmd.Env = append(os.Environ(), brand.EnvName("AGENT_GENERATION")+"="+testhelpers.TestAgentGeneration)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func readDependencyRepairState(t *testing.T, blackboard *db.Blackboard) *models.State {
	t.Helper()
	state, err := blackboard.Read()
	if err != nil {
		t.Fatalf("read dependency repair state: %v", err)
	}
	return state
}

func mustFindDependencyRepairTask(t *testing.T, state *models.State, taskID string) *models.Task {
	t.Helper()
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	return task
}
