package ops_test

import (
	"errors"
	"os"
	"testing"

	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSetTaskOutput_PersistenceErrorContext(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	// A directory at the state file path produces a real filesystem error,
	// independently of privilege level or platform permission semantics.
	if err := os.Mkdir(statePath, 0755); err != nil {
		t.Fatal(err)
	}
	err := ops.SetTaskOutput(projectRoot, &ops.SetTaskOutputInput{
		TaskID: "plan-1", AgentID: "code-planner-1",
		Output: []models.OutputEntry{{Desc: "Implement feature", DoneWhen: "Tests pass", Scope: "src", SpecRef: "specs/feature.md"}},
	})
	if err == nil {
		t.Fatal("expected state read failure")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Path != statePath {
		t.Fatalf("persistence error lost its filesystem cause: %v", err)
	}
	_, message := jsonout.ClassifyError(err)
	if message == "internal error" {
		t.Fatalf("persistence failure has no operation context: %v", err)
	}
	details := jsonout.ErrorDetails(err)
	if details["operation"] != "set-task-output" || details["phase"] != "persist-output" || details["task_id"] != "plan-1" || details["state_path"] != statePath || details["output_count"] != 1 || details["recovery_hint"] == "" || details["cause"] == nil {
		t.Fatalf("missing persistence diagnostics: %#v", details)
	}
	if details["outcome"] != models.LifecycleStateChanged || details["safe_action"] != "requery" || details["task_status"] != "UNKNOWN" {
		t.Fatalf("storage failure was classified as caller input or invented task state: %#v", details)
	}
}
