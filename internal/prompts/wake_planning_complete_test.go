package prompts

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D73: the PLANNING_COMPLETE turn lists each plan to review with its plan
// files, the executability checks, and the three supported dispositions.
func TestPlanningCompleteInstructionsReviewDispositions(t *testing.T) {
	now := time.Now().UTC()
	plan := func(id string, deps ...string) models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
		task.RolePair = "code-planning-pair"
		task.DependsOn = deps
		task.Output = []models.OutputEntry{
			{Desc: "one", DoneWhen: "d", Scope: "s", PlanRef: "specs/plans/" + id + ".md#Task 1"},
			{Desc: "two", DoneWhen: "d", Scope: "s", PlanRef: "specs/plans/" + id + ".md#Task 2"},
		}
		return task
	}
	upstream := plan("upstream")
	upstream.TransitionsExecuted = map[string]bool{"replanned": true, "code-plan-to-coding": true}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{plan("fresh"), plan("consumer", "upstream"), upstream}
	state.Sprint.Scope.Planned = []string{"fresh", "consumer", "upstream"}

	_, instruction, err := RenderOrchestratorDashboard(state, setupPipelineConfig(t), "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PLANS TO REVIEW:\n- fresh: specs/plans/fresh.md\n",
		"- consumer: specs/plans/consumer.md (a pass is refused while upstream upstream was replanned)",
		"`pre-commit run --files <paths>`",
		strings.Join(ops.TestFileMatcherPatterns(), ", "),
		"plan_ref anchor names exactly one ATX heading",
		"has a producer (a task, a committed generator, or an explicit human step)",
		"its instances are recorded after the children exist, so do not hold for them",
		"use reviewed amend-plan or replan to declare runtime_inputs when",
		"Notes never excuse missing DB/venv/container producers, durable runtime_inputs or human provisioning",
		brand.Command("plan-check") + " <task-id> --pass --notes-file <file>",
		brand.Command("amend-plan") + " <task-id> --apply <correction-id>",
		brand.Command("replan") + ` <task-id> --reason "<check>: <evidence> → <required correction>" --changed-by orchestrator-1`,
		brand.Command("plan-check") + ` <task-id> --hold "<exact human action>" --agent-id orchestrator-1 --json`,
		brand.Command("plan-check") + " <task-id> --pass --agent-id orchestrator-1 --json",
		"If every plan awaits amendment, was replanned or held, do not checkpoint.",
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("instruction missing %q:\n%s", want, instruction)
		}
	}
	if strings.Contains(instruction, "READY") || strings.Contains(instruction, "- upstream") {
		t.Errorf("a replanned plan is not handed off and nothing is ready:\n%s", instruction)
	}
}
