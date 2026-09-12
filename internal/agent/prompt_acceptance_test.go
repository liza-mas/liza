package agent

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestBuildTaskRoleContextDataAcceptanceEvidence(t *testing.T) {
	resolver := testResolver(t)
	for _, role := range []string{"coder", "code-reviewer"} {
		t.Run(role, func(t *testing.T) {
			config := SupervisorConfig{Role: role, AgentID: role + "-1", ProjectRoot: t.TempDir()}
			state := testhelpers.CreateValidState()
			state.Config.IntegrationBranch = "main"
			source := &models.AcceptanceSource{Ref: "specs/plan.md#Boundary", Commit: "source-commit", Blob: "source-blob", ParentTask: "plan-1", ParentReviewCommit: "parent-review"}
			index := 0
			receipt := &models.AcceptanceReceipt{Version: 1, ReviewCommit: "candidate-commit", Source: *source,
				ManifestPath: "acceptance/boundary.json", ManifestBlob: "manifest-blob",
				Mappings: []referencecontract.AcceptanceMapping{{ObligationID: "AC-replay", File: "tests/boundary.test", Assertion: "contested replay", CommandIndex: &index}},
				Commands: []models.AcceptanceCommandResult{{Command: "run-tests", ExitCode: 0}},
			}
			state.Tasks = []models.Task{{ID: "task-42", Status: models.TaskStatusImplementing, AcceptanceSource: source, AcceptanceReceipt: receipt}}
			data, err := testBuildTaskRoleContextData(t, &state.Tasks[0], state, config, resolver)
			if err != nil {
				t.Fatal(err)
			}
			if data.AcceptanceSource == nil || *data.AcceptanceSource != *source || data.AcceptanceReceipt == nil || data.AcceptanceReceipt.ReviewCommit != receipt.ReviewCommit {
				t.Fatal("task evidence did not reach role context")
			}
			// Render through the public role-context builder so the task wiring and
			// template agree, rather than testing a pointer assignment alone.
			sections, err := resolver.ContextSections(role)
			if err != nil {
				t.Fatal(err)
			}
			output, err := prompts.BuildRoleContext(role, sections, data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, source.Ref) || !strings.Contains(output, source.ParentReviewCommit) {
				t.Fatalf("source evidence missing from %s prompt", role)
			}
			if role == "code-reviewer" && (!strings.Contains(output, "contested replay") || !strings.Contains(output, "get tasks task-42 --format json")) {
				t.Fatal("reviewer did not receive mapping and receipt inspection command")
			}
			state.Tasks[0].AcceptanceSource = nil
			state.Tasks[0].AcceptanceReceipt = nil
			data, err = testBuildTaskRoleContextData(t, &state.Tasks[0], state, config, resolver)
			if err != nil {
				t.Fatal(err)
			}
			output, err = prompts.BuildRoleContext(role, sections, data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, "Legacy task: acceptance evidence was not machine-validated") {
				t.Fatalf("legacy classification missing from %s prompt", role)
			}
		})
	}
}
