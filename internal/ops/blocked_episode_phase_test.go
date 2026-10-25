package ops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Frozen-phase tests P1-P9 of the D49 correction of A3 Scope 3. They pin the
// phase order, lock windows and transaction boundaries of the blocked-episode
// operations as they stand before the episode concerns are separated, so a
// refactor that reorders a check, moves an inherited Git call (B1-B8) out of
// its callback or changes a lock window fails here. Each refusal test asserts
// the error identity, its text and the persisted state, and is paired with a
// control that reaches the same decision by another route. Barriers are the
// per-blackboard lifecycle hook and channels; nothing sleeps for ordering.

// phaseUnblockFixture is an orchestrator fixture with a BLOCKED coding task
// without preserved work and a registered coder, written as raw state so a
// test can install conditions that validation would refuse.
func phaseUnblockFixture(t *testing.T, edit func(state *models.State, task *models.Task)) lifecycleGenerationFixture {
	t.Helper()
	fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusBlocked)
	phaseRewriteState(t, fixture, func(state *models.State) {
		task := state.FindTask("task-1")
		task.Worktree, task.BaseCommit = nil, nil
		state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
		if edit != nil {
			edit(state, task)
		}
	})
	return fixture
}

func phaseRewriteState(t *testing.T, fixture lifecycleGenerationFixture, edit func(*models.State)) {
	t.Helper()
	bb := db.New(fixture.statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read fixture state: %v", err)
	}
	edit(state)
	if err := bb.Write(state); err != nil {
		t.Fatalf("write fixture state: %v", err)
	}
}

// requirePhaseRefusal asserts a refusal that names want, is a precondition
// failure carrying a lifecycle outcome, and left the persisted state untouched.
func requirePhaseRefusal(t *testing.T, fixture lifecycleGenerationFixture, before []byte, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("unblock succeeded, want a refusal naming %q", want)
	}
	var precondition *PreconditionError
	if !errors.As(err, &precondition) || !strings.Contains(err.Error(), want) {
		t.Fatalf("refusal = %T %v, want a precondition failure naming %q", err, err, want)
	}
	if after := fixture.stateBytes(t); !bytes.Equal(before, after) {
		t.Fatalf("refused unblock changed state\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// unblockBreaker installs exactly one failing check of the unblock mutate
// closure, in the order the closure reaches it (P3).
type unblockBreaker struct {
	name, want string
	apply      func(state *models.State, task *models.Task, opts *UnblockTaskOptions)
}

func phaseUnblockBreakers(now time.Time) []unblockBreaker {
	rolePair := func(task *models.Task, pair string) { task.RolePair = pair }
	return []unblockBreaker{
		{"validation-prerequisite-assign", "validation preflight requires the target session", func(_ *models.State, task *models.Task, opts *UnblockTaskOptions) {
			if opts.AssignTo == "" {
				opts.AssignTo = "coder-1"
			}
			task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "true"}}
		}},
		{"not-blocked", "task must be BLOCKED to unblock", func(_ *models.State, task *models.Task, _ *UnblockTaskOptions) {
			task.Status, task.AssignedTo, task.BlockedReason, task.BlockedQuestions = models.TaskStatusReady, nil, nil, nil
		}},
		{"automatic-options", "automatic state lock continuation cannot assign, rebase or consume an iteration", func(_ *models.State, _ *models.Task, opts *UnblockTaskOptions) {
			opts.stateLockHold, opts.NewIteration = &models.StateLockHold{}, true
		}},
		{"automatic-gate", "state lock hold recovery refused: hold_changed", func(_ *models.State, _ *models.Task, opts *UnblockTaskOptions) {
			opts.stateLockHold = &models.StateLockHold{}
		}},
		{"rejection-rca-gate", "has an open rejection-RCA gate", func(_ *models.State, task *models.Task, _ *UnblockTaskOptions) {
			reason := models.BlockedReasonRejectionRCARequired + ": 4 durable rejections reached threshold 4"
			task.BlockedReason = &reason
			task.RejectionRCA = &models.RejectionRCARecord{SchemaVersion: models.RejectionRCASchemaVersion, Threshold: 4, RejectionCount: 4, GatedAt: now}
		}},
		{"role-pair-missing", "has no role_pair set", func(_ *models.State, task *models.Task, _ *UnblockTaskOptions) { rolePair(task, "") }},
		{"role-pair-unrecognized", "unrecognized role-pair", func(_ *models.State, task *models.Task, _ *UnblockTaskOptions) {
			rolePair(task, "no-such-pair")
		}},
		{"scope-exception-non-coding", "scope exceptions apply to coding tasks only", func(_ *models.State, task *models.Task, opts *UnblockTaskOptions) {
			rolePair(task, "code-planning-pair")
			opts.ScopeException = &ScopeException{Files: []string{"internal/example.go"}, Change: "edit", Unchanged: "all else"}
		}},
		{"budget-exception-non-coding", "budget exceptions apply to coding tasks only", func(_ *models.State, task *models.Task, opts *UnblockTaskOptions) {
			rolePair(task, "code-planning-pair")
			opts.BudgetException = &BudgetException{TimeoutSeconds: 600, Ruling: "D-phase-1"}
		}},
		{"assign-role-mismatch", `assign-to agent role "code-reviewer" does not match task doer role`, func(_ *models.State, _ *models.Task, opts *UnblockTaskOptions) {
			opts.AssignTo = "code-reviewer-1"
		}},
		{"direct-dependency-invalid", `has invalid depends_on entry " padded"`, func(_ *models.State, task *models.Task, _ *UnblockTaskOptions) {
			task.DependsOn = append(task.DependsOn, " padded")
		}},
		{"dependency-direction", "has downstream dependency dep-down", func(state *models.State, task *models.Task, _ *UnblockTaskOptions) {
			rolePair(task, "code-planning-pair")
			// Edit the task before appending: the append may move state.Tasks.
			task.DependsOn = append(task.DependsOn, "dep-down")
			state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("dep-down", models.TaskStatusImplementing, now))
		}},
		{"unmet-dependency", "has invalid dependency: missing-dep", func(_ *models.State, task *models.Task, _ *UnblockTaskOptions) {
			task.DependsOn = append(task.DependsOn, "missing-dep")
		}},
	}
}

// phaseBreakersConflict reports pairs whose conditions cannot hold together:
// both write the role pair, or an assignment makes the automatic-option check
// the first failure of an automatic recovery.
func phaseBreakersConflict(a, b string) bool {
	exclusive := map[string]bool{"role-pair-missing": true, "role-pair-unrecognized": true}
	shared := map[string]bool{"scope-exception-non-coding": true, "budget-exception-non-coding": true, "dependency-direction": true}
	if (exclusive[a] && (exclusive[b] || shared[b])) || (exclusive[b] && shared[a]) {
		return true
	}
	assigns := map[string]bool{"validation-prerequisite-assign": true, "assign-role-mismatch": true}
	return (a == "automatic-gate" && assigns[b]) || (b == "automatic-gate" && assigns[a])
}

// P3: the final mutate closure returns the earlier of two failing checks and
// publishes nothing. The closure also runs as the dry-run on the preflight
// copy, so every check before the rebase revalidation is reached there.
func TestPhaseUnblockMutateCheckOrder(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	breakers := phaseUnblockBreakers(now)
	run := func(t *testing.T, applied ...unblockBreaker) (lifecycleGenerationFixture, []byte, error) {
		t.Helper()
		opts := UnblockTaskOptions{}
		fixture := phaseUnblockFixture(t, func(state *models.State, task *models.Task) {
			task.RolePair = "coding-pair"
			for _, breaker := range applied {
				breaker.apply(state, task, &opts)
			}
		})
		before := fixture.stateBytes(t)
		_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, opts)
		return fixture, before, err
	}

	t.Run("control-unbroken-unblock-succeeds", func(t *testing.T) {
		t.Parallel()
		fixture, _, err := run(t)
		if err != nil {
			t.Fatalf("unblock with no failing check: %v", err)
		}
		if task := readStateForTest(t, fixture.statePath).FindTask("task-1"); task.Status == models.TaskStatusBlocked {
			t.Fatal("control unblock left the task BLOCKED")
		}
	})
	for _, breaker := range breakers {
		t.Run("alone/"+breaker.name, func(t *testing.T) {
			t.Parallel()
			fixture, before, err := run(t, breaker)
			requirePhaseRefusal(t, fixture, before, err, breaker.want)
		})
	}
	for i, earlier := range breakers {
		for _, later := range breakers[i+1:] {
			if phaseBreakersConflict(earlier.name, later.name) {
				continue
			}
			t.Run(earlier.name+"-before-"+later.name, func(t *testing.T) {
				t.Parallel()
				fixture, before, err := run(t, earlier, later)
				requirePhaseRefusal(t, fixture, before, err, earlier.want)
				if strings.Contains(err.Error(), later.want) {
					t.Fatalf("refusal %q also names the later check %q", err, later.want)
				}
			})
		}
	}
}

// phaseTaskLockProbe reports, from inside a transaction, whether the unblock
// task lock and the project lifecycle lock are held by the running lifecycle.
type phaseTaskLockProbe struct {
	t       *testing.T
	fixture lifecycleGenerationFixture
	// held records, per observed transaction, the verdicts of one probe.
	taskHeld, projectShared, projectExclusiveRefused []bool
}

func (p *phaseTaskLockProbe) observe() {
	p.t.Helper()
	taskLock := filelock.New(claimTaskWorktreeLockPath(p.fixture.statePath, "task-1")).WithTimeout(60 * time.Millisecond)
	err := taskLock.WithLockOperation("phase-probe", func() error { return nil })
	p.taskHeld = append(p.taskHeld, filelock.IsLockErrorType(err, filelock.LockErrorTimeout))

	shared := false
	if err := WithProjectLifecycleSharedLock(p.fixture.projectRoot, "phase-probe", func() error { shared = true; return nil }); err != nil {
		p.t.Errorf("shared project lifecycle acquisition inside the window: %v", err)
	}
	p.projectShared = append(p.projectShared, shared)

	projectLock, err := projectLifecycleLock(p.fixture.projectRoot)
	if err != nil {
		p.t.Errorf("project lifecycle lock: %v", err)
		return
	}
	err = withProjectLifecycleLock(projectLock, "phase-probe", false, 60*time.Millisecond, func() error { return nil })
	p.projectExclusiveRefused = append(p.projectExclusiveRefused, err != nil)
}

func (p *phaseTaskLockProbe) requireWindow(transactions int) {
	p.t.Helper()
	for name, verdicts := range map[string][]bool{"unblock task lock held": p.taskHeld, "shared project lifecycle lock acquirable": p.projectShared, "exclusive project lifecycle lock refused": p.projectExclusiveRefused} {
		if len(verdicts) != transactions {
			p.t.Fatalf("%s: observed %d transactions, want %d", name, len(verdicts), transactions)
		}
		for i, ok := range verdicts {
			if !ok {
				p.t.Errorf("transaction %d: %s = false inside the unblock lock window", i+1, name)
			}
		}
	}
}

// P1: preparation, conflict and final transactions run inside the window of
// withOwnershipTaskLock (project lifecycle shared, then the unblock-task task
// lock) and take no outer lock themselves: every transaction starts with the
// task lock held and the project lifecycle lock shared, and an exclusive
// acquirer is refused for the whole lifecycle. Released afterwards.
func TestPhaseUnblockLifecycleRunsInsideOwnershipTaskLock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		conflict     bool
		transactions int
	}{
		{"preparation-and-final", false, 2},
		{"preparation-and-conflict", true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newPhaseRebaseFixture(t, tc.conflict)
			probe := &phaseTaskLockProbe{t: t, fixture: fixture}
			t.Cleanup(setLifecycleMutationTestHook(db.For(fixture.statePath), probe.observe))

			_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, UnblockTaskOptions{RebaseOn: "integration"})
			var conflict *UnblockRebaseConflictError
			if tc.conflict != errors.As(err, &conflict) || (!tc.conflict && err != nil) {
				t.Fatalf("unblock error = %v, want conflict=%t", err, tc.conflict)
			}
			probe.requireWindow(tc.transactions)

			// Control: the lifecycle released both locks when it returned.
			released := filelock.New(claimTaskWorktreeLockPath(fixture.statePath, "task-1")).WithTimeout(time.Second).WithLockOperation("phase-control", func() error { return nil })
			exclusive := WithProjectLifecycleExclusiveLock(fixture.projectRoot, "phase-control", func() error { return nil })
			if released != nil || exclusive != nil {
				t.Fatalf("locks still held after the lifecycle returned: task=%v project=%v", released, exclusive)
			}
		})
	}
}

// newPhaseRebaseFixture is an orchestrator fixture whose BLOCKED task keeps a
// real worktree on task/task-1 and whose integration branch moved ahead (clean
// rebase) or moved with a conflicting edit of the same file (conflict).
func newPhaseRebaseFixture(t *testing.T, conflict bool) lifecycleGenerationFixture {
	t.Helper()
	fixture := newOrchestratorLifecycleFixture(t, true, models.TaskStatusBlocked)
	if conflict {
		writeAndCommit(t, fixture.projectRoot, "conflict.txt", "base\n", "Add base conflict file")
		testhelpers.MustGit(t, fixture.projectRoot, "branch", "-f", "integration", "HEAD")
	}
	testhelpers.CreateTestWorktree(t, fixture.projectRoot, "task-1")
	baseCommit := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "integration")
	worktreeDir := filepath.Join(fixture.projectRoot, ".worktrees", "task-1")
	if conflict {
		writeAndCommit(t, worktreeDir, "conflict.txt", "task\n", "Task conflict edit")
		writeAndCommit(t, fixture.projectRoot, "conflict.txt", "integration\n", "Integration conflict edit")
	} else {
		writeAndCommit(t, fixture.projectRoot, "integration.txt", "integration move\n", "Move integration")
	}
	testhelpers.MustGit(t, fixture.projectRoot, "branch", "-f", "integration", testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "HEAD"))
	phaseRewriteState(t, fixture, func(state *models.State) {
		task := state.FindTask("task-1")
		task.RolePair = "code-planning-pair"
		task.BaseCommit = &baseCommit
		worktree := ".worktrees/task-1"
		task.Worktree = &worktree
		state.Agents["code-planner-1"] = testhelpers.RegisteredTestAgent("code-planner")
	})
	return fixture
}

// phaseRequestFor returns the lifecycle identity of one request against the
// task's current boundary.
func phaseRequestFor(t *testing.T, fixture lifecycleGenerationFixture, id string) LifecycleRequestOptions {
	t.Helper()
	return LifecycleRequestOptions{RequestID: id, ExpectedTransition: models.TaskTransitionID(readStateForTest(t, fixture.statePath).FindTask("task-1"))}
}

// P2: an exact replay returns from the dry-run on the preflight copy, before
// preparation and before any Git call; the control reaches Git with the same
// options on a task that is still BLOCKED.
func TestPhaseUnblockReplayReturnsBeforePreparationOrGit(t *testing.T) {
	t.Parallel()
	opts := func(request LifecycleRequestOptions) UnblockTaskOptions {
		return UnblockTaskOptions{Request: request, RebaseOn: "integration"}
	}
	t.Run("replay", func(t *testing.T) {
		t.Parallel()
		fixture := newPhaseRebaseFixture(t, false)
		request := phaseRequestFor(t, fixture, "phase-replay")
		first, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, opts(request))
		if err != nil || first.Outcome != models.LifecycleCompleted || first.Rebase == nil {
			t.Fatalf("original unblock = %+v, %v; want a completed rebase", first, err)
		}
		// Every Git call from here on fails: the worktree is gone.
		worktree := filepath.Join(fixture.projectRoot, ".worktrees", "task-1")
		if err := os.Rename(worktree, worktree+".away"); err != nil {
			t.Fatal(err)
		}
		transactions := 0
		t.Cleanup(setLifecycleMutationTestHook(db.For(fixture.statePath), func() { transactions++ }))
		before := fixture.stateBytes(t)

		replay, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, opts(request))
		if err != nil {
			t.Fatalf("exact replay with an unusable worktree: %v", err)
		}
		if replay.Outcome != models.LifecycleAlreadyCompleted || replay.Rebase != nil {
			t.Fatalf("replay = %+v, want ALREADY_COMPLETED without a rebase", replay)
		}
		if transactions != 0 || !bytes.Equal(before, fixture.stateBytes(t)) {
			t.Fatalf("replay opened %d transactions or changed state", transactions)
		}
	})
	t.Run("control-first-request-reaches-git", func(t *testing.T) {
		t.Parallel()
		fixture := newPhaseRebaseFixture(t, false)
		worktree := filepath.Join(fixture.projectRoot, ".worktrees", "task-1")
		if err := os.Rename(worktree, worktree+".away"); err != nil {
			t.Fatal(err)
		}
		transactions := 0
		t.Cleanup(setLifecycleMutationTestHook(db.For(fixture.statePath), func() { transactions++ }))
		before := fixture.stateBytes(t)

		_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, opts(phaseRequestFor(t, fixture, "phase-control")))
		requirePhaseRefusal(t, fixture, before, err, "worktree not healthy")
		if transactions != 0 {
			t.Fatalf("a refused snapshot opened %d transactions, want none before preparation", transactions)
		}
	})
}

// phaseFinalTransactionHook runs fn once, immediately before the second
// lifecycle transaction of an identified unblock: after the preparation write
// and the rebase, before the final mutate. It returns the state the hook left
// behind, which the final transaction must not change when it refuses.
type phaseFinalTransactionHook struct {
	transactions int
	afterHook    []byte
}

func installPhaseFinalHook(t *testing.T, fixture lifecycleGenerationFixture, fn func()) *phaseFinalTransactionHook {
	t.Helper()
	observed := &phaseFinalTransactionHook{}
	t.Cleanup(setLifecycleMutationTestHook(db.For(fixture.statePath), func() {
		observed.transactions++
		if observed.transactions == 2 {
			fn()
			observed.afterHook = fixture.stateBytes(t)
		}
	}))
	return observed
}

func phaseGit(t *testing.T, fixture lifecycleGenerationFixture, dir string, args ...string) string {
	t.Helper()
	return testhelpers.MustGit(t, filepath.Join(fixture.projectRoot, dir), args...)
}

// requireRevalidationRefusal asserts the refusal of the final transaction of a
// rebase unblock: unchanged text and identity, nothing published after the
// preparation write, and the recorded effects left to the requery path (the
// preparation is kept, the rebase is not undone, the base commit is not moved).
func requireRevalidationRefusal(t *testing.T, fixture lifecycleGenerationFixture, hook *phaseFinalTransactionHook, base string, err error, want string, precondition bool) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("refusal = %v, want text naming %q", err, want)
	}
	var identity *PreconditionError
	if precondition != errors.As(err, &identity) {
		t.Fatalf("refusal %T %v: precondition identity = %t, want %t", err, err, !precondition, precondition)
	}
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged || lifecycleErr.Outcome.SafeAction != "requery" || lifecycleErr.Outcome.Effects != "unknown" {
		t.Fatalf("refusal outcome = %+v, want STATE_CHANGED/requery with unknown effects", err)
	}
	if hook.transactions != 2 {
		t.Fatalf("lifecycle transactions = %d, want preparation and final only", hook.transactions)
	}
	if !bytes.Equal(hook.afterHook, fixture.stateBytes(t)) {
		t.Fatal("the refused final transaction published state")
	}
	task := readStateForTest(t, fixture.statePath).FindTask("task-1")
	if task.Status != models.TaskStatusBlocked || task.Lifecycle == nil || task.Lifecycle.Preparation == nil {
		t.Fatalf("task = %s, preparation kept = %t; want BLOCKED with the recorded preparation", task.Status, task.Lifecycle != nil && task.Lifecycle.Preparation != nil)
	}
	if task.BaseCommit == nil || *task.BaseCommit != base {
		t.Fatalf("base commit moved to %v despite the refusal", task.BaseCommit)
	}
	for _, entry := range task.History {
		if entry.Event == models.TaskEventUnblocked {
			t.Fatal("a refused unblock recorded an unblocked entry")
		}
	}
}

func phaseBaseCommit(t *testing.T, fixture lifecycleGenerationFixture) string {
	t.Helper()
	return *readStateForTest(t, fixture.statePath).FindTask("task-1").BaseCommit
}

func phaseRenameAway(t *testing.T, fixture lifecycleGenerationFixture) {
	t.Helper()
	worktree := filepath.Join(fixture.projectRoot, ".worktrees", "task-1")
	if err := os.Rename(worktree, worktree+".away"); err != nil {
		t.Fatal(err)
	}
}

// P4: after a successful --rebase-on, the inherited revalidation in the final
// transaction (B4 health, B5 branch, B6 HEAD against the rebase NewHead) refuses
// with unchanged error and cause, publishes no unblock and leaves the recorded
// preparation and rebase effects to the requery path. The control completes.
func TestPhaseUnblockRebaseRevalidationRefusals(t *testing.T) {
	t.Parallel()
	opts := UnblockTaskOptions{RebaseOn: "integration"}
	t.Run("control-completes", func(t *testing.T) {
		t.Parallel()
		fixture := newPhaseRebaseFixture(t, false)
		hook := installPhaseFinalHook(t, fixture, func() {})
		result, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, opts)
		if err != nil || result.Rebase == nil || hook.transactions != 2 {
			t.Fatalf("unblock = %+v, %v after %d transactions; want a completed rebase unblock", result, err, hook.transactions)
		}
		if head := phaseGit(t, fixture, ".worktrees/task-1", "rev-parse", "HEAD"); head != result.Rebase.NewHead {
			t.Fatalf("worktree HEAD %s differs from the recorded NewHead %s", head, result.Rebase.NewHead)
		}
	})
	for _, tc := range []struct {
		name, want   string
		precondition bool
		sabotage     func(t *testing.T, fixture lifecycleGenerationFixture)
	}{
		{"b4-worktree-recorded-path", "worktree changed during rebase", true, func(t *testing.T, fixture lifecycleGenerationFixture) {
			phaseRewriteState(t, fixture, func(state *models.State) {
				elsewhere := ".worktrees/elsewhere"
				state.FindTask("task-1").Worktree = &elsewhere
			})
		}},
		{"b4-worktree-health", "worktree not healthy after rebase: worktree directory missing", true, func(t *testing.T, fixture lifecycleGenerationFixture) { phaseRenameAway(t, fixture) }},
		{"b5-worktree-branch", "worktree branch changed during rebase: phase-other", true, func(t *testing.T, fixture lifecycleGenerationFixture) {
			phaseGit(t, fixture, ".worktrees/task-1", "checkout", "-b", "phase-other")
		}},
		{"b6-worktree-head", "worktree HEAD changed during unblock", true, func(t *testing.T, fixture lifecycleGenerationFixture) {
			writeAndCommit(t, filepath.Join(fixture.projectRoot, ".worktrees", "task-1"), "late.txt", "late\n", "Late commit")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newPhaseRebaseFixture(t, false)
			base := phaseBaseCommit(t, fixture)
			hook := installPhaseFinalHook(t, fixture, func() { tc.sabotage(t, fixture) })
			_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, opts)
			requireRevalidationRefusal(t, fixture, hook, base, err, tc.want, tc.precondition)
		})
	}
}

// P3 (final transaction): checks reached only after the rebase keep their
// order against the failing check that follows or precedes them. Each case
// installs two failures between the preparation and the final transaction and
// expects the earlier one; its control installs the later failure alone.
func TestPhaseUnblockFinalTransactionCheckOrder(t *testing.T) {
	t.Parallel()
	type pair struct {
		name, want, controlWant string
		precondition            bool
		opts                    UnblockTaskOptions
		earlier, later          func(t *testing.T, fixture lifecycleGenerationFixture)
	}
	addMissingDependency := func(t *testing.T, fixture lifecycleGenerationFixture) {
		phaseRewriteState(t, fixture, func(state *models.State) {
			task := state.FindTask("task-1")
			task.DependsOn = append(task.DependsOn, "missing-dep")
		})
	}
	unregisterAssignee := func(t *testing.T, fixture lifecycleGenerationFixture) {
		phaseRewriteState(t, fixture, func(state *models.State) { delete(state.Agents, "code-planner-1") })
	}
	clearPreparation := func(t *testing.T, fixture lifecycleGenerationFixture) {
		phaseRewriteState(t, fixture, func(state *models.State) { state.FindTask("task-1").Lifecycle.Preparation = nil })
	}
	health := func(t *testing.T, fixture lifecycleGenerationFixture) { phaseRenameAway(t, fixture) }
	for _, tc := range []pair{
		{name: "preparation-validation-before-revalidation", want: "preparation or task boundary changed before finalization", controlWant: "worktree not healthy after rebase",
			earlier: clearPreparation, later: health},
		{name: "unmet-dependency-before-revalidation", want: "missing-dep", controlWant: "worktree not healthy after rebase", precondition: true,
			earlier: addMissingDependency, later: health},
		{name: "revalidation-before-assignment", want: "worktree not healthy after rebase", controlWant: "code-planner-1", precondition: true,
			opts: UnblockTaskOptions{AssignTo: "code-planner-1"}, earlier: health, later: unregisterAssignee},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := tc.opts
			opts.RebaseOn = "integration"
			run := func(t *testing.T, installs ...func(*testing.T, lifecycleGenerationFixture)) error {
				t.Helper()
				fixture := newPhaseRebaseFixture(t, false)
				base := phaseBaseCommit(t, fixture)
				hook := installPhaseFinalHook(t, fixture, func() {
					for _, install := range installs {
						install(t, fixture)
					}
				})
				_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, opts)
				requireFinalTransactionRefusal(t, fixture, hook, base)
				return err
			}
			if err := run(t, tc.later); err == nil || !strings.Contains(err.Error(), tc.controlWant) {
				t.Fatalf("control refusal = %v, want text naming %q", err, tc.controlWant)
			}
			err := run(t, tc.earlier, tc.later)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal = %v, want text naming %q", err, tc.want)
			}
			var identity *PreconditionError
			if tc.precondition != errors.As(err, &identity) {
				t.Fatalf("refusal %T %v: precondition identity = %t, want %t", err, err, !tc.precondition, tc.precondition)
			}
		})
	}
}

// P3 (final transaction): a task that disappears between preparation and final
// is refused at the first check of the final transaction, with nothing published.
func TestPhaseUnblockFinalTransactionTaskDisappears(t *testing.T) {
	t.Parallel()
	fixture := newPhaseRebaseFixture(t, false)
	hook := installPhaseFinalHook(t, fixture, func() {
		phaseRewriteState(t, fixture, func(state *models.State) { state.Tasks = nil })
	})
	_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, UnblockTaskOptions{RebaseOn: "integration"})
	if err == nil || !strings.Contains(err.Error(), "task-1") {
		t.Fatalf("refusal = %v, want text naming the missing task", err)
	}
	if hook.transactions != 2 || !bytes.Equal(hook.afterHook, fixture.stateBytes(t)) {
		t.Fatalf("lifecycle transactions = %d, or the refused final transaction published state", hook.transactions)
	}
}

// requireFinalTransactionRefusal asserts what every refused final transaction
// leaves: exactly the preparation and final transactions ran, the state is the
// one the hook left, the task is BLOCKED at its original base, and no unblocked
// entry was recorded.
func requireFinalTransactionRefusal(t *testing.T, fixture lifecycleGenerationFixture, hook *phaseFinalTransactionHook, base string) {
	t.Helper()
	if hook.transactions != 2 || !bytes.Equal(hook.afterHook, fixture.stateBytes(t)) {
		t.Fatalf("lifecycle transactions = %d, or the refused final transaction published state", hook.transactions)
	}
	task := readStateForTest(t, fixture.statePath).FindTask("task-1")
	if task.Status != models.TaskStatusBlocked || (task.BaseCommit != nil && *task.BaseCommit != base) {
		t.Fatalf("task = %s base %v; want BLOCKED at the original base", task.Status, task.BaseCommit)
	}
	for _, entry := range task.History {
		if entry.Event == models.TaskEventUnblocked {
			t.Fatal("a refused unblock recorded an unblocked entry")
		}
	}
}
