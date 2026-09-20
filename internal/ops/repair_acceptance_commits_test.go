package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// rebasedPlanFixture builds the D11 shape: a plan commit merged on integration,
// then integration rewritten so the plan's commits are content-identical but
// unreachable. Returns the orphaned (base, review) SHAs and their rewritten
// replacements.
type rebasedPlanFixture struct {
	root               string
	oldBase, oldReview string
	newBase, newReview string
	carrier            string
}

func buildRebasedPlanFixture(t *testing.T) rebasedPlanFixture {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	git := func(args ...string) string { return testhelpers.MustGit(t, root, args...) }

	git("checkout", "-q", "integration")
	origin := git("rev-parse", "HEAD")

	// The parent's base is itself a commit on integration, so a rebase
	// rewrites it too — exactly the shape the acceptance check trips on.
	if err := os.WriteFile(filepath.Join(root, "BASE.md"), []byte("master plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "BASE.md")
	git("commit", "-q", "-m", "docs(planning): master plan the code plan builds on")
	oldBase := git("rev-parse", "HEAD")

	carrier := filepath.Join("specs", "plans", "cp-0.md")
	if err := os.MkdirAll(filepath.Join(root, "specs", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, carrier), []byte("# Code plan\n\nTask 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", carrier)
	git("commit", "-q", "-m", "docs(planning): code plan for output 0")
	oldReview := git("rev-parse", "HEAD")

	// Rewrite integration: same content, new object ids. An unrelated commit is
	// inserted under the plan, which is what a rebase onto a moved base does.
	git("checkout", "-q", "-b", "rewritten", origin)
	if err := os.WriteFile(filepath.Join(root, "UNRELATED.md"), []byte("moved base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "UNRELATED.md")
	git("commit", "-q", "-m", "chore: unrelated change that moved the base")
	git("cherry-pick", oldBase)
	newBase := git("rev-parse", "HEAD")
	git("cherry-pick", oldReview)
	newReview := git("rev-parse", "HEAD")
	git("branch", "-f", "integration", "rewritten")
	git("checkout", "-q", "integration")

	return rebasedPlanFixture{root: root, oldBase: oldBase, oldReview: oldReview, newBase: newBase, newReview: newReview, carrier: carrier}
}

func mergedPlanningParent(id string, base, review, merge string, now time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
	task.Type = models.TaskTypePlanning
	task.BaseCommit = &base
	task.ReviewCommit = &review
	task.MergeCommit = &merge
	agent := "code-planner-1"
	submitted := review
	task.History = append(task.History, models.TaskHistoryEntry{
		Time: now, Event: string(models.TaskEventSubmittedForReview), Agent: &agent, Commit: &submitted,
	})
	return task
}

func TestRepairAcceptanceCommits_RemapsOrphanedParentByContentIdentity(t *testing.T) {
	fx := buildRebasedPlanFixture(t)
	statePath, _ := testhelpers.SetupLizaDir(t, fx.root)
	testhelpers.SetupPipelineConfig(t, fx.root)

	now := time.Now().UTC()
	parent := mergedPlanningParent("cp-0", fx.oldBase, fx.oldReview, fx.oldReview, now)
	child := testhelpers.BuildTaskByStatus("cp-0-code-0", models.TaskStatusReady, now)
	child.ParentTasks = []string{"cp-0"}

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{parent, child}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	result, err := RepairAcceptanceCommits(bb, fx.root, "integration")
	if err != nil {
		t.Fatalf("RepairAcceptanceCommits: %v", err)
	}
	if len(result.Repaired) != 1 || result.Repaired[0].TaskID != "cp-0" {
		t.Fatalf("Repaired = %+v, want exactly cp-0", result.Repaired)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want none", result.Skipped)
	}

	updated, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	got := updated.FindTask("cp-0")
	if *got.BaseCommit != fx.newBase {
		t.Fatalf("BaseCommit = %s, want rewritten base %s", *got.BaseCommit, fx.newBase)
	}
	if *got.ReviewCommit != fx.newReview || *got.MergeCommit != fx.newReview {
		t.Fatalf("Review/Merge = %s/%s, want both %s", *got.ReviewCommit, *got.MergeCommit, fx.newReview)
	}
	var submitted *string
	for _, entry := range got.History {
		if entry.Event == string(models.TaskEventSubmittedForReview) {
			submitted = entry.Commit
		}
	}
	if submitted == nil || *submitted != fx.newReview {
		t.Fatalf("submitted_for_review history commit = %v, want %s so the parent's author stays recoverable", submitted, fx.newReview)
	}
}

func TestRepairAcceptanceCommits_ReachableParentIsUntouched(t *testing.T) {
	fx := buildRebasedPlanFixture(t)
	statePath, _ := testhelpers.SetupLizaDir(t, fx.root)
	testhelpers.SetupPipelineConfig(t, fx.root)

	now := time.Now().UTC()
	parent := mergedPlanningParent("cp-healthy", fx.newBase, fx.newReview, fx.newReview, now)
	child := testhelpers.BuildTaskByStatus("cp-healthy-code-0", models.TaskStatusReady, now)
	child.ParentTasks = []string{"cp-healthy"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{parent, child}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	result, err := RepairAcceptanceCommits(bb, fx.root, "integration")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repaired) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("healthy state must be a no-op, got repaired=%+v skipped=%v", result.Repaired, result.Skipped)
	}
}

func TestRepairAcceptanceCommits_RefusesWhenNoIdenticalReplacementExists(t *testing.T) {
	fx := buildRebasedPlanFixture(t)
	statePath, _ := testhelpers.SetupLizaDir(t, fx.root)
	testhelpers.SetupPipelineConfig(t, fx.root)

	// A commit whose content never made it onto integration in any form.
	git := func(args ...string) string { return testhelpers.MustGit(t, fx.root, args...) }
	git("checkout", "-q", "-b", "lost", fx.oldBase)
	if err := os.WriteFile(filepath.Join(fx.root, "LOST.md"), []byte("never integrated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "LOST.md")
	git("commit", "-q", "-m", "docs: content that was never integrated")
	lost := git("rev-parse", "HEAD")
	git("checkout", "-q", "integration")

	now := time.Now().UTC()
	parent := mergedPlanningParent("cp-lost", fx.oldBase, lost, lost, now)
	child := testhelpers.BuildTaskByStatus("cp-lost-code-0", models.TaskStatusReady, now)
	child.ParentTasks = []string{"cp-lost"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{parent, child}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	result, err := RepairAcceptanceCommits(bb, fx.root, "integration")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repaired) != 0 {
		t.Fatalf("must not repair a parent with no content-identical replacement, got %+v", result.Repaired)
	}
	if _, skipped := result.Skipped["cp-lost"]; !skipped {
		t.Fatalf("Skipped = %v, want cp-lost with a reason", result.Skipped)
	}
	updated, _ := bb.Read()
	if *updated.FindTask("cp-lost").ReviewCommit != lost {
		t.Fatal("a refused parent must be left exactly as it was")
	}
}

func TestRepairAcceptanceCommits_DroppedMergeCommitResolvesToReviewedReplacement(t *testing.T) {
	fx := buildRebasedPlanFixture(t)
	statePath, _ := testhelpers.SetupLizaDir(t, fx.root)
	testhelpers.SetupPipelineConfig(t, fx.root)

	// Build the real merge commit the old integration would have had, then
	// leave it orphaned: it has no diff of its own, so it cannot be matched.
	git := func(args ...string) string { return testhelpers.MustGit(t, fx.root, args...) }
	git("checkout", "-q", "-b", "old-integration", fx.oldBase)
	git("merge", "--no-ff", "-m", "Merge cp-0", fx.oldReview)
	oldMerge := git("rev-parse", "HEAD")
	git("checkout", "-q", "integration")

	now := time.Now().UTC()
	parent := mergedPlanningParent("cp-0", fx.oldBase, fx.oldReview, oldMerge, now)
	child := testhelpers.BuildTaskByStatus("cp-0-code-0", models.TaskStatusReady, now)
	child.ParentTasks = []string{"cp-0"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{parent, child}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	result, err := RepairAcceptanceCommits(bb, fx.root, "integration")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repaired) != 1 {
		t.Fatalf("Repaired = %+v, want cp-0; skipped=%v", result.Repaired, result.Skipped)
	}
	updated, _ := bb.Read()
	got := updated.FindTask("cp-0")
	if *got.MergeCommit != fx.newReview {
		t.Fatalf("MergeCommit = %s, want the reviewed commit's replacement %s (where the work entered integration)", *got.MergeCommit, fx.newReview)
	}
}

func TestPlanAcceptanceCommitRepair_WritesNothing(t *testing.T) {
	fx := buildRebasedPlanFixture(t)
	statePath, _ := testhelpers.SetupLizaDir(t, fx.root)
	testhelpers.SetupPipelineConfig(t, fx.root)

	now := time.Now().UTC()
	parent := mergedPlanningParent("cp-0", fx.oldBase, fx.oldReview, fx.oldReview, now)
	child := testhelpers.BuildTaskByStatus("cp-0-code-0", models.TaskStatusReady, now)
	child.ParentTasks = []string{"cp-0"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{parent, child}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	result, err := PlanAcceptanceCommitRepair(bb, fx.root, "integration")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repaired) != 1 || result.Repaired[0].Replaced[fx.oldReview] != fx.newReview {
		t.Fatalf("plan must report the mapping it would apply, got %+v", result.Repaired)
	}
	updated, _ := bb.Read()
	if *updated.FindTask("cp-0").ReviewCommit != fx.oldReview {
		t.Fatal("dry run must not modify state")
	}
}

// The property the command exists for: a child stranded by an integration
// rewrite is refused before the repair and allocated after it. This drives the
// real submission path rather than asserting which SHAs landed in fields.
func TestRepairAcceptanceCommits_ChildAllocatesAfterRepair(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	git := func(args ...string) string { return testhelpers.MustGit(t, root, args...) }

	before := readAcceptanceState(t, bb)
	parent := before.FindTask("acceptance-parent")
	oldBase, oldReview := *parent.BaseCommit, *parent.ReviewCommit

	// Rewrite integration the way a rebase does: an unrelated commit under the
	// parent's work, then the same two commits replayed with new ids.
	git("checkout", "-q", "integration")
	git("checkout", "-q", "-b", "rewritten", oldBase+"~1")
	if err := os.WriteFile(filepath.Join(root, "UNRELATED.md"), []byte("moved base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "UNRELATED.md")
	git("commit", "-q", "-m", "chore: unrelated change that moved the base")
	git("cherry-pick", oldBase)
	git("cherry-pick", oldReview)
	git("branch", "-f", "integration", "rewritten")
	git("checkout", "-q", "integration")

	// D11 reproduced: the parent's evidence no longer reaches integration.
	requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, "acceptance.source")

	result, err := RepairAcceptanceCommits(bb, root, "integration")
	if err != nil {
		t.Fatalf("RepairAcceptanceCommits: %v", err)
	}
	if len(result.Repaired) != 1 || result.Repaired[0].TaskID != "acceptance-parent" {
		t.Fatalf("Repaired = %+v (skipped %v), want the stranded parent", result.Repaired, result.Skipped)
	}

	// The same submission now clears allocation.
	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatalf("SubmitForReview after repair: %v", err)
	}
	after := readAcceptanceState(t, bb)
	child := after.FindTask(taskID)
	if child.AcceptanceSource == nil || child.AcceptanceSource.ParentTask != "acceptance-parent" {
		t.Fatalf("child AcceptanceSource = %+v, want allocation from the repaired parent", child.AcceptanceSource)
	}
	repaired := after.FindTask("acceptance-parent")
	if child.AcceptanceSource.ParentReviewCommit != *repaired.ReviewCommit {
		t.Fatalf("child names parent review %s, parent records %s", child.AcceptanceSource.ParentReviewCommit, *repaired.ReviewCommit)
	}

	// The rewrite is on the record, with the pairs an auditor needs.
	var audit *models.TaskHistoryEntry
	for i := range repaired.History {
		if repaired.History[i].Event == string(models.TaskEventAcceptanceCommitsRemapped) {
			audit = &repaired.History[i]
		}
	}
	if audit == nil {
		t.Fatal("no acceptance_commits_remapped history entry on the repaired parent")
	}
	replaced, _ := audit.Extra["replaced"].(map[string]any)
	if replaced[oldReview] != *repaired.ReviewCommit {
		t.Fatalf("audit entry replaced[%s] = %v, want %s", oldReview, replaced[oldReview], *repaired.ReviewCommit)
	}
}
