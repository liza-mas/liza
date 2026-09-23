package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReplaceTaskSchemaParity(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*ReplaceTaskInput)
	}{
		{"required", func(p *ReplaceTaskInput) { p.SourceTaskID = ""; p.Reason = "" }},
		{"enum", func(p *ReplaceTaskInput) { p.Replacement.Type = "PRIVATE_REJECTED_VALUE" }},
		{"cardinality", func(p *ReplaceTaskInput) { p.Consumers = append(p.Consumers, p.Consumers[0]) }},
		{"explicit lists", func(p *ReplaceTaskInput) { p.Consumers[0].ExpectedDependsOn = nil }},
		{"preserved base", func(p *ReplaceTaskInput) { p.PreservedBase = &PreservedTaskBase{BaseCommit: "abc"} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := newReplacementFixture(t)
			change.apply(&f.input)
			before := replacementBytes(t, f.statePath)
			t.Cleanup(setLifecycleMutationTestHook(db.For(f.statePath), func() { t.Error("invalid payload reached mutation") }))
			encoded, err := json.Marshal(f.input)
			if err != nil {
				t.Fatal(err)
			}
			var payload any
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatal(err)
			}
			_, want, err := payloadschema.Validate("replace-task", payload)
			if err != nil || len(want) == 0 {
				t.Fatalf("preflight: %+v %v", want, err)
			}
			err = taskSchemaCallWithStateLocked(t, f.statePath, func() error {
				_, err := f.run()
				return err
			})
			var lifecycle *LifecycleError
			if !errors.As(err, &lifecycle) || lifecycle.Outcome.Outcome != models.LifecycleInvalidInput || lifecycle.Outcome.SafeAction != "correct_input" || !reflect.DeepEqual(want, lifecycle.Outcome.Diagnostics) {
				t.Fatalf("boundary error=%v, want diagnostics=%+v", err, want)
			}
			if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
				t.Fatal("invalid payload changed state")
			}
		})
	}
}

type replacementFixture struct {
	root      string
	statePath string
	input     ReplaceTaskInput
	authority models.AgentAuthority
	opts      LifecycleRequestOptions
}

func newReplacementFixture(t *testing.T) replacementFixture {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	s := testhelpers.CreateValidState()
	s.Goal.SpecRef = "README.md"
	s.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	now := time.Now().UTC()
	s.Tasks = []models.Task{testhelpers.BuildTaskByStatus("source", models.TaskStatusReady, now)}
	updates := []models.DependencyUpdate{}
	for _, id := range []string{"consumer-a", "consumer-b"} {
		consumer := testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, now)
		consumer.DependsOn = []string{"source"}
		s.Tasks = append(s.Tasks, consumer)
		updates = append(updates, models.DependencyUpdate{TaskID: id, ExpectedDependsOn: []string{"source"}, DesiredDependsOn: []string{"replacement"}})
	}
	testhelpers.WriteInitialState(t, statePath, s)
	return replacementFixture{root: root, statePath: statePath,
		authority: models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration},
		opts:      LifecycleRequestOptions{RequestID: "replace-1", ExpectedTransition: models.TaskTransitionID(s.FindTask("source"))},
		input: ReplaceTaskInput{SourceTaskID: "source", Reason: "reviewed correction", Consumers: updates,
			Replacement: AddTaskInput{ID: "replacement", RolePair: "coding-pair", Description: "private replacement description", SpecRef: "README.md", DoneWhen: "private completion criteria", Scope: "private scope text", Priority: 1}},
	}
}

func (f replacementFixture) run() (*ReplaceTaskResult, error) {
	return ReplaceTaskWithAuthorityAndOptions(f.root, f.input, f.authority, f.opts)
}

func replacementBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func replacementState(t *testing.T, f replacementFixture) *models.State {
	t.Helper()
	s, err := db.New(f.statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func assertReplacementCommitted(t *testing.T, s *models.State) {
	t.Helper()
	source := s.FindTask("source")
	if source.Status != models.TaskStatusSuperseded || !slices.Equal(source.SupersededBy, []string{"replacement"}) {
		t.Fatalf("source lineage: %+v", source)
	}
	if s.FindTask("replacement") == nil || !slices.Contains(s.Sprint.Scope.Planned, "replacement") {
		t.Fatal("replacement missing from tasks or sprint")
	}
	for _, id := range []string{"consumer-a", "consumer-b"} {
		if !slices.Equal(s.FindTask(id).DependsOn, []string{"replacement"}) {
			t.Fatalf("consumer %s not repointed", id)
		}
	}
}

func TestReplaceTask_CommitsAtomically(t *testing.T) {
	f := newReplacementFixture(t)
	var calls atomic.Int32
	t.Cleanup(setLifecycleMutationTestHook(db.For(f.statePath), func() { calls.Add(1) }))
	r, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("mutation callbacks = %d", calls.Load())
	}
	if r.Outcome != models.LifecycleCompleted || r.SafeAction != "continue" || r.Effects != "committed" || r.Changed == nil || !*r.Changed {
		t.Fatalf("result: %+v", r)
	}
	if !slices.Equal(r.RetargetedConsumers, []string{"consumer-a", "consumer-b"}) {
		t.Fatalf("consumers: %v", r.RetargetedConsumers)
	}
	assertReplacementCommitted(t, replacementState(t, f))
}

func TestReplaceTask_NoIntermediateLineage(t *testing.T) {
	f := newReplacementFixture(t)
	before := replacementBytes(t, f.statePath)
	var observed bool
	replaceTaskCandidateTestHooks.Store(db.For(f.statePath), replaceTaskTestHooks{afterUpdates: func(s *models.State) error {
		observed = true
		if s.FindTask("replacement") == nil || !slices.Equal(s.FindTask("consumer-a").DependsOn, []string{"replacement"}) {
			t.Error("barrier did not reach creation and retargeting")
		}
		if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
			t.Error("candidate leaked to state file")
		}
		return nil
	}})
	t.Cleanup(func() { replaceTaskCandidateTestHooks.Delete(db.For(f.statePath)) })
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("callback barrier not reached")
	}
	assertReplacementCommitted(t, replacementState(t, f))
}

func TestSliceCP4_AuditIncludesImplicitConsumers(t *testing.T) {
	for _, mode := range []string{"empty", "partial", "overlapping", "reverse explicit"} {
		t.Run(mode, func(t *testing.T) {
			f := newReplacementFixture(t)
			switch mode {
			case "empty":
				f.input.Consumers = []models.DependencyUpdate{}
			case "partial":
				f.input.Consumers = f.input.Consumers[1:]
			case "overlapping":
				f.input.Consumers = f.input.Consumers[1:]
				f.input.Consumers[0].DesiredDependsOn = []string{"replacement", "source"}
			case "reverse explicit":
				slices.Reverse(f.input.Consumers)
			}
			// Historical rewrites and unchanged tasks are not transaction effects.
			if err := db.For(f.statePath).Modify(func(s *models.State) error {
				unrelated := testhelpers.BuildTaskByStatus("unrelated", models.TaskStatusReady, time.Now().UTC())
				unrelated.History = append(unrelated.History, models.TaskHistoryEntry{Event: models.TaskEventDependenciesRewritten, Time: time.Now().UTC()})
				s.Tasks = append(s.Tasks, unrelated)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			first, err := f.run()
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"consumer-a", "consumer-b"}
			if !slices.Equal(first.RetargetedConsumers, want) {
				t.Errorf("committed consumers = %v, want %v", first.RetargetedConsumers, want)
			}
			s := replacementState(t, f)
			assertReplacementCommitted(t, s)
			audits := historyEntries(s.FindTask("source"), models.TaskEventReplacementCommitted)
			if len(audits) != 1 {
				t.Fatalf("replacement audit count = %d", len(audits))
			}
			encoded, err := json.Marshal(audits[0].Extra)
			if err != nil {
				t.Fatal(err)
			}
			var audit struct {
				Consumers []string `json:"retargeted_consumers"`
			}
			if err := json.Unmarshal(encoded, &audit); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(audit.Consumers, want) {
				t.Errorf("audit consumers = %v, want %v", audit.Consumers, want)
			}
			if len(audits[0].Extra) != 6 {
				t.Errorf("audit gained fields beyond compact completion identifiers: %s", encoded)
			}

			// Remove old edges, add a new consumer and advance the source boundary.
			// Replay must retain the original set and original completion identity.
			if err := db.For(f.statePath).Modify(func(s *models.State) error {
				for _, id := range want {
					s.FindTask(id).DependsOn = nil
					s.FindTask(id).History = append(s.FindTask(id).History, models.TaskHistoryEntry{Event: models.TaskEventDependenciesRewritten, Time: time.Now().UTC()})
				}
				s.FindTask("unrelated").DependsOn = []string{"replacement"}
				models.AdvanceLifecycle(s.FindTask("source"))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := replacementBytes(t, f.statePath)
			logBefore := replacementBytes(t, paths.New(f.root).LogPath())
			replay, err := f.run()
			if err != nil {
				t.Fatal(err)
			}
			if replay.Outcome != models.LifecycleAlreadyCompleted || replay.Changed == nil || *replay.Changed || replay.Effects != "none" || replay.SafeAction != "stop" {
				t.Fatalf("replay: %+v", replay)
			}
			if !slices.Equal(replay.RetargetedConsumers, want) || replay.CompletedTransitionID != first.CompletedTransitionID || replay.TransitionID == first.TransitionID {
				t.Errorf("original completion not retained: %+v", replay)
			}
			if !bytes.Equal(before, replacementBytes(t, f.statePath)) || !bytes.Equal(logBefore, replacementBytes(t, paths.New(f.root).LogPath())) {
				t.Fatal("replay changed durable state or activity log")
			}
		})
	}
}

func TestSliceCP4_ImplicitConsumersRollback(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			f := newReplacementFixture(t)
			if partial {
				f.input.Consumers = f.input.Consumers[1:]
			} else {
				f.input.Consumers = []models.DependencyUpdate{}
			}
			before := replacementBytes(t, f.statePath)
			replaceTaskCandidateTestHooks.Store(db.For(f.statePath), replaceTaskTestHooks{afterUpdates: func(s *models.State) error {
				// Supersession rewrites both consumers before full-state validation
				// rejects this unrelated missing dependency.
				consumer := s.FindTask("consumer-a")
				consumer.DependsOn = append(consumer.DependsOn, "missing")
				return nil
			}})
			t.Cleanup(func() { replaceTaskCandidateTestHooks.Delete(db.For(f.statePath)) })
			result, err := f.run()
			if result != nil || err == nil || !strings.Contains(err.Error(), "missing") {
				t.Fatalf("candidate validation: result=%+v error=%v", result, err)
			}
			if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
				t.Fatal("failed candidate persisted graph, audit or receipt changes")
			}
		})
	}
}

func TestSliceCP4_ReplayRequiresMatchingConsumerAudit(t *testing.T) {
	for _, missingField := range []string{"source_new_transition_id", "retargeted_consumers"} {
		t.Run(missingField, func(t *testing.T) {
			f := newReplacementFixture(t)
			f.input.Consumers = []models.DependencyUpdate{}
			if _, err := f.run(); err != nil {
				t.Fatal(err)
			}
			if err := db.For(f.statePath).Modify(func(s *models.State) error {
				for _, entry := range s.FindTask("source").History {
					if entry.Event == models.TaskEventReplacementCommitted {
						delete(entry.Extra, missingField)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := replacementBytes(t, f.statePath)
			result, err := f.run()
			requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "none")
			if result != nil || !bytes.Equal(before, replacementBytes(t, f.statePath)) {
				t.Fatal("unavailable completion audit produced a result or changed state")
			}
		})
	}
}

func TestReplaceTask_RollsBackInjectedFailure(t *testing.T) {
	f := newReplacementFixture(t)
	before := replacementBytes(t, f.statePath)
	injected := errors.New("injected failure after creation and retargeting")
	replaceTaskCandidateTestHooks.Store(db.For(f.statePath), replaceTaskTestHooks{afterUpdates: func(s *models.State) error {
		if s.FindTask("replacement") == nil || !slices.Equal(s.FindTask("consumer-b").DependsOn, []string{"replacement"}) {
			t.Error("failure injected too early")
		}
		return injected
	}})
	t.Cleanup(func() { replaceTaskCandidateTestHooks.Delete(db.For(f.statePath)) })
	_, err := f.run()
	if !errors.Is(err, injected) {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
		t.Fatal("failure changed state or history")
	}
}

func TestReplaceTask_RejectsInvalidDependencyShape(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		change        func(*replacementFixture)
	}{
		{"stale", models.LifecycleStateChanged, func(f *replacementFixture) { f.input.Consumers[1].ExpectedDependsOn = []string{} }},
		{"downstream", models.LifecycleInvalidInput, func(f *replacementFixture) {
			f.input.Replacement.RolePair = "code-planning-pair"
			f.input.Replacement.DependsOn = []string{"consumer-a"}
		}},
		{"cycle", models.LifecycleInvalidInput, func(f *replacementFixture) { f.input.Replacement.DependsOn = []string{"consumer-a"} }},
		{"collision", models.LifecycleInvalidInput, func(f *replacementFixture) { f.input.Replacement.ID = "consumer-a" }},
		{"stale-transition", models.LifecycleStateChanged, func(f *replacementFixture) { f.opts.ExpectedTransition = strings.Repeat("a", 64) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReplacementFixture(t)
			tc.change(&f)
			before := replacementBytes(t, f.statePath)
			_, err := f.run()
			var le *LifecycleError
			if !errors.As(err, &le) || le.Outcome.Outcome != tc.outcome || le.Outcome.Effects != "none" {
				t.Fatalf("error: %v (%+v)", err, le)
			}
			if tc.outcome == models.LifecycleStateChanged && le.Outcome.SafeAction != "requery" {
				t.Fatalf("action: %s", le.Outcome.SafeAction)
			}
			if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
				t.Fatal("rejection wrote state")
			}
		})
	}
}

func replacementGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestReplaceTask_PreservedBaseDeclared(t *testing.T) {
	for _, name := range []string{"healthy", "omitted", "base-only", "unresolvable", "missing", "unhealthy", "wrong-branch", "noncanonical", "not-ancestor"} {
		t.Run(name, func(t *testing.T) {
			f := newReplacementFixture(t)
			base := replacementGit(t, f.root, "rev-parse", "HEAD")
			wt := filepath.Join(paths.WorktreesDirName, "replacement")
			if name != "omitted" {
				f.input.PreservedBase = &PreservedTaskBase{BaseCommit: base, Worktree: wt}
				if name != "missing" && name != "base-only" {
					replacementGit(t, f.root, "worktree", "add", "-b", paths.TaskBranchPrefix+"replacement", wt, base)
				}
			}
			switch name {
			case "base-only":
				f.input.PreservedBase.Worktree = ""
			case "unresolvable":
				f.input.PreservedBase.BaseCommit = strings.Repeat("f", 40)
			case "unhealthy":
				if err := os.Remove(filepath.Join(f.root, wt, ".git")); err != nil {
					t.Fatal(err)
				}
			case "wrong-branch":
				replacementGit(t, filepath.Join(f.root, wt), "checkout", "-b", "wrong-branch")
			case "noncanonical":
				f.input.PreservedBase.Worktree = "../replacement"
			case "not-ancestor":
				replacementGit(t, f.root, "commit", "--allow-empty", "-m", "later base")
				f.input.PreservedBase.BaseCommit = replacementGit(t, f.root, "rev-parse", "HEAD")
			}
			before := replacementBytes(t, f.statePath)
			_, err := f.run()
			if name != "healthy" && name != "omitted" {
				var le *LifecycleError
				if !errors.As(err, &le) || le.Outcome.Outcome != models.LifecycleInvalidInput {
					t.Fatalf("error: %v", err)
				}
				if name == "base-only" && (len(le.Outcome.Diagnostics) != 1 || le.Outcome.Diagnostics[0].Field != "/preserved_base/worktree") {
					t.Fatalf("diagnostic: %+v", le.Outcome.Diagnostics)
				}
				if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
					t.Fatal("invalid base wrote state")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			rep := replacementState(t, f).FindTask("replacement")
			if name == "omitted" {
				if rep.BaseCommit != nil || rep.Worktree != nil {
					t.Fatal("omitted base persisted fields")
				}
				return
			}
			if rep.BaseCommit == nil || *rep.BaseCommit != base || rep.Worktree == nil || *rep.Worktree != wt {
				t.Fatalf("preserved base: %+v", rep)
			}
		})
	}
}

func TestReplaceTask_ConcurrentIdenticalAttempts(t *testing.T) {
	f := newReplacementFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	contending := make(chan struct{})
	var lockAttempts atomic.Int32
	replaceTaskCandidateTestHooks.Store(db.For(f.statePath), replaceTaskTestHooks{beforeLock: func() {
		if lockAttempts.Add(1) == 2 {
			close(contending)
		}
	}})
	t.Cleanup(func() { replaceTaskCandidateTestHooks.Delete(db.For(f.statePath)) })
	var calls atomic.Int32
	t.Cleanup(setLifecycleMutationTestHook(db.For(f.statePath), func() {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}))
	type attempt struct {
		result *ReplaceTaskResult
		err    error
	}
	results := make(chan attempt, 2)
	run := func() { r, err := f.run(); results <- attempt{r, err} }
	go run()
	<-entered
	go run()
	// The first call owns the task lock while the second has finished its
	// unlocked reads and reached that same lock boundary.
	<-contending
	close(release)
	outcomes := map[string]int{}
	for range 2 {
		a := <-results
		if a.err != nil {
			t.Fatal(a.err)
		}
		outcomes[a.result.Outcome]++
	}
	if outcomes[models.LifecycleCompleted] != 1 || outcomes[models.LifecycleAlreadyCompleted] != 1 {
		t.Fatalf("outcomes: %v", outcomes)
	}
	s := replacementState(t, f)
	assertReplacementCommitted(t, s)
	count := 0
	for _, task := range s.Tasks {
		if task.ID == "replacement" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("replacement count: %d", count)
	}
	count = 0
	for _, h := range s.FindTask("source").History {
		if h.Event == models.TaskEventReplacementCommitted {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("audit count: %d", count)
	}
}

func TestReplaceTask_IdenticalRetry(t *testing.T) {
	f := newReplacementFixture(t)
	first, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	before := replacementBytes(t, f.statePath)
	logBefore := replacementBytes(t, paths.New(f.root).LogPath())
	sequence := replacementState(t, f).FindTask("source").Lifecycle.CompletionSequence
	r, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != models.LifecycleAlreadyCompleted || r.Changed == nil || *r.Changed || r.Effects != "none" || r.SafeAction != "stop" {
		t.Fatalf("replay: %+v", r)
	}
	source := replacementState(t, f).FindTask("source")
	if r.ReplacementTaskID != source.SupersededBy[0] || r.SourceOriginalStatus != first.SourceOriginalStatus || r.CompletedTransitionID != first.CompletedTransitionID || !slices.Equal(r.RetargetedConsumers, first.RetargetedConsumers) {
		t.Fatalf("original result lost: %+v", r)
	}
	if source.Lifecycle.CompletionSequence != sequence || !bytes.Equal(before, replacementBytes(t, f.statePath)) || !bytes.Equal(logBefore, replacementBytes(t, paths.New(f.root).LogPath())) {
		t.Fatal("replay changed state, sequence, audit or activity log")
	}
	metrics := ReadLifecycleOutcomes(f.root, CaptureLifecycleSprint(replacementState(t, f).Sprint))
	if metrics.Counts["replace-task"][models.LifecycleAlreadyCompleted] != 1 {
		t.Fatalf("replay counter: %+v", metrics)
	}
}

func TestSliceCP4_ReplayAfterPreservedWorktreeCleanup(t *testing.T) {
	f := newReplacementFixture(t)
	base := replacementGit(t, f.root, "rev-parse", "HEAD")
	wt := filepath.Join(paths.WorktreesDirName, "replacement")
	replacementGit(t, f.root, "worktree", "add", "-b", paths.TaskBranchPrefix+"replacement", wt, base)
	f.input.PreservedBase = &PreservedTaskBase{BaseCommit: base, Worktree: wt}
	first, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	// Only the temporary successor worktree is removed. The source receipt,
	// caller generation, request pair and payload remain intact.
	replacementGit(t, f.root, "worktree", "remove", wt)
	before := replacementBytes(t, f.statePath)
	logBefore := replacementBytes(t, paths.New(f.root).LogPath())
	worktreesBefore := replacementGit(t, f.root, "worktree", "list", "--porcelain")
	refsBefore := replacementGit(t, f.root, "show-ref")
	replay, err := f.run()
	if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
		t.Fatal("replay changed durable state, history or receipts")
	}
	if err != nil {
		t.Fatalf("retained exact receipt must survive worktree cleanup: %v", err)
	}
	if replay.Outcome != models.LifecycleAlreadyCompleted || replay.Changed == nil || *replay.Changed || replay.Effects != "none" || replay.SafeAction != "stop" {
		t.Fatalf("unexpected replay: %+v", replay)
	}
	if replay.SourceTaskID != first.SourceTaskID || replay.ReplacementTaskID != first.ReplacementTaskID || replay.SourceOriginalStatus != first.SourceOriginalStatus || replay.CompletedTransitionID != first.CompletedTransitionID || !slices.Equal(replay.RetargetedConsumers, first.RetargetedConsumers) {
		t.Fatalf("original completion evidence lost: %+v", replay)
	}
	if !bytes.Equal(logBefore, replacementBytes(t, paths.New(f.root).LogPath())) || worktreesBefore != replacementGit(t, f.root, "worktree", "list", "--porcelain") || refsBefore != replacementGit(t, f.root, "show-ref") || len(replay.Warnings) != 0 {
		t.Fatal("replay repeated activity or Git cleanup effects")
	}
}

func TestReplaceTask_CleanedPreservedBaseReplayGuards(t *testing.T) {
	for _, name := range []string{"payload conflict", "stale caller", "generation changes before lock", "lineage mismatch", "expired receipt"} {
		t.Run(name, func(t *testing.T) {
			f := newReplacementFixture(t)
			base := replacementGit(t, f.root, "rev-parse", "HEAD")
			wt := filepath.Join(paths.WorktreesDirName, "replacement")
			replacementGit(t, f.root, "worktree", "add", "-b", paths.TaskBranchPrefix+"replacement", wt, base)
			f.input.PreservedBase = &PreservedTaskBase{BaseCommit: base, Worktree: wt}
			if _, err := f.run(); err != nil {
				t.Fatal(err)
			}
			replacementGit(t, f.root, "worktree", "remove", wt)
			s := replacementState(t, f)
			want, action := models.LifecycleStateChanged, "requery"
			switch name {
			case "payload conflict":
				f.input.Replacement.Description = "different intent"
				want, action = models.LifecycleConflict, "stop"
			case "stale caller":
				f.authority.Generation = "retired-generation"
				want, action = models.LifecycleStaleCaller, "stop"
			case "generation changes before lock":
				t.Cleanup(setLifecycleMutationTestHook(db.For(f.statePath), func() {
					agent := s.Agents[f.authority.ID]
					agent.Generation = "new-generation"
					s.Agents[f.authority.ID] = agent
					testhelpers.WriteInitialState(t, f.statePath, s)
				}))
				want, action = models.LifecycleStaleCaller, "stop"
			case "lineage mismatch":
				s.FindTask("source").SupersededBy = []string{"other"}
				testhelpers.WriteInitialState(t, f.statePath, s)
			case "expired receipt":
				s.FindTask("source").Lifecycle.Receipts = nil
				testhelpers.WriteInitialState(t, f.statePath, s)
			}
			before := replacementBytes(t, f.statePath)
			logBefore := replacementBytes(t, paths.New(f.root).LogPath())
			_, err := f.run()
			requireLifecycleError(t, err, want, action, "none")
			if name == "payload conflict" {
				requireReplacementConflict(t, err, "request_id")
			}
			if name == "generation changes before lock" {
				if !reflect.DeepEqual(replacementState(t, f), s) {
					t.Fatal("rejected replay changed state beyond the generation fence")
				}
			} else if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
				t.Fatal("rejected replay wrote state")
			}
			if !bytes.Equal(logBefore, replacementBytes(t, paths.New(f.root).LogPath())) {
				t.Fatal("rejected replay wrote activity log")
			}
		})
	}
}

func TestReplaceTask_ReplayLineageMismatch(t *testing.T) {
	f := newReplacementFixture(t)
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	s := replacementState(t, f)
	s.FindTask("source").SupersededBy = []string{"other"}
	testhelpers.WriteInitialState(t, f.statePath, s)
	before := replacementBytes(t, f.statePath)
	_, err := f.run()
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "none")
	if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
		t.Fatal("mismatch wrote state")
	}
}

func requireReplacementConflict(t *testing.T, err error, field string) {
	t.Helper()
	requireLifecycleError(t, err, models.LifecycleConflict, "stop", "none")
	var le *LifecycleError
	if !errors.As(err, &le) {
		t.Fatal(err)
	}
	if le.Outcome.Changed != nil || len(le.Outcome.Diagnostics) != 1 {
		t.Fatalf("conflict: %+v", le.Outcome)
	}
	d := le.Outcome.Diagnostics[0]
	if d.Field != field || d.SchemaVersion != 1 || d.ValueClass != models.FieldValueClassConflict || d.SafeAction != models.FieldDiagnosticRequery || d.Constraint == "" {
		t.Fatalf("diagnostic: %+v", d)
	}
}

func TestReplaceTask_ConflictingRetry(t *testing.T) {
	for _, field := range []string{"replacement.id", "request_id"} {
		t.Run(field, func(t *testing.T) {
			f := newReplacementFixture(t)
			if _, err := f.run(); err != nil {
				t.Fatal(err)
			}
			before := replacementBytes(t, f.statePath)
			if field == "replacement.id" {
				f.input.Replacement.ID = "other"
			} else {
				f.input.Replacement.Description = "changed private description"
			}
			_, err := f.run()
			requireReplacementConflict(t, err, field)
			if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
				t.Fatal("conflict wrote state")
			}
			if strings.Contains(err.Error(), f.input.Replacement.Description) {
				t.Fatal("error leaked payload")
			}
			metrics := ReadLifecycleOutcomes(f.root, CaptureLifecycleSprint(replacementState(t, f).Sprint))
			if metrics.Counts["replace-task"][models.LifecycleConflict] != 1 {
				t.Fatalf("conflict counter: %+v", metrics)
			}
		})
	}
}

func TestReplaceTask_BatchIDCollision(t *testing.T) {
	f := newReplacementFixture(t)
	input := &AddTasksInput{Tasks: []AddTaskInput{f.input.Replacement, f.input.Replacement}, OrchestratorID: f.authority.ID}
	batch, err := AddTasks(f.statePath, paths.New(f.root).LogPath(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Results) != 2 || !batch.Results[0].Success || batch.Results[1].Success {
		t.Fatalf("batch partiality: %+v", batch)
	}
	before := replacementBytes(t, f.statePath)
	_, err = f.run()
	requireReplacementConflict(t, err, "replacement.id")
	if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
		t.Fatal("ID collision wrote state")
	}
}

func TestReplaceTask_ConcurrentConflictingAttempts(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		name := "original-first"
		if reversed {
			name = "changed-first"
		}
		t.Run(name, func(t *testing.T) {
			f := newReplacementFixture(t)
			other := f
			other.input.Replacement.Description = "different description"
			if reversed {
				f, other = other, f
			}
			entered, release, contending := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var attempts, calls atomic.Int32
			replaceTaskCandidateTestHooks.Store(db.For(f.statePath), replaceTaskTestHooks{beforeLock: func() {
				if attempts.Add(1) == 2 {
					close(contending)
				}
			}})
			t.Cleanup(func() { replaceTaskCandidateTestHooks.Delete(db.For(f.statePath)) })
			t.Cleanup(setLifecycleMutationTestHook(db.For(f.statePath), func() {
				if calls.Add(1) == 1 {
					close(entered)
					<-release
				}
			}))
			type attempt struct {
				result *ReplaceTaskResult
				err    error
			}
			results := make(chan attempt, 2)
			go func() { r, err := f.run(); results <- attempt{r, err} }()
			<-entered
			go func() { r, err := other.run(); results <- attempt{r, err} }()
			<-contending
			close(release)
			completed, conflicts := 0, 0
			for range 2 {
				a := <-results
				if a.err != nil {
					requireReplacementConflict(t, a.err, "request_id")
					conflicts++
				} else {
					if a.result.Outcome != models.LifecycleCompleted {
						t.Fatalf("winner: %+v", a.result)
					}
					completed++
				}
			}
			if completed != 1 || conflicts != 1 {
				t.Fatalf("completed=%d conflicts=%d", completed, conflicts)
			}
			s := replacementState(t, f)
			assertReplacementCommitted(t, s)
			if len(s.Tasks) != 4 || s.FindTask("replacement").Description != f.input.Replacement.Description || s.FindTask("source").Lifecycle.CompletionSequence != 1 {
				t.Fatal("wrong winner or duplicate lineage")
			}
			audits := 0
			for _, h := range s.FindTask("source").History {
				if h.Event == models.TaskEventReplacementCommitted {
					audits++
				}
			}
			if audits != 1 {
				t.Fatalf("audits: %d", audits)
			}
		})
	}
}

func TestReplaceTask_RequiresRequestIdentity(t *testing.T) {
	for _, field := range []string{"request_id", "expected_transition", "both"} {
		t.Run(field, func(t *testing.T) {
			f := newReplacementFixture(t)
			if field != "expected_transition" {
				f.opts.RequestID = ""
			}
			if field != "request_id" {
				f.opts.ExpectedTransition = ""
			}
			before := replacementBytes(t, f.statePath)
			_, err := f.run()
			requireLifecycleError(t, err, models.LifecycleInvalidInput, "correct_input", "none")
			var le *LifecycleError
			if !errors.As(err, &le) {
				t.Fatal(err)
			}
			want := field
			if field == "both" {
				want = "request_id"
			}
			if len(le.Outcome.Diagnostics) == 0 || le.Outcome.Diagnostics[0].Field != want || le.Outcome.Diagnostics[0].ValueClass != models.FieldValueClassMissing {
				t.Fatalf("diagnostics: %+v", le.Outcome.Diagnostics)
			}
			if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
				t.Fatal("missing identity wrote state")
			}
		})
	}
}

func TestReplaceTask_AuditRecord(t *testing.T) {
	f := newReplacementFixture(t)
	r, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	s := replacementState(t, f)
	if models.TaskTransitionID(s.FindTask("source")) != r.TransitionID {
		t.Fatal("returned transition differs from persisted boundary")
	}
	for _, h := range s.FindTask("source").History {
		if h.Event != models.TaskEventReplacementCommitted {
			continue
		}
		if h.Extra["source_task_id"] != "source" || h.Extra["replacement_task_id"] != "replacement" || h.Extra["source_prior_transition_id"] != f.opts.ExpectedTransition || h.Extra["source_new_transition_id"] != r.TransitionID || h.Extra["request_id"] != f.opts.RequestID {
			t.Fatalf("audit: %+v", h)
		}
		b, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{f.input.Replacement.Description, f.input.Replacement.Scope, f.input.Replacement.DoneWhen} {
			if bytes.Contains(b, []byte(forbidden)) {
				t.Fatal("audit contains task payload")
			}
		}
		if !bytes.Contains(b, []byte("consumer-a")) || !bytes.Contains(b, []byte("consumer-b")) {
			t.Fatal("audit omits consumers")
		}
		return
	}
	t.Fatal("missing replacement audit")
}

func TestReplaceTask_Counters(t *testing.T) {
	f := newReplacementFixture(t)
	s := replacementState(t, f)
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	metrics := ReadLifecycleOutcomes(f.root, CaptureLifecycleSprint(s.Sprint))
	if !metrics.Available || metrics.Counts["replace-task"][models.LifecycleCompleted] != 1 {
		t.Fatalf("metrics: %+v", metrics)
	}
	for _, operation := range []string{"supersede-task", "apply-dependency-repair"} {
		if metrics.Counts[operation][models.LifecycleCompleted] != 0 {
			t.Fatalf("nested %s counted", operation)
		}
	}
}

func TestReplaceTask_Authority(t *testing.T) {
	f := newReplacementFixture(t)
	before := replacementBytes(t, f.statePath)
	f.authority.Generation = "retired-generation"
	_, err := f.run()
	var le *LifecycleError
	if !errors.As(err, &le) || le.Outcome.Outcome != models.LifecycleStaleCaller || le.Outcome.SafeAction != "stop" || le.Outcome.Effects != "none" {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
		t.Fatal("stale authority wrote state")
	}
}

func TestReplaceTask_SourceEligibility(t *testing.T) {
	for _, status := range []models.TaskStatus{models.TaskStatusReady, models.TaskStatusRejected, models.TaskStatusBlocked, models.TaskStatusIntegrationFailed, models.TaskStatusImplementing, models.TaskStatusMerged, models.TaskStatusSuperseded} {
		t.Run(string(status), func(t *testing.T) {
			f := newReplacementFixture(t)
			s := replacementState(t, f)
			s.Tasks[0] = testhelpers.BuildTaskByStatus("source", status, time.Now().UTC())
			testhelpers.WriteInitialState(t, f.statePath, s)
			f.opts.ExpectedTransition = models.TaskTransitionID(s.FindTask("source"))
			before := replacementBytes(t, f.statePath)
			_, err := f.run()
			allowed := status == models.TaskStatusReady || status == models.TaskStatusRejected || status == models.TaskStatusBlocked || status == models.TaskStatusIntegrationFailed
			if allowed {
				if err != nil {
					t.Fatal(err)
				}
				assertReplacementCommitted(t, replacementState(t, f))
				return
			}
			var le *LifecycleError
			if !errors.As(err, &le) || le.Outcome.Outcome != models.LifecycleAlreadyTransitioned || le.Outcome.SafeAction != "stop" {
				t.Fatalf("error: %v", err)
			}
			if !bytes.Equal(before, replacementBytes(t, f.statePath)) {
				t.Fatal("ineligible source changed state")
			}
		})
	}
}

func TestReplaceTask_PostCommitCleanup(t *testing.T) {
	f := newReplacementFixture(t)
	base := replacementGit(t, f.root, "rev-parse", "HEAD")
	wt := filepath.Join(paths.WorktreesDirName, "source")
	replacementGit(t, f.root, "worktree", "add", "-b", paths.TaskBranchPrefix+"source", wt, base)
	s := replacementState(t, f)
	s.FindTask("source").Worktree = &wt
	s.FindTask("source").BaseCommit = &base
	testhelpers.WriteInitialState(t, f.statePath, s)
	// Force only the post-commit activity append to fail.
	if err := os.Mkdir(paths.New(f.root).LogPath(), 0700); err != nil {
		t.Fatal(err)
	}
	r, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) == 0 || r.Outcome != models.LifecycleCompleted {
		t.Fatalf("result: %+v", r)
	}
	assertReplacementCommitted(t, replacementState(t, f))
	if _, err := os.Stat(filepath.Join(f.root, wt)); !os.IsNotExist(err) {
		t.Fatalf("source worktree remains: %v", err)
	}
	if got := replacementGit(t, f.root, "rev-parse", paths.TaskBranchPrefix+"source"); got != base {
		t.Fatal("source branch not preserved")
	}
}

func TestReplaceTask_SamePairReplacementKeepsParentLineage(t *testing.T) {
	parentID := "planning-parent"
	for _, tc := range []struct {
		name            string
		lineage         func(source *models.Task)
		replacementPair string
		wantParent      *string
		wantParents     []string
	}{
		{name: "singular parent", lineage: func(source *models.Task) { source.ParentTask = &parentID }, replacementPair: "coding-pair", wantParent: &parentID},
		{name: "plural parents", lineage: func(source *models.Task) { source.ParentTasks = []string{parentID, "other-parent"} }, replacementPair: "coding-pair", wantParents: []string{parentID, "other-parent"}},
		{name: "cross-pair replacement", lineage: func(source *models.Task) { source.ParentTasks = []string{parentID} }, replacementPair: "code-planning-pair"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a source task that belongs to a parent's allocation
			f := newReplacementFixture(t)
			if err := db.New(f.statePath).Modify(func(s *models.State) error {
				for _, id := range []string{parentID, "other-parent"} {
					s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, time.Now().UTC()))
				}
				source := s.FindTask("source")
				source.RolePair = "coding-pair"
				tc.lineage(source)
				f.opts.ExpectedTransition = models.TaskTransitionID(source)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			f.input.Replacement.RolePair = tc.replacementPair

			// WHEN it is replaced
			if _, err := f.run(); err != nil {
				t.Fatal(err)
			}

			// THEN only a same-pair replacement continues the parent lineage
			replacement := replacementState(t, f).FindTask("replacement")
			if !reflect.DeepEqual(replacement.ParentTask, tc.wantParent) || !slices.Equal(replacement.ParentTasks, tc.wantParents) {
				t.Fatalf("replacement lineage = %v / %v, want %v / %v", replacement.ParentTask, replacement.ParentTasks, tc.wantParent, tc.wantParents)
			}
			if replacement.EpicRef != "" {
				t.Fatalf("replacement inherited epic_ref %q", replacement.EpicRef)
			}
		})
	}
}
