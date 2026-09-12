package ops

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestMarkBlockedDependencyDirection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		sourcePair       string
		dependencyPair   string
		mergedDependency bool
		existingOnly     bool
		wantReject       bool
	}{
		{name: "pending downstream", sourcePair: "epic-planning-pair", dependencyPair: "us-writing-pair", wantReject: true},
		{name: "merged downstream", sourcePair: "epic-planning-pair", dependencyPair: "us-writing-pair", mergedDependency: true, wantReject: true},
		{name: "upstream", sourcePair: "us-writing-pair", dependencyPair: "epic-planning-pair"},
		{name: "same stage", sourcePair: "epic-planning-pair", dependencyPair: "epic-planning-pair"},
		{name: "emergency block retains existing invalid edge", sourcePair: "epic-planning-pair", dependencyPair: "us-writing-pair", existingOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			testhelpers.SetupPipelineConfig(t, root)
			resolver, _, err := loadResolver(root)
			if err != nil {
				t.Fatal(err)
			}
			executing, err := resolver.ExecutingStatus(tc.sourcePair)
			if err != nil {
				t.Fatal(err)
			}
			doer, err := resolver.DoerRole(tc.sourcePair)
			if err != nil {
				t.Fatal(err)
			}
			initial, err := resolver.InitialStatus(tc.dependencyPair)
			if err != nil {
				t.Fatal(err)
			}
			if tc.mergedDependency {
				initial = models.TaskStatusMerged
			}
			now := time.Now().UTC()
			task := testhelpers.BuildTaskByStatus("source", models.TaskStatusImplementing, now)
			task.RolePair, task.Status = tc.sourcePair, executing
			agentID := doer + "-1"
			task.AssignedTo = &agentID
			task.DependsOn = []string{"prior"}
			dependency := testhelpers.BuildTaskByStatus("dependency", initial, now)
			dependency.RolePair = tc.dependencyPair
			prior := testhelpers.BuildTaskByStatus("prior", models.TaskStatusMerged, now)
			prior.RolePair = "epic-planning-main-pair"
			newDeps := []string{dependency.ID}
			if tc.existingOnly {
				task.DependsOn = append(task.DependsOn, dependency.ID)
				newDeps = nil
			}
			wantDeps := append(slices.Clone(task.DependsOn), newDeps...)
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{task, dependency, prior}
			agent := testhelpers.RegisteredTestAgent(doer)
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask, agent.LeaseExpires = &task.ID, task.LeaseExpires
			state.Agents[agentID] = agent
			testhelpers.WriteInitialState(t, statePath, state)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}

			_, err = MarkBlockedWithOptions(root, task.ID, "Needs corrected policy", []string{"Who supplies the correction?"}, agentID, MarkBlockedOptions{DependsOn: newDeps})
			if tc.wantReject {
				if err == nil || !strings.Contains(err.Error(), "downstream dependency dependency") {
					t.Errorf("error = %v, want downstream dependency rejection", err)
				}
				after, readErr := os.ReadFile(statePath)
				if readErr != nil || !bytes.Equal(before, after) {
					t.Fatalf("rejected dependency changed persisted task/owner/lifecycle state: %v", readErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := db.For(statePath).Read()
			if err != nil {
				t.Fatal(err)
			}
			blocked := after.FindTask(task.ID)
			if blocked.Status != models.TaskStatusBlocked || blocked.AssignedTo != nil || !slices.Equal(blocked.DependsOn, wantDeps) {
				t.Fatalf("block did not preserve requested dependencies and release ownership: status=%s assigned=%v deps=%v", blocked.Status, blocked.AssignedTo, blocked.DependsOn)
			}
		})
	}
}
