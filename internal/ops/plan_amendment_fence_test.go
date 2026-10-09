package ops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func TestPlanAmendmentFencesEveryCardinalityAndRecovery(t *testing.T) {
	config, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(config)
	for _, cardinality := range []string{"per-subtask", "one-to-one", "many-to-one"} {
		for _, status := range []models.TaskStatus{models.TaskStatusCodingPlanApproved, models.TaskStatusMerged} {
			for _, correction := range []bool{false, true} {
				t.Run(string(status)+"/"+cardinality+"/"+map[bool]string{false: "pending original", true: "correction"}[correction], func(t *testing.T) {
					task := models.Task{ID: "source", RolePair: "code-planning-pair", Status: status, Output: []models.OutputEntry{{Desc: "existing slot"}}}
					if correction {
						task.AmendsPlan = "original"
					} else {
						task.PlanAmendment = &models.PlanAmendment{Pending: "correction"}
					}
					state := &models.State{Tasks: []models.Task{task}}
					definition := transitionDef{cardinality: cardinality, sourceRolePair: task.RolePair, requiredStatus: status}
					before, _ := json.Marshal(state)
					result := &ProceedResult{}
					if err := proceedInner(state, task.ID, "transition", definition, inheritedDepSet{}, resolver, time.Now().UTC(), result); err == nil || !strings.Contains(err.Error(), "fenced") {
						t.Fatalf("generation: %v", err)
					}
					if err := recoverCrashedTransition(state, &state.Tasks[0], task.ID, "transition", definition, inheritedDepSet{}, resolver, time.Now().UTC(), result); err == nil || !strings.Contains(err.Error(), "fenced") {
						t.Fatalf("recovery: %v", err)
					}
					after, _ := json.Marshal(state)
					if string(before) != string(after) || len(result.ChildTaskIDs) != 0 {
						t.Fatal("fenced source mutated generation state")
					}
				})
			}
		}
	}
}
