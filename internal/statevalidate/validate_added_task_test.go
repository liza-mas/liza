package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateAddedTask_ParentLineage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		parents   []string
		wantError string
	}{
		{name: "existing parent outside the scoped record", parents: []string{"parent"}},
		{name: "missing parent", parents: []string{"ghost"}, wantError: "task added has parent_task referencing non-existent task 'ghost'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN an added task whose parent lives elsewhere in state, with an
			// unrelated invalid record the scoped validation must not require
			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			parent := testhelpers.BuildTaskByStatus("parent", models.TaskStatusMerged, now)
			parent.DoneWhen = ""
			added := testhelpers.BuildTaskByStatus("added", models.TaskStatusReady, now)
			added.ParentTasks = tc.parents
			state.Tasks = []models.Task{parent, added}
			state.Sprint.Scope.Planned = []string{"added"}

			root := t.TempDir()
			testhelpers.SetupLizaDir(t, root)
			testhelpers.SetupPipelineConfig(t, root)

			// WHEN the added task is validated
			err := ValidateAddedTask(state, root, "added", true, nil)

			// THEN parent existence is checked against the full state
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("ValidateAddedTask() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("ValidateAddedTask() error = %v, want %q", err, tc.wantError)
			}
		})
	}
}
