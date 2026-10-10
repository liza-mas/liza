package commands

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const slotReviewerList = "roles:\n  code-reviewer:\n    - {cli: claude, model: m1}\n    - {cli: codex, model: m2}\n"

func slotReviewerFile(t *testing.T) rolemodels.File {
	t.Helper()
	f, err := rolemodels.Parse([]byte(slotReviewerList))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// writeSlotRepairProject writes state under the default pipeline with a
// coding-pair quorum of 2, so a half-approved task awaits its second review.
func writeSlotRepairProject(t *testing.T, state *models.State) string {
	t.Helper()
	root := writeRepairAgentPoolState(t, state)
	policy := []byte("task-slug: code\n      review-policy:\n        quorum: 1\n")
	content := embedded.PipelineConfigContent()
	if bytes.Count(content, policy) != 1 {
		t.Fatal("fixture: coding-pair review policy not found in the default pipeline")
	}
	testhelpers.SetupPipelineConfigBytes(t, root, bytes.Replace(content, policy, bytes.Replace(policy, []byte("quorum: 1"), []byte("quorum: 2"), 1), 1))
	return root
}

func loadSlotResolver(t *testing.T, max int) slotPoolResolver {
	t.Helper()
	pr, err := ops.LoadResolverForModels(writeSlotRepairProject(t, testhelpers.CreateValidState()))
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}
	return slotPoolResolver{PipelineResolver: pr, max: max}
}

func secondReviewTask(id string, now time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusPartiallyApproved, now)
	task.Approvals = []models.Approval{{Agent: "code-reviewer-9", Provider: "claude", Model: "m1", Timestamp: now}}
	task.ReviewCommit = testhelpers.StringPtr("review123")
	return task
}

// slotPoolResolver fixes the reviewer role's max-instances and applies no
// provider diversity, so only the slot binding decides eligibility.
type slotPoolResolver struct {
	models.PipelineResolver
	max int
}

func (r slotPoolResolver) MaxInstances(string) (int, error)                 { return r.max, nil }
func (r slotPoolResolver) ProviderDiversity(string, string) (string, error) { return "", nil }

// A fresh task and a half-approved one need slots 1 and 2: pool repair starts
// one reviewer per item, under distinct IDs from the role's shared allocator,
// without --cli so each resolves its item, model included.
func TestRepairAgentPool_ReviewSlotsStartTheirItems(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now),
		secondReviewTask("review-second", now),
	}
	projectRoot := writeSlotRepairProject(t, state)
	if err := os.WriteFile(paths.New(projectRoot).ModelsPath(), []byte(slotReviewerList), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: projectRoot, Missing: true})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	want := []spawnedAgentCall{
		{role: "code-reviewer", cli: "claude", agentID: "code-reviewer-1", fromConfig: true, modelsItem: 1},
		{role: "code-reviewer", cli: "codex", agentID: "code-reviewer-2", fromConfig: true, modelsItem: 2},
	}
	for i := range calls {
		calls[i].projectRoot = ""
	}
	slices.SortFunc(calls, func(a, b spawnedAgentCall) int { return a.modelsItem - b.modelsItem })
	if !slices.Equal(calls, want) {
		t.Fatalf("spawn calls = %+v, want %+v", calls, want)
	}
	for _, command := range []string{
		brand.BinaryName + " agent code-reviewer --models-item 1 --agent-id code-reviewer-1",
		brand.BinaryName + " agent code-reviewer --models-item 2 --agent-id code-reviewer-2",
	} {
		if !slices.Contains(result.Commands, command) {
			t.Errorf("commands = %v, want %q", result.Commands, command)
		}
	}
}

// An idle reviewer running another slot's entry does not cover the task.
// With the role limit full it holds the only slot, so nothing starts; once it
// leaves (its idle exit), the matching item is planned.
func TestFindRoleCapacityDeficits_WrongSlotReviewerDoesNotCover(t *testing.T) {
	now := time.Now().UTC()
	pr := loadSlotResolver(t, 1)
	wrongSlot := repairReviewerAgent("code-reviewer", "codex")
	wrongSlot.Model = "m2"
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{"code-reviewer-1": wrongSlot}
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now)}
	repairCLI := RepairCLI{RoleModels: slotReviewerFile(t)}

	missing, unservable := FindRoleCapacityDeficits(state, pr, repairCLI, nil, now)
	if len(missing) != 0 || len(unservable) != 0 {
		t.Fatalf("role limit full: missing = %+v, unservable = %+v; want nothing started", missing, unservable)
	}

	pr.max = 2
	missing, _ = FindRoleCapacityDeficits(state, pr, repairCLI, nil, now)
	if len(missing) != 1 || missing[0].TaskCount != 1 || !slices.Equal(missing[0].Items, []int{1}) || !slices.Equal(missing[0].AgentIDs, []string{"code-reviewer-2"}) {
		t.Fatalf("headroom left: missing = %+v, want one item-1 start as code-reviewer-2", missing)
	}

	pr.max = 1
	delete(state.Agents, "code-reviewer-1")
	missing, _ = FindRoleCapacityDeficits(state, pr, repairCLI, nil, now)
	if len(missing) != 1 || !slices.Equal(missing[0].Items, []int{1}) || !slices.Equal(missing[0].AgentIDs, []string{"code-reviewer-1"}) {
		t.Fatalf("after the wrong-slot reviewer left: missing = %+v, want one item-1 start", missing)
	}
}

// A live reviewer whose agent lease a verdict or release cleared until its
// next heartbeat (operator note D-17 part B) holds its slot and its ID: the
// start planner neither exceeds the role limit nor reuses that ID.
func TestFindRoleCapacityDeficits_LiveLeaseLessReviewerKeepsSlotAndID(t *testing.T) {
	now := time.Now().UTC()
	leaseLess := repairReviewerAgent("code-reviewer", "codex")
	leaseLess.Model = "m2"
	leaseLess.LeaseExpires = nil
	leaseLess.Heartbeat = now.Add(-10 * time.Second)
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{"code-reviewer-1": leaseLess}
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now)}
	repairCLI := RepairCLI{RoleModels: slotReviewerFile(t)}

	t.Run("role limit full", func(t *testing.T) {
		missing, unservable := FindRoleCapacityDeficits(state, loadSlotResolver(t, 1), repairCLI, nil, now)
		if len(missing) != 0 || len(unservable) != 0 {
			t.Fatalf("missing = %+v, unservable = %+v; want nothing started", missing, unservable)
		}
	})
	t.Run("headroom left", func(t *testing.T) {
		missing, _ := FindRoleCapacityDeficits(state, loadSlotResolver(t, 2), repairCLI, nil, now)
		if len(missing) != 1 || !slices.Equal(missing[0].AgentIDs, []string{"code-reviewer-2"}) {
			t.Fatalf("missing = %+v, want one start as code-reviewer-2", missing)
		}
	})
}

// An idle reviewer running the task's slot entry covers it.
func TestFindRoleCapacityDeficits_MatchingSlotReviewerCovers(t *testing.T) {
	now := time.Now().UTC()
	pr := loadSlotResolver(t, 2)
	matching := repairReviewerAgent("code-reviewer", "codex")
	matching.Model = "m2"
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{"code-reviewer-1": matching}
	state.Tasks = []models.Task{secondReviewTask("review-second", now)}

	missing, _ := FindRoleCapacityDeficits(state, pr, RepairCLI{RoleModels: slotReviewerFile(t)}, nil, now)
	if len(missing) != 0 {
		t.Fatalf("missing = %+v, want the idle slot-2 reviewer to cover the second review", missing)
	}
	delete(state.Agents, "code-reviewer-1")
	if missing, _ = FindRoleCapacityDeficits(state, pr, RepairCLI{RoleModels: slotReviewerFile(t)}, nil, now); len(missing) != 1 || !slices.Equal(missing[0].Items, []int{2}) {
		t.Fatalf("fixture: without the reviewer, missing = %+v, want one item-2 start", missing)
	}
}

// An explicit --cli starts every reviewer on that CLI; work its registration
// cannot take under the slot binding is reported per item as unservable.
func TestFindRoleCapacityDeficits_ExplicitCLIOutsideSlotIsUnservable(t *testing.T) {
	now := time.Now().UTC()
	pr := loadSlotResolver(t, 2)
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{}
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now)}

	missing, unservable := FindRoleCapacityDeficits(state, pr, RepairCLI{Explicit: "codex", RoleModels: slotReviewerFile(t)}, nil, now)
	if len(missing) != 0 || len(unservable) != 1 || unservable[0].CLI != "codex" || unservable[0].Item != 0 {
		t.Fatalf("missing = %+v, unservable = %+v; want the fresh task unservable with --cli codex", missing, unservable)
	}
}

// A registered reviewer bound to another slot does not keep the role from
// being reported missing for the task.
func TestFindMissingRolesWithClaimableWork_HonorsReviewSlots(t *testing.T) {
	now := time.Now().UTC()
	pr := loadSlotResolver(t, 2)
	wrongSlot := repairReviewerAgent("code-reviewer", "codex")
	wrongSlot.Model = "m2"
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{"code-reviewer-1": wrongSlot}
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now)}

	if missing := FindMissingRolesWithClaimableWork(state, pr, rolemodels.File{}); len(missing) != 0 {
		t.Fatalf("unbound: missing = %+v, want the registered reviewer to cover", missing)
	}
	missing := FindMissingRolesWithClaimableWork(state, pr, slotReviewerFile(t))
	if len(missing) != 1 || missing[0].Role != "code-reviewer" {
		t.Fatalf("bound: missing = %+v, want code-reviewer reported", missing)
	}
}

func writeSlotRepairFixture(t *testing.T, maxInstances int, tasks ...models.Task) string {
	t.Helper()
	state := testhelpers.CreateValidState()
	state.Config.MaxInstances = maxInstances
	state.Tasks = tasks
	root := writeSlotRepairProject(t, state)
	if err := os.WriteFile(paths.New(root).ModelsPath(), []byte(slotReviewerList), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// A failed start stalls only its own list item: another item of the same
// role still starts, and the failure is reported.
func TestRepairAgentPool_FailedItemDoesNotBlockOtherItems(t *testing.T) {
	now := time.Now().UTC()
	root := writeSlotRepairFixture(t, 2,
		testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now),
		secondReviewTask("review-second", now))
	var calls []spawnedAgentCall
	original := repairAgentPoolSpawn
	repairAgentPoolSpawn = func(_, role, cli, agentID string, fromConfig bool, modelsItem int) (int, error) {
		calls = append(calls, spawnedAgentCall{role: role, cli: cli, agentID: agentID, fromConfig: fromConfig, modelsItem: modelsItem})
		if cli == "claude" {
			return 0, errors.New("claude: executable not found")
		}
		return 12345, nil
	}
	t.Cleanup(func() { repairAgentPoolSpawn = original })

	result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, Missing: true})
	if err == nil {
		t.Fatal("RepairAgentPool() error = nil, want the item-1 start failure reported")
	}
	if len(result.Failed) != 1 || result.Failed[0].CLI != "claude" {
		t.Fatalf("failed = %+v, want the claude item-1 start", result.Failed)
	}
	if len(result.Spawned) != 1 || result.Spawned[0].Item != 2 || result.Spawned[0].CLI != "codex" {
		t.Fatalf("spawned = %+v, want the codex item-2 start despite the item-1 failure", result.Spawned)
	}
	if len(calls) != 2 {
		t.Fatalf("spawn calls = %+v, want both items attempted", calls)
	}
}

// A pending reviewer covers demand for the item it was started with only, and
// takes one of the role's starts either way.
func TestRepairAgentPool_PendingItemCoversOnlyItsItem(t *testing.T) {
	now := time.Now().UTC()
	root := writeSlotRepairFixture(t, 2,
		testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now),
		secondReviewTask("review-second", now))
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	_, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot:     root,
		Missing:         true,
		PendingSpawns:   map[string][]int{"code-reviewer": {1}},
		PendingAgentIDs: map[string]bool{"code-reviewer-1": true},
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(calls) != 1 || calls[0].modelsItem != 2 || calls[0].agentID == "code-reviewer-1" {
		t.Fatalf("spawn calls = %+v, want only the item-2 start under a fresh ID", calls)
	}

	// A pending start for an item nothing needs still takes the last start.
	calls = nil
	if _, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, Missing: true, PendingSpawns: map[string][]int{"code-reviewer": {3}}}); err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(calls) != 1 || calls[0].modelsItem != 1 {
		t.Fatalf("spawn calls = %+v, want one start within the headroom the pending one leaves", calls)
	}
}

// The summary names a CLI only when the role's starts share it.
func TestRepairAgentPool_SummaryCLIFollowsItemStarts(t *testing.T) {
	now := time.Now().UTC()
	root := writeSlotRepairFixture(t, 2, secondReviewTask("review-second", now))
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)
	result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, Missing: true, DryRun: true})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0].CLI != "codex" || result.RoleCLIs["code-reviewer"] != "codex" || result.CLI != "codex" {
		t.Fatalf("item-2 only: missing = %+v, role CLIs = %v, cli = %q; want codex throughout", result.Missing, result.RoleCLIs, result.CLI)
	}

	root = writeSlotRepairFixture(t, 2,
		testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now),
		secondReviewTask("review-second", now))
	result, err = RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, Missing: true, DryRun: true})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0].CLI != "" || result.CLI != "" {
		t.Fatalf("mixed items: missing = %+v, cli = %q; want no single CLI claimed", result.Missing, result.CLI)
	}
	if _, ok := result.RoleCLIs["code-reviewer"]; ok {
		t.Fatalf("mixed items: role CLIs = %v, want the role left unnamed", result.RoleCLIs)
	}
}
