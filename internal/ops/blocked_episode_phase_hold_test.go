package ops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// holdGateFixture is a state-lock-hold recovery fixture whose target owns a
// real, healthy preserved worktree, so every check of the automatic gate,
// Git-backed ones included, can be failed independently.
type holdGateFixture struct {
	root, path string
	bb         *db.Blackboard
	authority  models.AgentAuthority
	pr         *pipeline.Resolver
	git        *gitpkg.Git
	base       string
}

func newHoldGateFixture(t *testing.T) holdGateFixture {
	t.Helper()
	root, path, bb, authority := stateLockRecoveryFixture(t, true)
	testhelpers.SetupTestGitRepo(t, root)
	g := gitpkg.New(root)
	base, err := g.GetCommitSHA("integration")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.CreateWorktree("target", "integration"); err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("target")
		task.Worktree, task.BaseCommit = testhelpers.StringPtr(g.GetWorktreeRelPath(task.ID)), &base
		state.Tasks = append(state.Tasks, models.Task{ID: "merged-down", Status: models.TaskStatusMerged, RolePair: "coding-pair"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pr, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	return holdGateFixture{root: root, path: path, bb: bb, authority: authority, pr: pr, git: g, base: base}
}

// gate evaluates automaticStateLockHoldGate on a fresh read of the persisted
// state after memory mutates it. The expected hold is the persisted one unless
// mismatch is set.
func (f holdGateFixture) gate(t *testing.T, mismatch bool, memory func(*models.State, *models.Task)) string {
	t.Helper()
	state := readStateForTest(t, f.path)
	task := state.FindTask("target")
	if memory != nil {
		memory(state, task)
	}
	expected := *task.StateLockHold
	if mismatch {
		expected.RefusedReason = "not the observed hold"
	}
	return automaticStateLockHoldGate(state, task, f.root, f.pr, &expected)
}

// holdGateFault is one way to fail the automatic gate. memory applies it to
// the in-memory state; fs installs a filesystem fault and returns its undo.
type holdGateFault struct {
	reason   string
	mismatch bool
	memory   func(*models.State, *models.Task)
	fs       func(t *testing.T, f holdGateFixture) (undo func())
}

func holdGateFaults() []holdGateFault {
	note := "later"
	assessment := func(task *models.Task, key string, value any) {
		if len(task.History) > 0 && task.History[len(task.History)-1].Event == models.TaskEventOrchestratorAssessment {
			task.History[len(task.History)-1].Extra[key] = value
			return
		}
		task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment, Note: &note, Extra: map[string]any{key: value}})
	}
	return []holdGateFault{
		{reason: "hold_changed", mismatch: true},
		{reason: "publication_pending", memory: func(state *models.State, task *models.Task) {
			task.StateLockHold.AfterSequence = state.MutationSequence
		}},
		{reason: "system_not_running", memory: func(state *models.State, _ *models.Task) { state.Config.Mode = models.SystemModePaused }},
		{reason: "sprint_not_running", memory: func(state *models.State, _ *models.Task) { state.Sprint.Status = models.SprintStatusCheckpoint }},
		{reason: "repair_required", memory: func(_ *models.State, task *models.Task) {
			task.RepairRequest = &models.RepairRequest{Operation: "repair", Target: "target"}
		}},
		{reason: "human_required", memory: func(_ *models.State, task *models.Task) {
			assessment(task, models.AwaitingHumanExtraKey, "Inspect the holder")
		}},
		{reason: "rejection_rca_required", memory: func(_ *models.State, task *models.Task) { task.RejectionRCA = &models.RejectionRCARecord{} }},
		{reason: "preparation_pending", memory: func(_ *models.State, task *models.Task) {
			task.Lifecycle = &models.TaskLifecycle{Preparation: &models.LifecyclePreparation{}}
		}},
		{reason: "awaited_tasks_pending", memory: func(_ *models.State, task *models.Task) {
			assessment(task, AwaitedTasksExtraKey, []string{"merged-down"})
		}},
		{reason: "invalid_dependencies", memory: func(_ *models.State, task *models.Task) { task.DependsOn = append(task.DependsOn, "target") }},
		{reason: "dependencies_pending", memory: func(_ *models.State, task *models.Task) { task.DependsOn = append(task.DependsOn, "missing-dep") }},
		{reason: "invalid_role_pair", memory: func(_ *models.State, task *models.Task) { task.RolePair = "no-such-pair" }},
		{reason: "invalid_dependency_direction", memory: func(_ *models.State, task *models.Task) {
			task.RolePair = "code-planning-pair"
			task.DependsOn = append(task.DependsOn, "merged-down")
		}},
		{reason: "worktree_metadata_invalid", memory: func(_ *models.State, task *models.Task) {
			task.Worktree = testhelpers.StringPtr(".worktrees/elsewhere")
		}},
		{reason: "worktree_base_invalid", memory: func(_ *models.State, task *models.Task) {
			task.BaseCommit = testhelpers.StringPtr("missing-base")
		}},
		{reason: "worktree_unhealthy", fs: func(t *testing.T, f holdGateFixture) func() {
			worktree := filepath.Join(f.root, ".worktrees", "target")
			if err := os.Rename(worktree, worktree+".away"); err != nil {
				t.Fatal(err)
			}
			return func() {
				if err := os.Rename(worktree+".away", worktree); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{reason: "worktree_branch_changed", fs: func(t *testing.T, f holdGateFixture) func() {
			worktree := filepath.Join(f.root, ".worktrees", "target")
			testhelpers.MustGit(t, worktree, "checkout", "-b", "phase-other")
			return func() { testhelpers.MustGit(t, worktree, "checkout", paths.TaskBranchPrefix+"target") }
		}},
	}
}

// P5: automaticStateLockHoldGate keeps its check precedence. Every fault is
// installed at once; the gate must name the earliest, and peeling faults one by
// one in precedence order must expose the next, ending with the empty (resume)
// verdict. Filesystem faults are installed in reverse so the branch fault exists
// before the worktree is moved away.
func TestPhaseAutomaticStateLockHoldGatePrecedence(t *testing.T) {
	t.Parallel()
	f := newHoldGateFixture(t)
	if reason := f.gate(t, false, nil); reason != "" {
		t.Fatalf("pristine gate = %q, want none", reason)
	}
	faults := holdGateFaults()
	undos := make([]func(), len(faults))
	for i := len(faults) - 1; i >= 0; i-- {
		if faults[i].fs != nil {
			undos[i] = faults[i].fs(t, f)
		}
	}
	for i := range faults {
		memory := func(state *models.State, task *models.Task) {
			// Later faults first: when two faults write the same field, the
			// earlier one in precedence order is the one left installed.
			for j := len(faults) - 1; j >= i; j-- {
				if faults[j].memory != nil {
					faults[j].memory(state, task)
				}
			}
		}
		mismatch := false
		for _, fault := range faults[i:] {
			mismatch = mismatch || fault.mismatch
		}
		if got := f.gate(t, mismatch, memory); got != faults[i].reason {
			t.Fatalf("with faults %s..end installed the gate named %q, want %q", faults[i].reason, got, faults[i].reason)
		}
		if undos[i] != nil {
			undos[i]()
		}
	}
	if reason := f.gate(t, false, nil); reason != "" {
		t.Fatalf("gate after peeling every fault = %q, want none", reason)
	}
}

// P5: each fault alone is named by the gate, so no check is shadowed by a
// precedence peer that was never reachable on its own.
func TestPhaseAutomaticStateLockHoldGateEachFaultAlone(t *testing.T) {
	t.Parallel()
	for _, fault := range holdGateFaults() {
		t.Run(fault.reason, func(t *testing.T) {
			t.Parallel()
			f := newHoldGateFixture(t)
			if fault.fs != nil {
				fault.fs(t, f)
			}
			if got := f.gate(t, fault.mismatch, fault.memory); got != fault.reason {
				t.Fatalf("gate = %q, want %q", got, fault.reason)
			}
		})
	}
}

// P5: the direct-dependency validation of the automatic gate is behind the
// stateLockHoldGate dependency checks. Every terminal non-MERGED dependency that
// validateUnblockDirectDependencies would refuse is already named
// dependencies_pending first, so the "invalid_dependencies" it could return
// there is unreachable; only a MERGED dependency passes both.
func TestPhaseAutomaticStateLockHoldGateDirectDependencyIsShadowed(t *testing.T) {
	t.Parallel()
	f := newHoldGateFixture(t)
	for _, status := range []models.TaskStatus{models.TaskStatusAbandoned, models.TaskStatusIntegrationFailed, models.TaskStatusSuperseded} {
		got := f.gate(t, false, func(state *models.State, task *models.Task) {
			state.Tasks = append(state.Tasks, models.Task{ID: "dep-" + string(status), Status: status, RolePair: "coding-pair"})
			task.DependsOn = []string{"dep-" + string(status)}
		})
		if got != "dependencies_pending" {
			t.Fatalf("%s dependency: gate = %q, want dependencies_pending", status, got)
		}
	}
	if got := f.gate(t, false, func(_ *models.State, task *models.Task) { task.DependsOn = []string{"merged-down"} }); got != "" {
		t.Fatalf("merged dependency: gate = %q, want none", got)
	}
}

// P6: recordStateLockHoldRefusal publishes only RefusedReason and
// RefusedFingerprint, in one generation-fenced transaction, and only for the
// observed hold, episode and transition.
func TestPhaseRecordStateLockHoldRefusal(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (holdGateFixture, *models.Task, *models.StateLockHold) {
		t.Helper()
		f := newHoldGateFixture(t)
		observed := readStateForTest(t, f.path).FindTask("target")
		hold := *observed.StateLockHold
		return f, observed, &hold
	}
	record := func(f holdGateFixture, observed *models.Task, hold *models.StateLockHold, reason string) error {
		return recordStateLockHoldRefusal(f.bb, f.root, f.authority, f.pr, observed, hold, reason)
	}
	state := func(t *testing.T, f holdGateFixture) *models.State { return readStateForTest(t, f.path) }

	t.Run("gate-refusal-replaces-proposed-reason", func(t *testing.T) {
		t.Parallel()
		f, observed, hold := setup(t)
		if err := f.bb.Modify(func(s *models.State) error { s.Config.Mode = models.SystemModePaused; return nil }); err != nil {
			t.Fatal(err)
		}
		before := state(t, f)
		if err := record(f, observed, hold, "unblock_domain_refused"); err != nil {
			t.Fatal(err)
		}
		after := state(t, f)
		got := after.FindTask("target").StateLockHold
		if got.RefusedReason != "system_not_running" || got.RefusedFingerprint != stateLockHoldMaterial(after, after.FindTask("target"), "system_not_running") {
			t.Fatalf("published hold = %+v, want the gate's reason and its fingerprint", got)
		}
		if after.MutationSequence != before.MutationSequence+1 {
			t.Fatalf("mutation sequence %d -> %d, want one publication", before.MutationSequence, after.MutationSequence)
		}
		want := *before.FindTask("target")
		want.StateLockHold = got
		if !reflect.DeepEqual(&want, after.FindTask("target")) {
			t.Fatal("the refusal published more than RefusedReason and RefusedFingerprint")
		}
		if err := record(f, observed, hold, "unblock_domain_refused"); !errors.Is(err, errStateLockHoldUnchanged) {
			t.Fatalf("repeat = %v, want errStateLockHoldUnchanged", err)
		}
	})
	t.Run("passing-gate-publishes-only-unblock-reasons", func(t *testing.T) {
		t.Parallel()
		f, observed, hold := setup(t)
		before := state(t, f)
		if err := record(f, observed, hold, "worktree_unhealthy"); !errors.Is(err, errStateLockHoldUnchanged) {
			t.Fatalf("non-unblock reason with a passing gate = %v, want errStateLockHoldUnchanged", err)
		}
		if after := state(t, f); after.MutationSequence != before.MutationSequence {
			t.Fatal("a refused observation published state")
		}
		if err := record(f, observed, hold, "unblock_domain_refused"); err != nil {
			t.Fatal(err)
		}
		after := state(t, f)
		if got := after.FindTask("target").StateLockHold; got.RefusedReason != "unblock_domain_refused" || got.RefusedFingerprint != stateLockHoldMaterial(after, after.FindTask("target"), "unblock_domain_refused") {
			t.Fatalf("published hold = %+v", got)
		}
		if err := record(f, observed, hold, "unblock_domain_refused"); !errors.Is(err, errStateLockHoldUnchanged) {
			t.Fatalf("repeat = %v, want errStateLockHoldUnchanged", err)
		}
	})
	t.Run("changed-observation-publishes-nothing", func(t *testing.T) {
		t.Parallel()
		for name, change := range map[string]func(*models.State){
			"hold": func(s *models.State) { s.FindTask("target").StateLockHold.AfterSequence++ },
			"transition": func(s *models.State) {
				s.FindTask("target").History = append(s.FindTask("target").History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventUnblocked})
			},
			"task": func(s *models.State) { s.Tasks = s.Tasks[:0] },
		} {
			t.Run(name, func(t *testing.T) {
				f, observed, hold := setup(t)
				if err := f.bb.Modify(func(s *models.State) error { change(s); return nil }); err != nil {
					t.Fatal(err)
				}
				before := state(t, f)
				if err := record(f, observed, hold, "unblock_domain_refused"); !errors.Is(err, errStateLockHoldUnchanged) {
					t.Fatalf("changed %s = %v, want errStateLockHoldUnchanged", name, err)
				}
				if after := state(t, f); after.MutationSequence != before.MutationSequence {
					t.Fatalf("changed %s published state", name)
				}
			})
		}
	})
	t.Run("stale-generation-is-an-authority-error-with-no-write", func(t *testing.T) {
		t.Parallel()
		f, observed, hold := setup(t)
		before, _ := os.ReadFile(f.path)
		stale := f.authority
		stale.Generation = "retired"
		if err := recordStateLockHoldRefusal(f.bb, f.root, stale, f.pr, observed, hold, "unblock_domain_refused"); !IsAgentAuthorityError(err) {
			t.Fatalf("stale authority = %v, want an agent authority error", err)
		}
		if after, _ := os.ReadFile(f.path); !bytes.Equal(before, after) {
			t.Fatal("a stale authority published state")
		}
	})
}
