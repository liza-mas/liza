package ops

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-225: a story the orchestrator added without a cohort parent can never run
// the many-to-one us-to-coding transition, so only an operator ruling can
// settle it once its downstream work merged.

func cohortlessStory(id string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, time.Now().UTC())
	task.RolePair = "us-writing-pair"
	task.Type = "us-writing"
	return task
}

func storyConsumer(id, rolePair string, status models.TaskStatus, storyID string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, time.Now().UTC())
	task.RolePair = rolePair
	task.DependsOn = []string{storyID}
	return task
}

func unsettledPlanning(t *testing.T, root string, state *models.State) []string {
	t.Helper()
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := resolver.SlicedIntegrationCapability()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := UnsettledPreIntegrationPlanningTasks(state, capability)
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

func TestRetireByRulingSettlesDeliveredCohortlessStory(t *testing.T) {
	// GIVEN a merged parentless story whose architecture consumer merged
	story := cohortlessStory("story")
	arm := storyConsumer("story-arm", "architecture-main-pair", models.TaskStatusMerged, story.ID)
	root, stateFile := setupPlanCheckTest(t, story, arm)
	before, _ := db.For(stateFile).Read()
	if pending := unsettledPlanning(t, root, before); !slices.Contains(pending, story.ID) {
		t.Fatalf("fixture story is not unsettled: %v", pending)
	}

	// WHEN an operator retires it by ruling
	input := PlanCheckInput{TaskID: story.ID, Action: PlanCheckActionRetire, Ruling: "D-225", ChangedBy: "operator"}
	result, err := RecordPlanCheck(root, input)

	// THEN the retirement is recorded, replays, and settles integration planning
	if err != nil || !result.Changed || result.Verdict != models.PlanCheckRetired || result.Ruling != "D-225" {
		t.Fatalf("retire = %+v, %v", result, err)
	}
	if replay, err := RecordPlanCheck(root, input); err != nil || replay.Changed {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	after, _ := db.For(stateFile).Read()
	retired := after.FindTask(story.ID)
	if retired.Status != models.TaskStatusMerged || retired.RetiredBy() != "ruling D-225" || retired.PlanCheck.By != "operator" {
		t.Fatalf("retired story = %+v", retired.PlanCheck)
	}
	if retired.TransitionsExecuted["us-to-coding"] {
		t.Fatal("retirement fabricated the us-to-coding transition")
	}
	if pending := unsettledPlanning(t, root, after); slices.Contains(pending, story.ID) {
		t.Fatalf("retired story still unsettles planning: %v", pending)
	}
}

func TestRetireByRulingRefusesUndeliveredOrCohortStory(t *testing.T) {
	for scenario, want := range map[string]string{
		"no dependent":            "no MERGED downstream task outside us-writing-pair depends on it",
		"story dependent only":    "no MERGED downstream task outside us-writing-pair depends on it",
		"nonterminal consumer":    "downstream task story-code is not terminal",
		"cohort member":           "it belongs to the cohort of epic",
		"transition executed":     "no unexecuted many-to-one transition",
		"has output":              "it has output",
		"replace by its consumer": "must have an unused MERGED reviewed handoff",
	} {
		t.Run(scenario, func(t *testing.T) {
			story := cohortlessStory("story")
			tasks := []models.Task{story, storyConsumer("story-arm", "architecture-main-pair", models.TaskStatusMerged, story.ID)}
			input := PlanCheckInput{TaskID: story.ID, Action: PlanCheckActionRetire, Ruling: "D-225", ChangedBy: "operator"}
			switch scenario {
			case "no dependent":
				tasks = tasks[:1]
			case "story dependent only":
				tasks[1] = storyConsumer("story-next", "us-writing-pair", models.TaskStatusMerged, story.ID)
			case "nonterminal consumer":
				tasks = append(tasks, storyConsumer("story-code", "coding-pair", models.TaskStatusReady, story.ID))
			case "cohort member":
				epic := testhelpers.BuildTaskByStatus("epic", models.TaskStatusMerged, time.Now().UTC())
				epic.RolePair = "epic-planning-pair"
				tasks[0].ParentTasks = []string{epic.ID}
				tasks = append(tasks, epic)
			case "transition executed":
				tasks[0].TransitionsExecuted = map[string]bool{"us-to-coding": true}
			case "has output":
				tasks[0].Output = []models.OutputEntry{{Desc: "story output", DoneWhen: "done", Scope: "internal/", SpecRef: "README.md"}}
			case "replace by its consumer":
				input = PlanCheckInput{TaskID: story.ID, Action: PlanCheckActionReplace, ReplacedBy: "story-arm", ChangedBy: "operator"}
			}
			root, stateFile := setupPlanCheckTest(t, tasks...)
			before, _ := db.For(stateFile).Read()
			if _, err := RecordPlanCheck(root, input); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("retire = %v, want a refusal containing %q", err, want)
			}
			after, _ := db.For(stateFile).Read()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("refused retirement changed state")
			}
		})
	}
}
