package ops

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	repairArchDoc     = "specs/architecture/scope-arch.md"
	repairArchHeading = "Scope 0: Authoritative version-number reads"
	repairArchRef     = repairArchDoc + "#" + repairArchHeading
)

// setupRepairArchRefTest commits an architecture artifact to the integration
// branch only, so the repair must read it there, never from the working tree.
func setupRepairArchRefTest(t *testing.T, tasks ...models.Task) (string, string) {
	t.Helper()
	root, statePath, _ := setupProviderOpsTest(t, tasks...)
	testhelpers.MustGit(t, root, "checkout", "-q", "integration")
	writeAndCommit(t, root, repairArchDoc, "# Architecture\n\n## "+repairArchHeading+"\n\nReads.\n\n## Scope 1: Ambiguous\n\nOne.\n\n## Scope 1: Ambiguous\n\nTwo.\n", "add architecture")
	testhelpers.MustGit(t, root, "checkout", "-q", "main")
	return root, statePath
}

// unstartedCodePlan is a never-claimed code plan in its initial status whose
// arch_ref was lost, the D-76 shape (a pre-D-67 replacement).
func unstartedCodePlan(id string) models.Task {
	return providerOpsTask(id, "code-planning-pair", models.TaskStatusDraftCodingPlan)
}

func TestRepairArchRefSetsEmptyArchRefOnUnstartedTask(t *testing.T) {
	t.Parallel()
	// GIVEN an unstarted code plan with an empty arch_ref
	root, statePath := setupRepairArchRefTest(t, unstartedCodePlan("cp-0-replan-1"))

	// WHEN the operator repairs it with an exact Scope heading on integration
	result, err := RepairArchRef(root, "cp-0-replan-1", repairArchRef, "restore the reviewed Scope 0 lost by replacement")
	if err != nil {
		t.Fatalf("RepairArchRef: %v", err)
	}

	// THEN the task carries the ref and an attributable history entry
	task := mustReadTask(t, statePath, "cp-0-replan-1")
	if task.ArchRef != repairArchRef {
		t.Fatalf("arch_ref = %q, want %q", task.ArchRef, repairArchRef)
	}
	if task.Status != models.TaskStatusDraftCodingPlan || task.AssignedTo != nil {
		t.Fatalf("repair changed lifecycle: status=%s assigned=%v", task.Status, task.AssignedTo)
	}
	if len(task.History) != 1 || task.History[0].Event != string(models.TaskEventArchRefRepaired) {
		t.Fatalf("history = %+v, want one arch_ref_repaired entry", task.History)
	}
	entry := task.History[0]
	integration := strings.TrimSpace(testhelpers.MustGit(t, root, "rev-parse", "integration"))
	if entry.Reason == nil || *entry.Reason != "restore the reviewed Scope 0 lost by replacement" ||
		entry.Extra["arch_ref"] != repairArchRef || entry.Extra["integration"] != integration {
		t.Fatalf("history entry = %+v, want reason, arch_ref and integration %s", entry, integration)
	}
	if result == nil || result.TaskID != "cp-0-replan-1" || result.ArchRef != repairArchRef || result.Integration != integration || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v", result)
	}

	// AND the activity log records it
	entries, err := log.New(paths.New(root).LogPath()).Read()
	if err != nil {
		t.Fatal(err)
	}
	logged := false
	for _, e := range entries {
		logged = logged || e.Action == "arch_ref_repaired" && e.Task != nil && *e.Task == "cp-0-replan-1" && strings.Contains(e.Detail, repairArchRef)
	}
	if !logged {
		t.Fatalf("no arch_ref_repaired activity entry in %+v", entries)
	}
}

// The initial status comes from the task's role pair, not a code-plan constant.
func TestRepairArchRefUsesRolePairInitialStatus(t *testing.T) {
	t.Parallel()
	coding := providerOpsTask("code-0", "coding-pair", models.TaskStatusReady)
	root, statePath := setupRepairArchRefTest(t, coding)
	if _, err := RepairArchRef(root, "code-0", repairArchRef, "restore scope"); err != nil {
		t.Fatalf("RepairArchRef on an initial coding task: %v", err)
	}
	if got := mustReadTask(t, statePath, "code-0").ArchRef; got != repairArchRef {
		t.Fatalf("arch_ref = %q, want %q", got, repairArchRef)
	}
}

func TestRepairArchRefRefusals(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	agent := "code-planner-1"
	for _, tc := range []struct {
		name    string
		task    func() models.Task
		taskID  string
		archRef string
		reason  string
		want    []string
	}{
		{name: "non-empty arch_ref", task: func() models.Task {
			task := unstartedCodePlan("t")
			task.ArchRef = repairArchDoc + "#Scope 1: Other"
			return task
		}, archRef: repairArchRef, want: []string{"already has arch_ref", "Scope 1: Other"}},
		{name: "claimed once", task: func() models.Task {
			task := unstartedCodePlan("t")
			task.History = []models.TaskHistoryEntry{{Time: now, Event: string(models.TaskEventClaimed), Agent: &agent}}
			return task
		}, archRef: repairArchRef, want: []string{"unstarted"}},
		{name: "blocked", task: func() models.Task {
			task := providerOpsTask("t", "code-planning-pair", models.TaskStatusBlocked)
			reason := "waiting"
			task.BlockedReason = &reason
			return task
		}, archRef: repairArchRef, want: []string{"BLOCKED", string(models.TaskStatusDraftCodingPlan)}},
		{name: "missing fragment", task: func() models.Task { return unstartedCodePlan("t") }, archRef: repairArchDoc, want: []string{"exact Scope heading"}},
		{name: "slug fragment", task: func() models.Task { return unstartedCodePlan("t") }, archRef: repairArchDoc + "#scope-0-authoritative-version-number-reads", want: []string{"is missing", "not a slug"}},
		{name: "duplicate heading", task: func() models.Task { return unstartedCodePlan("t") }, archRef: repairArchDoc + "#Scope 1: Ambiguous", want: []string{"ambiguous"}},
		{name: "file absent at integration", task: func() models.Task { return unstartedCodePlan("t") }, archRef: "specs/architecture/absent.md#Scope 0", want: []string{"absent.md", "integration"}},
		{name: "unknown task", task: func() models.Task { return unstartedCodePlan("t") }, taskID: "missing", archRef: repairArchRef, want: []string{"not found"}},
		{name: "blank reason", task: func() models.Task { return unstartedCodePlan("t") }, archRef: repairArchRef, reason: " ", want: []string{"reason is required"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, statePath := setupRepairArchRefTest(t, tc.task())
			taskID, reason := tc.taskID, tc.reason
			if taskID == "" {
				taskID = "t"
			}
			if reason == "" {
				reason = "restore scope"
			}
			before := replacementBytes(t, statePath)
			_, err := RepairArchRef(root, taskID, tc.archRef, reason)
			requireProviderOpsAtomicRefusal(t, statePath, before, err, tc.want...)
		})
	}
}

// A file present only in the working tree is not reviewed architecture.
func TestRepairArchRefIgnoresWorkingTreeArtifact(t *testing.T) {
	t.Parallel()
	root, statePath := setupRepairArchRefTest(t, unstartedCodePlan("t"))
	if err := os.MkdirAll(root+"/specs/architecture", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/specs/architecture/draft.md", []byte("## Scope 0: Draft\n"), 0644); err != nil {
		t.Fatal(err)
	}
	before := replacementBytes(t, statePath)
	_, err := RepairArchRef(root, "t", "specs/architecture/draft.md#Scope 0: Draft", "restore scope")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "draft.md", "integration")
}

func TestRepairArchRefLogFailureRemainsCommitted(t *testing.T) {
	t.Parallel()
	root, statePath := setupRepairArchRefTest(t, unstartedCodePlan("t"))
	if err := os.Mkdir(paths.New(root).LogPath(), 0755); err != nil {
		t.Fatal(err)
	}
	result, err := RepairArchRef(root, "t", repairArchRef, "restore scope")
	if err != nil || result == nil || len(result.Warnings) != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if got := mustReadTask(t, statePath, "t").ArchRef; got != repairArchRef {
		t.Fatalf("committed arch_ref = %q, want %q", got, repairArchRef)
	}
}
