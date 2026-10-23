package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// D-33 fixture roles. In minimalPipelineYAML the story pair (us-writing-pair)
// moves on only through the many-to-one us-to-code-plan transition, whose
// target is code-planning-pair: a story added without a parent is a dead end
// unless a code-planning-pair task that depends on it is commissioned with it.
// research-pair is an isolated stage, neither upstream nor downstream of the
// others, so its edges to stories and plans are legal in both directions.
const (
	storyPair    = "us-writing-pair"
	deliveryPair = "code-planning-pair"
	unrelPair    = "research-pair"
	codingPair   = "coding-pair"
)

func deliveryPipelineYAML(t *testing.T) string {
	t.Helper()
	out := strings.Replace(minimalPipelineYAML, "  role-pairs:\n", `    researcher:
      type: doer
      display-name: "Researcher"
    research-reviewer:
      type: reviewer
      display-name: "Research Reviewer"
  role-pairs:
    research-pair:
      doer: researcher
      reviewer: research-reviewer
      states:
        initial: DRAFT_RESEARCH
        executing: RESEARCHING
        submitted: RESEARCH_READY_FOR_REVIEW
        reviewing: REVIEWING_RESEARCH
        approved: RESEARCH_APPROVED
        rejected: RESEARCH_REJECTED
`, 1)
	out = strings.Replace(out, "  sub-pipelines:\n", `  sub-pipelines:
    research-subpipeline:
      steps:
        - research-pair
      transitions: []
`, 1)
	if strings.Count(out, "research-pair") != 2 {
		t.Fatal("delivery fixture: pipeline anchors not found in minimalPipelineYAML")
	}
	return out
}

func setupDeliveryProject(t *testing.T, noFollowUp bool) (stateFile, logFile string) {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ = testhelpers.SetupLizaDir(t, tmpDir)
	logFile = filepath.Join(tmpDir, paths.ProjectDirName(), "log.jsonl")
	testhelpers.CreateSpecFile(t, tmpDir, "feature.md", "# Feature\n")
	pipelinePath := filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml")
	if err := os.WriteFile(pipelinePath, []byte(deliveryPipelineYAML(t)), 0644); err != nil {
		t.Fatalf("write pipeline.yaml: %v", err)
	}
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 1
	state.Config.NoFollowUp = noFollowUp
	testhelpers.WriteInitialState(t, stateFile, state)
	return stateFile, logFile
}

func deliveryItem(id, rolePair string, deps ...string) AddTaskInput {
	return AddTaskInput{
		ID:          id,
		Description: "D-33 fixture " + id,
		SpecRef:     "specs/feature.md",
		DoneWhen:    "reviewed",
		Scope:       "scope-" + id,
		Priority:    1,
		RolePair:    rolePair,
		DependsOn:   deps,
	}
}

// runDeliveryBatch adds items in one add-tasks request and checks that the
// results stay positional.
func runDeliveryBatch(t *testing.T, stateFile, logFile string, items ...AddTaskInput) *AddTasksResult {
	t.Helper()
	result, err := AddTasks(stateFile, logFile, &AddTasksInput{OrchestratorID: "orchestrator-1", Tasks: items})
	if err != nil {
		t.Fatalf("AddTasks() error: %v", err)
	}
	if len(result.Results) != len(items) {
		t.Fatalf("got %d results for %d items", len(result.Results), len(items))
	}
	for i, item := range items {
		if result.Results[i].TaskID != item.ID {
			t.Fatalf("result %d reports %q, want %q (results must stay positional)", i, result.Results[i].TaskID, item.ID)
		}
	}
	return result
}

func requireDeliveryTasks(t *testing.T, stateFile string, present bool, ids ...string) {
	t.Helper()
	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	for _, id := range ids {
		if got := state.FindTask(id) != nil; got != present {
			t.Errorf("task %s persisted = %v, want %v", id, got, present)
		}
	}
}

func requireResults(t *testing.T, result *AddTasksResult, success bool, ids ...string) {
	t.Helper()
	for _, r := range result.Results {
		for _, id := range ids {
			if r.TaskID == id && r.Success != success {
				t.Errorf("result for %s: success = %v (error %q), want %v", id, r.Success, r.Error, success)
			}
		}
	}
}

// loggedAdditions returns the task IDs of the activity log's task_added entries.
func loggedAdditions(t *testing.T, logFile string) []string {
	t.Helper()
	data, err := os.ReadFile(logFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read activity log: %v", err)
	}
	var entries []log.Entry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse activity log: %v", err)
	}
	var ids []string
	for _, entry := range entries {
		if entry.Action == "task_added" && entry.Task != nil {
			ids = append(ids, *entry.Task)
		}
	}
	return ids
}

func resultFor(t *testing.T, result *AddTasksResult, id string) AddTaskItemResult {
	t.Helper()
	for _, r := range result.Results {
		if r.TaskID == id {
			return r
		}
	}
	t.Fatalf("no result for %s", id)
	return AddTaskItemResult{}
}

// R1: add-task cannot carry a delivery task, so it refuses a dead-end story
// and says how to commission it.
func TestAddTask_RefusesDeadEndStoryAlone_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	input := deliveryItem("story", storyPair)
	_, err := AddTask(stateFile, logFile, &input, "orchestrator-1")

	if err == nil {
		t.Fatal("AddTask accepted a story that has no way to be delivered")
	}
	for _, want := range []string{"story", storyPair, "us-to-code-plan", deliveryPair, brand.Command("add-tasks")} {
		testhelpers.AssertErrorContains(t, err, want)
	}
	requireDeliveryTasks(t, stateFile, false, "story")
}

// R1 under a non-default brand: the refusal's command follows the brand (G1.3).
// Not parallel: it mutates the process-wide brand.
func TestAddTask_DeadEndRefusalFollowsBrand_D33(t *testing.T) {
	oldBinary := brand.BinaryName
	brand.BinaryName = "acme-cli"
	t.Cleanup(func() { brand.BinaryName = oldBinary })
	stateFile, logFile := setupDeliveryProject(t, false)

	input := deliveryItem("story", storyPair)
	_, err := AddTask(stateFile, logFile, &input, "orchestrator-1")

	testhelpers.AssertErrorContains(t, err, "acme-cli add-tasks")
	for _, leaked := range []string{"Liza", "LIZA", "liza"} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("non-default-brand refusal leaks %q: %v", leaked, err)
		}
	}
}

// R1b: the same story alone in an add-tasks request is refused.
func TestAddTasks_RefusesDeadEndStoryAlone_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile, deliveryItem("story", storyPair))

	requireResults(t, result, false, "story")
	if msg := resultFor(t, result, "story").Error; !strings.Contains(msg, deliveryPair) {
		t.Errorf("refusal %q does not name the delivery role pair %s", msg, deliveryPair)
	}
	requireDeliveryTasks(t, stateFile, false, "story")
}

// R2: a dependent outside the delivery target does not deliver the story.
func TestAddTasks_UnrelatedDependentIsNotADeliveryCompanion_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story", storyPair),
		deliveryItem("code", codingPair, "story"),
	)

	requireResults(t, result, false, "story")
	requireDeliveryTasks(t, stateFile, false, "story")
}

// R3: the story and its delivery task commit together or not at all.
func TestAddTasks_FailingCompanionLeavesNoStory_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story", storyPair),
		deliveryItem("plan", deliveryPair, "story", "missing-task"),
	)

	requireResults(t, result, false, "story", "plan")
	requireDeliveryTasks(t, stateFile, false, "story", "plan")
	if msg := resultFor(t, result, "story").Error; !strings.Contains(msg, "plan") {
		t.Errorf("story's failure %q does not attribute the group member that failed", msg)
	}
	if logged := loggedAdditions(t, logFile); len(logged) != 0 {
		t.Errorf("a failed group logged additions %v", logged)
	}
}

// R4: story plus delivery task in one request both persist.
func TestAddTasks_StoryWithDeliveryTaskPersists_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story", storyPair),
		deliveryItem("plan", deliveryPair, "story"),
	)

	requireResults(t, result, true, "story", "plan")
	requireDeliveryTasks(t, stateFile, true, "story", "plan")
	if logged := loggedAdditions(t, logFile); !slices.Equal(logged, []string{"story", "plan"}) {
		t.Errorf("activity log task_added entries = %v, want [story plan]", logged)
	}
}

// R5: under no_follow_up the delivery stage is intentionally suppressed, so a
// lone story is a legitimate terminal stage.
func TestAddTask_NoFollowUpAcceptsLoneStory_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, true)

	input := deliveryItem("story", storyPair)
	if _, err := AddTask(stateFile, logFile, &input, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask under no_follow_up: %v", err)
	}
	requireDeliveryTasks(t, stateFile, true, "story")
}

// R6: a failing group does not roll back unrelated items, and a failing
// unrelated item does not roll back a group.
func TestAddTasks_GroupAndUnrelatedItemsFailIndependently_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story-a", storyPair),
		deliveryItem("plan-a", deliveryPair, "story-a", "missing-task"),
		deliveryItem("code-ok", codingPair),
		deliveryItem("story-b", storyPair),
		deliveryItem("plan-b", deliveryPair, "story-b"),
		deliveryItem("code-bad", codingPair, "missing-task"),
	)

	requireResults(t, result, false, "story-a", "plan-a", "code-bad")
	requireResults(t, result, true, "code-ok", "story-b", "plan-b")
	requireDeliveryTasks(t, stateFile, false, "story-a", "plan-a", "code-bad")
	requireDeliveryTasks(t, stateFile, true, "code-ok", "story-b", "plan-b")
}

// R7: stages that can move on without a parent, or that are terminal, are
// accepted alone.
func TestAddTask_NonDeadEndStagesAcceptedAlone_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	for _, item := range []AddTaskInput{
		deliveryItem("plan", deliveryPair),
		deliveryItem("code", codingPair),
		deliveryItem("research", unrelPair),
	} {
		if _, err := AddTask(stateFile, logFile, &item, "orchestrator-1"); err != nil {
			t.Errorf("AddTask %s (%s): %v", item.ID, item.RolePair, err)
		}
	}
	requireDeliveryTasks(t, stateFile, true, "plan", "code", "research")
}

// R8: one delivery task covering two new stories forms one group.
func TestAddTasks_CompanionOfTwoStories_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story-1", storyPair),
		deliveryItem("story-2", storyPair),
		deliveryItem("plan", deliveryPair, "story-1", "story-2"),
	)

	requireResults(t, result, true, "story-1", "story-2", "plan")
	requireDeliveryTasks(t, stateFile, true, "story-1", "story-2", "plan")
}

// R9: an authority-fenced request with a stale generation persists nothing.
func TestAddTasks_StaleAuthorityPersistsNoGroup_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	stale := models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration + "-stale"}
	_, err := AddTasksWithAuthority(stateFile, logFile, &AddTasksInput{Tasks: []AddTaskInput{
		deliveryItem("story", storyPair),
		deliveryItem("plan", deliveryPair, "story"),
	}}, stale)

	if !IsAgentAuthorityError(err) {
		t.Fatalf("stale authority: got %v, want an agent authority error", err)
	}
	requireDeliveryTasks(t, stateFile, false, "story", "plan")
}

// R10: an ordinary prerequisite listed between the story and its delivery
// task is committed before the group.
func TestAddTasks_PrerequisiteBetweenStoryAndCompanion_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story", storyPair),
		deliveryItem("prereq", deliveryPair),
		deliveryItem("plan", deliveryPair, "story", "prereq"),
	)

	requireResults(t, result, true, "story", "prereq", "plan")
	requireDeliveryTasks(t, stateFile, true, "story", "prereq", "plan")
}

// R11: interleaved groups where one delivery task depends on the other group.
func TestAddTasks_InterleavedGroups_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story-1", storyPair),
		deliveryItem("story-2", storyPair),
		deliveryItem("plan-1", deliveryPair, "story-1", "plan-2"),
		deliveryItem("plan-2", deliveryPair, "story-2"),
	)

	requireResults(t, result, true, "story-1", "story-2", "plan-1", "plan-2")
	requireDeliveryTasks(t, stateFile, true, "story-1", "story-2", "plan-1", "plan-2")
}

// R12: a dependent of the story that no group member needs commits after the
// group, outside it.
func TestAddTasks_DependentOutsideGroupCommitsAfterIt_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story", storyPair),
		deliveryItem("research", unrelPair, "story"),
		deliveryItem("plan", deliveryPair, "story"),
	)

	requireResults(t, result, true, "story", "research", "plan")
	requireDeliveryTasks(t, stateFile, true, "story", "research", "plan")
}

// R13: a request with no dead-end story keeps today's item loop, including
// the failure of a forward reference.
func TestAddTasks_NoGroupKeepsRequestOrder_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("code-b", codingPair, "code-c"),
		deliveryItem("code-c", codingPair),
	)

	requireResults(t, result, false, "code-b")
	requireResults(t, result, true, "code-c")
	requireDeliveryTasks(t, stateFile, false, "code-b")
	requireDeliveryTasks(t, stateFile, true, "code-c")
}

// R14': an unrelated item on the path from the story to its delivery task
// joins the group.
func TestAddTasks_PathItemJoinsGroup_D33(t *testing.T) {
	t.Parallel()
	t.Run("all succeed", func(t *testing.T) {
		t.Parallel()
		stateFile, logFile := setupDeliveryProject(t, false)

		result := runDeliveryBatch(t, stateFile, logFile,
			deliveryItem("story", storyPair),
			deliveryItem("research", unrelPair, "story"),
			deliveryItem("plan", deliveryPair, "story", "research"),
		)

		requireResults(t, result, true, "story", "research", "plan")
		requireDeliveryTasks(t, stateFile, true, "story", "research", "plan")
	})
	t.Run("path item fails the whole group", func(t *testing.T) {
		t.Parallel()
		stateFile, logFile := setupDeliveryProject(t, false)

		result := runDeliveryBatch(t, stateFile, logFile,
			deliveryItem("story", storyPair),
			deliveryItem("research", unrelPair, "story", "missing-task"),
			deliveryItem("plan", deliveryPair, "story", "research"),
		)

		requireResults(t, result, false, "story", "research", "plan")
		requireDeliveryTasks(t, stateFile, false, "story", "research", "plan")
	})
}

// R15': two groups that each need an item built on the other's story are
// one atomic unit.
func TestAddTasks_CrossGroupCycleIsOneUnit_D33(t *testing.T) {
	t.Parallel()
	items := func(yDeps ...string) []AddTaskInput {
		return []AddTaskInput{
			deliveryItem("story-1", storyPair),
			deliveryItem("story-2", storyPair),
			deliveryItem("research-x", unrelPair, "story-1"),
			deliveryItem("research-y", unrelPair, yDeps...),
			deliveryItem("plan-1", deliveryPair, "story-1", "research-y"),
			deliveryItem("plan-2", deliveryPair, "story-2", "research-x"),
		}
	}
	all := []string{"story-1", "story-2", "research-x", "research-y", "plan-1", "plan-2"}
	t.Run("all succeed", func(t *testing.T) {
		t.Parallel()
		stateFile, logFile := setupDeliveryProject(t, false)

		result := runDeliveryBatch(t, stateFile, logFile, items("story-2")...)

		requireResults(t, result, true, all...)
		requireDeliveryTasks(t, stateFile, true, all...)
	})
	t.Run("one member fails all six", func(t *testing.T) {
		t.Parallel()
		stateFile, logFile := setupDeliveryProject(t, false)

		result := runDeliveryBatch(t, stateFile, logFile, items("story-2", "missing-task")...)

		requireResults(t, result, false, all...)
		requireDeliveryTasks(t, stateFile, false, all...)
	})
}

// R16: a one-way dependency between groups keeps them separate units.
func TestAddTasks_OneWayGroupDependencyStaysSeparate_D33(t *testing.T) {
	t.Parallel()
	stateFile, logFile := setupDeliveryProject(t, false)

	result := runDeliveryBatch(t, stateFile, logFile,
		deliveryItem("story-1", storyPair),
		deliveryItem("story-2", storyPair),
		deliveryItem("plan-1", deliveryPair, "story-1"),
		deliveryItem("plan-2", deliveryPair, "story-2", "plan-1", "missing-task"),
	)

	requireResults(t, result, true, "story-1", "plan-1")
	requireResults(t, result, false, "story-2", "plan-2")
	requireDeliveryTasks(t, stateFile, true, "story-1", "plan-1")
	requireDeliveryTasks(t, stateFile, false, "story-2", "plan-2")
}
