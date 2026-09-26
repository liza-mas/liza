package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	creationGoalRef = "specs/acceptance-goal.md"
	creationPlanRef = "specs/acceptance-plan.md#Task 1"
)

var creationValidation = []string{"sh boundary_test.sh"}

// newAcceptanceCreationFixture extends the replacement fixture with a strict
// carrier on integration whose Task 1 section is allocated to "source" by an
// independently approved merged planning parent.
func newAcceptanceCreationFixture(t *testing.T) replacementFixture {
	t.Helper()
	f := newReplacementFixture(t)
	write := func(name, contents string) {
		t.Helper()
		filename := filepath.Join(f.root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, f.root, "add", name)
	}
	write(creationGoalRef, "# Boundary\n\n## Identity\nReject malformed identity.\n")
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: record boundary requirements")
	sourceCommit := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	write("specs/acceptance-plan.md", fmt.Sprintf("# Code plan\n\n## Source References\nSource revision: %q\n\n### Direct References\n- \"identity\": \"%s#Identity\"\n\n### Obligation Coverage\n- \"AC-identity\" -> \"identity\"\n\n## Task 1\n\n### Acceptance Contract\n```json\n{\"version\":1,\"manifest\":\"acceptance/task-1.json\",\"obligations\":[\"AC-identity\"],\"validation\":[\"sh boundary_test.sh\"],\"timeout_seconds\":10,\"approved_proofs\":[]}\n```\n", sourceCommit, creationGoalRef))
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: independently reviewed acceptance allocation")
	parentCommit := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	testhelpers.MustGit(t, f.root, "branch", "-f", "integration", "HEAD")

	s := replacementState(t, f)
	parentID, planner, approver := "acceptance-parent", "code-planner-1", "code-plan-reviewer-1"
	source := s.FindTask("source")
	source.PlanRef, source.SpecRef, source.Validation = creationPlanRef, creationGoalRef, creationValidation
	source.ParentTask = &parentID
	// Start from a state-valid MERGED record (handoff events), then give it
	// the reviewed allocation's real commits, authorship and approval.
	parent := testhelpers.BuildTaskByStatus(parentID, models.TaskStatusMerged, time.Now().UTC())
	parent.Type, parent.RolePair, parent.SpecRef = models.TaskTypePlanning, "code-planning-pair", creationGoalRef
	parent.AssignedTo, parent.ApprovedBy = &planner, &approver
	parent.Approvals = []models.Approval{{Agent: approver, Timestamp: time.Now().UTC()}}
	parent.BaseCommit, parent.ReviewCommit, parent.MergeCommit = &sourceCommit, &parentCommit, &parentCommit
	parent.Output = []models.OutputEntry{{Desc: "task 1", DoneWhen: "identity rejected", Scope: "identity",
		PlanRef: creationPlanRef, SpecRef: creationGoalRef, Validation: creationValidation}}
	s.Tasks = append(s.Tasks, parent)
	testhelpers.WriteInitialState(t, f.statePath, s)

	f.input.Replacement.PlanRef = creationPlanRef
	f.input.Replacement.SpecRef = creationGoalRef
	f.input.Replacement.Validation = append([]string{}, creationValidation...)
	return f
}

func adHocCodingInput(planRef string) *AddTaskInput {
	return &AddTaskInput{ID: "adhoc", RolePair: "coding-pair", Description: "ad-hoc repair", SpecRef: creationGoalRef,
		PlanRef: planRef, Validation: append([]string{}, creationValidation...), DoneWhen: "repaired", Scope: "repair", Priority: 1}
}

// requireCreationRefusal asserts the typed INVALID_INPUT outcome carrying one
// field diagnostic, and that nothing was persisted.
func requireCreationRefusal(t *testing.T, f replacementFixture, before []byte, err error, operation, field string) *LifecycleError {
	t.Helper()
	var lifecycle *LifecycleError
	if !errors.As(err, &lifecycle) {
		t.Fatalf("%s accepted a task every claim at integration refuses, or refused untyped: err=%v", operation, err)
	}
	o := lifecycle.Outcome
	if o.Operation != operation || o.Outcome != models.LifecycleInvalidInput || o.SafeAction != models.FieldDiagnosticCorrectInput || len(o.Diagnostics) != 1 {
		t.Fatalf("err=%v; want one %s INVALID_INPUT/correct_input diagnostic, outcome = %+v", err, operation, o)
	}
	if d := o.Diagnostics[0]; d.Field != field || d.ValueClass != models.FieldValueClassConflict {
		t.Fatalf("diagnostic = %+v, want field %q class %q", d, field, models.FieldValueClassConflict)
	}
	if after := replacementBytes(t, f.statePath); string(before) != string(after) {
		t.Fatal("refused creation changed state")
	}
	return lifecycle
}

// The red cases below are only meaningful if the fixture's allocated child is
// claimable and each refused shape is refused by claim's own predicate.
func TestAcceptanceCreationFixture_ClaimPredicateBaseline(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	s := replacementState(t, f)
	integration := testhelpers.MustGit(t, f.root, "rev-parse", "integration")
	if input, err := loadAcceptanceInput(f.root, s, s.FindTask("source"), integration); err != nil || input == nil {
		t.Fatalf("allocated child must adopt the reviewed source: input=%v err=%v", input, err)
	}
	withValidation := *s.FindTask("source")
	withValidation.Validation = append(append([]string{}, creationValidation...), "ruff check .")
	for _, tc := range []struct {
		name   string
		task   *models.Task
		reason string
	}{
		{"no parent", &models.Task{ID: "adhoc", Type: models.TaskTypeCoding, PlanRef: creationPlanRef, SpecRef: creationGoalRef, Validation: creationValidation}, "requires allocation by a direct independently approved merged planning parent"},
		{"slug fragment", &models.Task{ID: "adhoc", Type: models.TaskTypeCoding, PlanRef: "specs/acceptance-plan.md#task-1", SpecRef: creationGoalRef, Validation: creationValidation}, `eligible ATX heading "task-1" is missing`},
		{"validation differs", &withValidation, acceptanceValidationMismatchReason},
	} {
		var evidence *AcceptanceEvidenceError
		if _, err := loadAcceptanceInput(f.root, s, tc.task, integration); !errors.As(err, &evidence) || !strings.Contains(evidence.Reason, tc.reason) {
			t.Errorf("%s: claim predicate err=%v, want acceptance refusal %q", tc.name, err, tc.reason)
		}
	}
}

// J79: an ad-hoc coding task allocated to a contract-carrying section has no
// parent, so claim refuses it at every integration commit.
func TestAddTask_RefusesContractAllocationWithoutPlanningParent(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	before := replacementBytes(t, f.statePath)
	_, err := AddTaskWithAuthority(f.statePath, paths.New(f.root).LogPath(), adHocCodingInput(creationPlanRef), f.authority)
	requireCreationRefusal(t, f, before, err, "add-task", "plan_ref")
	// The creator gets the ref, the claim's reason and a route it can take;
	// the submission-only repair does not apply to a task that never existed.
	for _, want := range []string{creationPlanRef, "planning parent", "refused at creation", brand.Command("replace-task")} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "update-review-commit") {
		t.Errorf("refusal %q recommends a submission-only repair", err)
	}
}

// J82: a slug fragment into a present carrier never resolves at claim.
func TestAddTask_RefusesSlugFragmentIntoPresentCarrier(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	before := replacementBytes(t, f.statePath)
	_, err := AddTaskWithAuthority(f.statePath, paths.New(f.root).LogPath(), adHocCodingInput("specs/acceptance-plan.md#task-1"), f.authority)
	requireCreationRefusal(t, f, before, err, "add-task", "plan_ref")
}

// 09-24: a same-pair replacement keeps its allocating parent but changes the
// reviewed validation, so claim refuses it.
func TestReplaceTask_RefusesValidationDifferingFromReviewedCommands(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	f.input.Replacement.Validation = append(f.input.Replacement.Validation, "ruff check .")
	before := replacementBytes(t, f.statePath)
	_, err := f.run()
	lifecycle := requireCreationRefusal(t, f, before, err, "replace-task", "validation")
	if lifecycle.Outcome.TaskID != "source" {
		t.Fatalf("outcome task = %q, want the replacement source", lifecycle.Outcome.TaskID)
	}
}

func addAdHoc(t *testing.T, f replacementFixture, input *AddTaskInput) error {
	t.Helper()
	_, err := AddTaskWithAuthority(f.statePath, paths.New(f.root).LogPath(), input, f.authority)
	if err == nil && replacementState(t, f).FindTask(input.ID) == nil {
		t.Fatalf("add-task of %s reported success without persisting it", input.ID)
	}
	return err
}

// writeUncommitted creates a ref target on disk, where add-task requires it,
// without committing it to integration.
func writeUncommitted(t *testing.T, f replacementFixture, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, name), []byte("# Draft\n\n## Task 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// Creation refuses exactly what claim refuses: sections claim admits as legacy
// or marker-free, and tasks claim does not judge, are still created.
func TestAddTask_AdmitsWhatClaimAdmits(t *testing.T) {
	for name, tc := range map[string]struct {
		uncommitted string
		input       *AddTaskInput
	}{
		"marker-free exact heading":     {input: adHocCodingInput(creationGoalRef + "#Identity")},
		"carrier absent at integration": {uncommitted: "specs/not-yet-merged.md", input: adHocCodingInput("specs/not-yet-merged.md#Task 1")},
		"non-coding task with a slug": {input: &AddTaskInput{ID: "adhoc", RolePair: "code-planning-pair", Description: "plan", SpecRef: creationGoalRef,
			PlanRef: "specs/acceptance-plan.md#task-1", DoneWhen: "planned", Scope: "plan", Priority: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAcceptanceCreationFixture(t)
			if tc.uncommitted != "" {
				writeUncommitted(t, f, tc.uncommitted)
			}
			if err := addAdHoc(t, f, tc.input); err != nil {
				t.Fatalf("add-task refused a task claim admits: %v", err)
			}
		})
	}
}

// The applicability gate runs before any state or Git read, so an unreadable
// integration only affects tasks whose claim would read it.
func TestAddTask_UnresolvableIntegrationOnlyAffectsJudgedTasks(t *testing.T) {
	breakIntegration := func(t *testing.T, f replacementFixture) {
		t.Helper()
		if err := db.For(f.statePath).Modify(func(s *models.State) error {
			s.Config.IntegrationBranch = "missing-integration"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("non-coding task created", func(t *testing.T) {
		f := newAcceptanceCreationFixture(t)
		breakIntegration(t, f)
		input := &AddTaskInput{ID: "adhoc", RolePair: "code-planning-pair", Description: "plan", SpecRef: creationGoalRef,
			PlanRef: creationPlanRef, DoneWhen: "planned", Scope: "plan", Priority: 1}
		if err := addAdHoc(t, f, input); err != nil {
			t.Fatalf("non-coding creation depended on integration: %v", err)
		}
	})
	t.Run("legacy-path coding task created", func(t *testing.T) {
		f := newAcceptanceCreationFixture(t)
		breakIntegration(t, f)
		// ValidateAcceptancePath refuses credential-like names, which claim
		// therefore judges through the legacy route without reading Git.
		writeUncommitted(t, f, "specs/secret-rotation.md")
		if err := addAdHoc(t, f, adHocCodingInput("specs/secret-rotation.md#Task 1")); err != nil {
			t.Fatalf("legacy-path creation depended on integration: %v", err)
		}
	})
	for name, create := range map[string]func(replacementFixture) error{
		"judged add-task retryable": func(f replacementFixture) error { return addAdHoc(t, f, adHocCodingInput(creationPlanRef)) },
		"judged replace-task retryable": func(f replacementFixture) error {
			_, err := f.run()
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAcceptanceCreationFixture(t)
			breakIntegration(t, f)
			before := replacementBytes(t, f.statePath)
			err := create(f)
			var lifecycle *LifecycleError
			if !errors.As(err, &lifecycle) || lifecycle.Outcome.Outcome != models.LifecycleRetryable || lifecycle.Outcome.SafeAction != "retry" {
				t.Fatalf("err=%v, want a retryable outcome", err)
			}
			if string(before) != string(replacementBytes(t, f.statePath)) {
				t.Fatal("unavailable acceptance check changed state")
			}
		})
	}
}

// Each gated-out shape is one loadAcceptanceInput admits without judging, so
// the gate can neither skip a task claim would refuse nor read Git claim
// would not.
func TestAcceptanceCreationApplies_MatchesClaimPredicateGates(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	s := replacementState(t, f)
	integration := testhelpers.MustGit(t, f.root, "rev-parse", "integration")
	for name, task := range map[string]*models.Task{
		"non-coding":  {ID: "x", Type: models.TaskTypePlanning, PlanRef: "specs/acceptance-plan.md#task-1"},
		"no ref":      {ID: "x", Type: models.TaskTypeCoding},
		"legacy path": {ID: "x", Type: models.TaskTypeCoding, PlanRef: "specs/secret-rotation.md#Task 1"},
	} {
		if acceptanceCreationApplies(task) {
			t.Errorf("%s: gate judges a task claim does not", name)
		}
		if input, err := loadAcceptanceInput(f.root, s, task, integration); input != nil || err != nil {
			t.Errorf("%s: claim predicate judged a gated-out task: input=%v err=%v", name, input, err)
		}
	}
}

// J79's supported repair: a same-pair replacement keeps the allocating parent,
// is created, and its claim adopts the reviewed source.
func TestReplaceTask_AllocatedReplacementIsCreatedAndClaimable(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	if _, err := f.run(); err != nil {
		t.Fatalf("replacement continuing a valid allocation refused: %v", err)
	}
	if err := db.For(f.statePath).Modify(func(s *models.State) error {
		s.Agents["coder-1"] = testhelpers.RegisteredTestAgent(models.RoleCoder)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimTask(f.root, "replacement", "coder-1"); err != nil {
		t.Fatalf("created replacement is not claimable: %v", err)
	}
	if source := replacementState(t, f).FindTask("replacement").AcceptanceSource; source == nil || source.ParentTask != "acceptance-parent" {
		t.Fatalf("claim did not adopt the inherited allocation: %+v", source)
	}
}

// A completed replacement replays even when today's carrier would refuse it:
// the verdict is reported after replay, like the preserved base.
func TestReplaceTask_ReplayPrecedesAcceptanceCheck(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	first, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(f.root, "specs/acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, []byte(strings.Replace(string(plan), "## Task 1", "## Task one", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, f.root, "commit", "-am", "test: rename the allocated heading")
	testhelpers.MustGit(t, f.root, "branch", "-f", "integration", "HEAD")
	replay, err := f.run()
	if err != nil {
		t.Fatalf("identical completed request did not replay: %v", err)
	}
	if replay.ReplacementTaskID != first.ReplacementTaskID || replay.TransitionID != first.TransitionID {
		t.Fatalf("replay = %+v, want the completed result %+v", replay, first)
	}
}

// A parent record changing between the pre-lock check and the transaction
// invalidates the verdict: requery rather than persist on stale evidence.
func TestReplaceTask_ParentEvidenceChangeDuringCheckRequeries(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	bb := db.For(f.statePath)
	replaceTaskCandidateTestHooks.Store(bb, replaceTaskTestHooks{beforeLock: func() {
		if err := bb.Modify(func(s *models.State) error {
			parent := s.FindTask("acceptance-parent")
			parent.Approvals = append(parent.Approvals, models.Approval{Agent: "code-plan-reviewer-2", Timestamp: time.Now().UTC()})
			return nil
		}); err != nil {
			t.Error(err)
		}
	}})
	t.Cleanup(func() { replaceTaskCandidateTestHooks.Delete(bb) })
	_, err := f.run()
	var lifecycle *LifecycleError
	if !errors.As(err, &lifecycle) || lifecycle.Outcome.Outcome != models.LifecycleStateChanged || lifecycle.Outcome.SafeAction != "requery" {
		t.Fatalf("err=%v, want STATE_CHANGED/requery", err)
	}
	if replacementState(t, f).FindTask("replacement") != nil {
		t.Fatal("replacement persisted on stale parent evidence")
	}
}

// D53: a same-pair replacement that only changes dependencies continues the
// reviewed allocation: it keeps the source's parent, is created and claimable.
func TestReplaceTask_ChangedDependenciesKeepAllocation(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	if err := db.For(f.statePath).Modify(func(s *models.State) error {
		s.Agents["coder-1"] = testhelpers.RegisteredTestAgent(models.RoleCoder)
		s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus("new-provider", models.TaskStatusMerged, time.Now().UTC()))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.input.Replacement.DependsOn = []string{"new-provider"}

	if _, err := f.run(); err != nil {
		t.Fatalf("dependency-only replacement refused: %v", err)
	}
	replacement := replacementState(t, f).FindTask("replacement")
	if replacement.ParentTask == nil || *replacement.ParentTask != "acceptance-parent" || !slices.Equal(replacement.DependsOn, []string{"new-provider"}) {
		t.Fatalf("replacement lineage = parent %v deps %v, want acceptance-parent and [new-provider]", replacement.ParentTask, replacement.DependsOn)
	}
	if _, err := ClaimTask(f.root, "replacement", "coder-1"); err != nil {
		t.Fatalf("dependency-only replacement is not claimable: %v", err)
	}
	if source := replacementState(t, f).FindTask("replacement").AcceptanceSource; source == nil || source.ParentTask != "acceptance-parent" {
		t.Fatalf("claim did not adopt the inherited allocation: %+v", source)
	}
}
