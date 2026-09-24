package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateTaskInvariants_PlanCheckShape(t *testing.T) {
	cfg := loadTestConfig(t)
	resolver := pipeline.NewResolver(cfg)
	now := time.Now().UTC()
	plan := func(mutate func(*models.Task)) models.Task {
		task := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
		task.RolePair = "code-planning-pair"
		task.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckPassed, By: "orchestrator-1", At: now}
		if mutate != nil {
			mutate(&task)
		}
		return task
	}
	cases := []struct {
		name    string
		task    models.Task
		wantErr string
	}{
		{name: "passed", task: plan(nil)},
		{name: "held with ask", task: plan(func(task *models.Task) {
			task.PlanCheck.Verdict, task.PlanCheck.Ask = models.PlanCheckHeld, "seed smoke users"
		})},
		{name: "held without ask", task: plan(func(task *models.Task) { task.PlanCheck.Verdict = models.PlanCheckHeld }), wantErr: "held requires ask"},
		{name: "unknown verdict", task: plan(func(task *models.Task) { task.PlanCheck.Verdict = "approved" }), wantErr: "invalid verdict"},
		{name: "missing actor", task: plan(func(task *models.Task) { task.PlanCheck.By = "" }), wantErr: "requires by and at"},
		{name: "not merged", task: plan(func(task *models.Task) {
			check := task.PlanCheck
			*task = testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusDraftCodingPlan, now)
			task.RolePair, task.PlanCheck = "code-planning-pair", check
		}), wantErr: "requires MERGED"},
		{name: "not a planning pair", task: plan(func(task *models.Task) { task.RolePair = "coding-pair" }), wantErr: "non-planning role_pair"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTaskInvariants(stateWithTasks(tc.task), "", true, resolver, cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateTaskInvariants = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateTaskInvariants = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
