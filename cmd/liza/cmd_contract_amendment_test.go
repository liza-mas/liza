package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func TestPendingAmendmentBlocksConsumerCheckpointWithoutInvalidatingExecution(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	correction := beginAmendmentCLI(t, root, statePath)
	before := readStateBytes(t, statePath)
	stdout, err := executeRootCommandCapture(t, root, "write-checkpoint", "ordinary",
		"--intent", "Continue existing implementation",
		"--validation-plan", "Run boundary tests",
		"--files-to-modify", "README.md",
		"--agent-id", "coder-2", "--json")
	if err == nil {
		t.Fatalf("consumer checkpoint escaped pending amendment: %s", stdout)
	}
	envelope := parseEnvelope(t, stdout)
	if envelope["ok"] != false || !strings.Contains(envelope["error"].(map[string]any)["message"].(string), "reviewed plan amendment pending for original") {
		t.Fatalf("consumer checkpoint refusal did not identify pending amendment: %s", stdout)
	}
	if readStateBytes(t, statePath) != before {
		t.Fatal("refused consumer checkpoint changed existing execution")
	}
	// The repair's real checkpoint, independent review and merge must proceed.
	mergeAmendmentCLI(t, root, statePath, correction, readState(t, statePath).FindTask("original").Output)
}

func TestReplanCLI_PreservesReviewedOutputIdentityThroughAdoption(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	if err := db.For(statePath).Modify(func(state *models.State) error {
		// The reviewed whole-file planning reference permits handoff prose
		// changes; the output's strict Task 1 allocation remains frozen.
		state.FindTask("original").PlanRef = "specs/acceptance-plan.md"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := readState(t, statePath)
	runAmendmentCLI(t, root, "replan", "original", "--preserve-output-identity", "--trigger", "selector-equivalence", "--reason", "Correct handoff prose with unchanged selectors")
	pending := readState(t, statePath)
	correctionID := pending.FindTask("original").PlanAmendment.Pending
	correction := mustFindTask(t, pending, correctionID)
	if correctionID != "original-replan-1" || correction.AmendmentMode != models.PlanAmendmentPreserveIdentity || correction.PlanningChange == nil || correction.PlanningChange.Trigger != "selector-equivalence" {
		t.Fatalf("preserved replan metadata: %+v", correction)
	}
	output := before.FindTask("original").Output
	mergeAmendmentCLI(t, root, statePath, correctionID, output)
	runAmendmentCLI(t, root, "amend-plan", "original", "--apply", correctionID, "--agent-id", "orchestrator-1", "--json")
	after := readState(t, statePath)
	original := mustFindTask(t, after, "original")
	if original.TransitionsExecuted["replanned"] || original.PlanAmendment.Pending != "" || !reflect.DeepEqual(output, original.Output) || *original.ReviewCommit != *before.FindTask("original").ReviewCommit {
		t.Fatalf("original identity/review facts changed: %+v", original)
	}
	for _, id := range []string{"started-consumer", "ordinary"} {
		if !reflect.DeepEqual(before.FindTask(id), after.FindTask(id)) {
			t.Fatalf("preserved replan changed consumer %s", id)
		}
	}
}
