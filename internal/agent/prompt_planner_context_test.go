package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPlannerProvidersPreserveExplicitOrderAndAllDiscoveredOwners(t *testing.T) {
	current := models.Task{
		ID: "master-architecture-7", Type: models.TaskTypeArchitecture,
		ParentTasks: []string{"master"}, DependsOn: []string{"z-dependency", "b-dependency", "z-dependency", "missing"},
		Decomposition: &models.DecompositionManifest{
			ReadOnlyTaskDependsOn: []string{"a-read-only", "b-dependency"},
			InterfacesConsumed:    []string{"API-1 exact", "API-2 use", "API-3 use", "API-4 missing"},
		},
	}
	state := &models.State{Tasks: []models.Task{
		current,
		{ID: "master-architecture-0", Type: models.TaskTypeArchitecture, ParentTasks: []string{"master"}, ArchRef: "arch.md"},
		{ID: "scope0-b", Type: models.TaskTypeArchitecture, ParentTasks: []string{"master"}, ArchRef: "b.md#Scope 0: Foundation"},
		{ID: "scope0-a", Type: models.TaskTypeArchitecture, ParentTasks: []string{"master"}, ArchRef: "a.md#Scope 0"},
		{ID: "not-scope0", Type: models.TaskTypeArchitecture, ParentTasks: []string{"master"}, ArchRef: "n.md#Scope 01"},
		{ID: "not-sibling", Type: models.TaskTypeArchitecture, ParentTasks: []string{"elsewhere"}, ArchRef: "x.md#Scope 0"},
		{ID: "exact", Decomposition: &models.DecompositionManifest{InterfacesOwned: []string{"API-1 exact"}}},
		{ID: "shadow", Decomposition: &models.DecompositionManifest{InterfacesOwned: []string{"API-1 unrelated"}}},
		{ID: "unique", Decomposition: &models.DecompositionManifest{InterfacesOwned: []string{"API-2: owner"}}},
		{ID: "ambiguous-b", Decomposition: &models.DecompositionManifest{InterfacesOwned: []string{"API-3 second"}}},
		{ID: "ambiguous-a", Decomposition: &models.DecompositionManifest{InterfacesOwned: []string{"API-3 first"}}},
	}}
	before := slices.Clone(current.DependsOn)
	providers, issues := plannerProviderTasks(&current, state, models.RoleArchitect)
	var ids []string
	for _, provider := range providers {
		ids = append(ids, provider.id)
	}
	want := []string{"z-dependency", "b-dependency", "missing", "a-read-only", "ambiguous-a", "ambiguous-b", "exact", "master-architecture-0", "scope0-a", "scope0-b", "unique"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("providers = %v, want %v", ids, want)
	}
	if !reflect.DeepEqual(current.DependsOn, before) {
		t.Fatal("advisory interface discovery mutated dependency edges")
	}
	if len(issues) != 2 || !strings.Contains(issues[0], "ambiguous owners [ambiguous-a, ambiguous-b]") || !strings.Contains(issues[1], "no owner found") {
		t.Fatalf("ownership issues = %v", issues)
	}
	if !strings.Contains(providers[7].reasons[0], "generated output index 0") || strings.Contains(providers[7].reasons[0], "Scope 0") {
		t.Fatalf("legacy candidate invents a Scope heading: %v", providers[7])
	}

	// More explicit providers than the old graph limit remain addressable.
	current.DependsOn = nil
	current.Decomposition = nil
	for i := 0; i < 25; i++ {
		current.DependsOn = append(current.DependsOn, fmt.Sprintf("provider-%02d", i))
	}
	providers, _ = plannerProviderTasks(&current, state, models.RoleArchitect)
	if len(providers) != 28 || providers[24].id != "provider-24" {
		t.Fatalf("provider list was capped: len=%d, providers=%v", len(providers), providers)
	}
}

func TestCodePlannerFindsParentArchitectScopeZeroCohort(t *testing.T) {
	state := &models.State{Tasks: []models.Task{
		{ID: "my-plan", Type: models.TaskTypePlanning, ParentTasks: []string{"master-architecture-4"}},
		{ID: "master-architecture-4", Type: models.TaskTypeArchitecture, ParentTasks: []string{"master"}},
		{ID: "master-architecture-0", Type: models.TaskTypeArchitecture, ParentTasks: []string{"master"}, ArchRef: "legacy.md"},
		{ID: "explicit-foundation", Type: models.TaskTypeArchitecture, ParentTasks: []string{"master"}, ArchRef: "shared.md#Scope 0: Bindings"},
		{ID: "own-cohort-foundation", Type: models.TaskTypePlanning, ParentTasks: []string{"master-architecture-4"}, ArchRef: "local.md#Scope 0"},
	}}
	providers, issues := plannerProviderTasks(&state.Tasks[0], state, models.RoleCodePlanner)
	var ids []string
	for _, provider := range providers {
		ids = append(ids, provider.id)
	}
	if !reflect.DeepEqual(ids, []string{"explicit-foundation", "master-architecture-0", "own-cohort-foundation"}) || len(issues) != 0 {
		t.Fatalf("providers=%v, issues=%v", ids, issues)
	}
}

func TestPlannerProviderArtifactsPinActualProducersAndEveryCodingUnit(t *testing.T) {
	archCommit, planCommit := strings.Repeat("a", 40), strings.Repeat("b", 40)
	state := &models.State{Tasks: []models.Task{
		{ID: "foundation", Type: models.TaskTypeArchitecture, Status: models.TaskStatusMerged, ReviewCommit: &archCommit,
			Output: []models.OutputEntry{{ArchRef: "specs/arch.md#Scope 0: Bindings"}}},
		{ID: "foundation-plan", Type: models.TaskTypePlanning, ParentTasks: []string{"foundation"}, Status: models.TaskStatusMerged,
			ReviewCommit: &planCommit, ArchRef: "specs/arch.md#Scope 0: Bindings"},
		// Integration descendants must not leak into this planning/coding lineage.
		{ID: "unrelated-analysis", Type: models.TaskTypeIntegration, ParentTasks: []string{"foundation"}, PlanRef: "wrong.md"},
	}}
	for i := 0; i < 30; i++ {
		ref := fmt.Sprintf("specs/plan.md#Unit %d: Contract", i+1)
		state.Tasks[1].Output = append(state.Tasks[1].Output, models.OutputEntry{PlanRef: ref, ArchRef: "specs/arch.md#Scope 0: Bindings"})
		state.Tasks = append(state.Tasks, models.Task{ID: fmt.Sprintf("foundation-code-%d", i), Type: models.TaskTypeCoding, ParentTasks: []string{"foundation-plan"}, Status: models.TaskStatus("DRAFT_CODE"), PlanRef: ref})
	}
	// Nonnumeric headings and persisted IDs must not be derived from Unit labels.
	state.Tasks[1].Output = append(state.Tasks[1].Output, models.OutputEntry{PlanRef: "specs/plan.md#Held observation"})
	state.Tasks = append(state.Tasks, models.Task{ID: "actual-held-id", Type: models.TaskTypeCoding, ParentTasks: []string{"foundation-plan"}, Status: models.TaskStatusBlocked, PlanRef: "specs/plan.md#Held observation"})
	// A malformed historical ancestry cycle cannot make traversal recur forever.
	state.Tasks[0].ParentTasks = []string{"actual-held-id"}
	artifacts, unavailable := plannerProviderArtifacts(&state.Tasks[0], state)
	if unavailable != "" || len(artifacts) != 2 {
		t.Fatalf("artifacts=%+v, unavailable=%q", artifacts, unavailable)
	}
	if artifacts[0].ProducerID != "foundation" || artifacts[0].Commit != archCommit || artifacts[1].ProducerID != "foundation-plan" || artifacts[1].Commit != planCommit {
		t.Fatalf("artifact producer pins=%+v", artifacts)
	}
	plan := artifacts[1]
	if len(plan.Units) != 31 || len(plan.Refs) != 31 {
		t.Fatalf("unit mappings truncated or repeated: %+v", plan)
	}
	wantHold := prompts.PlannerUnitPointer{ID: "actual-held-id", Status: "BLOCKED", PlanRef: "specs/plan.md#Held observation"}
	wantBeyondCap := prompts.PlannerUnitPointer{ID: "foundation-code-29", Status: "DRAFT_CODE", PlanRef: "specs/plan.md#Unit 30: Contract"}
	if !slices.Contains(plan.Units, wantHold) || !slices.Contains(plan.Units, wantBeyondCap) {
		t.Fatalf("missing actual units: %+v", plan.Units)
	}
}

func TestPlannerProviderArtifactsDiscloseUnavailableAttribution(t *testing.T) {
	commit := strings.Repeat("c", 40)
	state := &models.State{Tasks: []models.Task{
		{ID: "provider", Type: models.TaskTypeArchitecture, Status: models.TaskStatusImplementing, ReviewCommit: &commit,
			Output: []models.OutputEntry{{ArchRef: "specs/draft.md#Scope 0"}}},
		{ID: "plan", Type: models.TaskTypePlanning, Status: models.TaskStatusMerged, ParentTasks: []string{"provider"},
			Output: []models.OutputEntry{{PlanRef: "specs/plan.md#Unit A"}}},
		{ID: "unit-without-ref", Type: models.TaskTypeCoding, ParentTasks: []string{"plan"}, Status: models.TaskStatusReady},
	}}
	artifacts, _ := plannerProviderArtifacts(&state.Tasks[0], state)
	if len(artifacts) != 3 || artifacts[0].Commit != "" || !strings.Contains(artifacts[0].Unresolved, "unavailable as merged authority") || artifacts[1].Commit != "" || !strings.Contains(artifacts[1].Unresolved, "no reviewed commit") {
		t.Fatalf("unavailable producers falsely pinned: %+v", artifacts)
	}
	if len(artifacts[2].Units) != 1 || artifacts[2].Units[0].ID != "unit-without-ref" || artifacts[2].Commit != "" || artifacts[2].File != "" {
		t.Fatalf("missing unit ref was hidden or fabricated: %+v", artifacts[2])
	}

	state.Tasks = append(state.Tasks,
		models.Task{ID: "owner-b", Type: models.TaskTypeArchitecture, Status: models.TaskStatusMerged, ReviewCommit: &commit, Output: []models.OutputEntry{{ArchRef: "ambiguous.md#Scope X"}}},
		models.Task{ID: "owner-a", Type: models.TaskTypeArchitecture, Status: models.TaskStatusMerged, ReviewCommit: &commit, Output: []models.OutputEntry{{ArchRef: "ambiguous.md#Scope X"}}},
		models.Task{ID: "unattributed", Type: models.TaskTypePlanning, ArchRef: "ambiguous.md#Scope X"},
	)
	artifacts, unavailable := plannerProviderArtifacts(&state.Tasks[len(state.Tasks)-1], state)
	if len(artifacts) != 1 || artifacts[0].Commit != "" || !strings.Contains(artifacts[0].Unresolved, "ambiguous producing tasks [owner-a, owner-b]") || unavailable != "no produced output recorded" {
		t.Fatalf("ambiguous attribution=%+v, unavailable=%q", artifacts, unavailable)
	}
}

func TestPlannerArtifactsUseNearestManifestOwnerAndKeepOutputArchitectureRefs(t *testing.T) {
	currentCommit, historicalCommit, archCommit := strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40)
	ref := "specs/reused.md#Unit A"
	state := &models.State{Tasks: []models.Task{
		{ID: "actual-plan", Type: models.TaskTypePlanning, ParentTasks: []string{"arch"}, Status: models.TaskStatusMerged, ReviewCommit: &currentCommit,
			Output: []models.OutputEntry{{PlanRef: ref, ArchRef: "specs/secondary-arch.md#Scope 4"}}},
		{ID: "actual-code", Type: models.TaskTypeCoding, ParentTasks: []string{"actual-plan"}, Status: models.TaskStatusReady, PlanRef: ref},
		{ID: "historical-plan", Type: models.TaskTypePlanning, Status: models.TaskStatusMerged, ReviewCommit: &historicalCommit,
			Output: []models.OutputEntry{{PlanRef: ref}}},
		{ID: "arch", Type: models.TaskTypeArchitecture, Status: models.TaskStatusMerged, ReviewCommit: &archCommit,
			Output: []models.OutputEntry{{ArchRef: "specs/secondary-arch.md#Scope 4"}}},
	}}
	artifacts, _ := plannerProviderArtifacts(&state.Tasks[0], state)
	if len(artifacts) != 2 || artifacts[0].ProducerID != "actual-plan" || artifacts[0].Commit != currentCommit || len(artifacts[0].Units) != 1 || artifacts[0].Units[0].ID != "actual-code" {
		t.Fatalf("historical same-ref task displaced actual parent ownership: %+v", artifacts)
	}
	if artifacts[1].ProducerID != "arch" || artifacts[1].Commit != archCommit || artifacts[1].Ref != "specs/secondary-arch.md#Scope 4" {
		t.Fatalf("output-only inherited architecture ref lost attribution: %+v", artifacts)
	}
	// Multiple exact parent manifests remain ambiguous rather than selecting
	// the first parent or the newest unrelated producer.
	state.Tasks[1].ParentTasks = []string{"actual-plan", "historical-plan"}
	artifacts, _ = plannerProviderArtifacts(&state.Tasks[1], state)
	if len(artifacts) != 1 || artifacts[0].Commit != "" || !strings.Contains(artifacts[0].Unresolved, "ambiguous producing tasks [actual-plan, historical-plan]") {
		t.Fatalf("multiple actual parents silently resolved: %+v", artifacts)
	}
}

func TestPlannerFormatPrecedentPrefersMergedSiblingThenLatestWithStableTies(t *testing.T) {
	commit := strings.Repeat("d", 40)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)
	current := models.Task{ID: "current", Type: models.TaskTypePlanning, RolePair: "custom-plan-pair", ParentTasks: []string{"cohort"}}
	state := &models.State{Tasks: []models.Task{
		current,
		{ID: "latest-b", Type: models.TaskTypePlanning, RolePair: current.RolePair, Status: models.TaskStatusMerged, ReviewCommit: &commit, Output: []models.OutputEntry{{PlanRef: "b.md#Unit 1"}}, History: []models.TaskHistoryEntry{{Event: models.TaskEventMerged, Time: newer}}},
		{ID: "latest-a", Type: models.TaskTypePlanning, RolePair: current.RolePair, Status: models.TaskStatusMerged, ReviewCommit: &commit, Output: []models.OutputEntry{{PlanRef: "a.md#Unit 1"}}, History: []models.TaskHistoryEntry{{Event: models.TaskEventMerged, Time: newer}}},
		{ID: "sibling", Type: models.TaskTypePlanning, RolePair: current.RolePair, ParentTasks: current.ParentTasks, Status: models.TaskStatusMerged, ReviewCommit: &commit, Output: []models.OutputEntry{{PlanRef: "s.md#Unit A"}}, History: []models.TaskHistoryEntry{{Event: models.TaskEventMerged, Time: old}}},
	}}
	precedent := plannerFormatPrecedent(&current, state, models.RoleCodePlanner)
	if precedent == nil || precedent.ProducerID != "sibling" || precedent.Ref != "s.md#Unit A" || precedent.Commit != commit {
		t.Fatalf("merged sibling precedent=%+v", precedent)
	}
	state.Tasks[3].Status = models.TaskStatusImplementing
	precedent = plannerFormatPrecedent(&current, state, models.RoleCodePlanner)
	if precedent == nil || precedent.ProducerID != "latest-a" {
		t.Fatalf("latest/tie precedent=%+v", precedent)
	}
	state.Tasks[2].ReviewCommit = nil
	precedent = plannerFormatPrecedent(&current, state, models.RoleCodePlanner)
	if precedent == nil || precedent.ProducerID != "latest-b" {
		t.Fatalf("unattributed precedent selected: %+v", precedent)
	}
}

func TestPlannerArtifactsRetainDifferentAmbiguousOwnersWithinOneFile(t *testing.T) {
	state := &models.State{Tasks: []models.Task{
		{ID: "root", Type: models.TaskTypeArchitecture},
		{ID: "alpha", Type: models.TaskTypeCoding, ParentTasks: []string{"root", "owner-a", "owner-b"}, PlanRef: "same.md#Unit Alpha"},
		{ID: "beta", Type: models.TaskTypeCoding, ParentTasks: []string{"root", "owner-c", "owner-d"}, PlanRef: "same.md#Unit Beta"},
		{ID: "owner-a", Type: models.TaskTypePlanning, Output: []models.OutputEntry{{PlanRef: "same.md#Unit Alpha"}}},
		{ID: "owner-b", Type: models.TaskTypePlanning, Output: []models.OutputEntry{{PlanRef: "same.md#Unit Alpha"}}},
		{ID: "owner-c", Type: models.TaskTypePlanning, Output: []models.OutputEntry{{PlanRef: "same.md#Unit Beta"}}},
		{ID: "owner-d", Type: models.TaskTypePlanning, Output: []models.OutputEntry{{PlanRef: "same.md#Unit Beta"}}},
	}}
	artifacts, _ := plannerProviderArtifacts(&state.Tasks[0], state)
	if len(artifacts) != 2 || !strings.Contains(artifacts[0].Unresolved, "[owner-a, owner-b]") || !strings.Contains(artifacts[1].Unresolved, "[owner-c, owner-d]") {
		t.Fatalf("grouping dropped a distinct ambiguity: %+v", artifacts)
	}
	if len(artifacts[0].Units) != 1 || artifacts[0].Units[0].ID != "alpha" || len(artifacts[1].Units) != 1 || artifacts[1].Units[0].ID != "beta" {
		t.Fatalf("unit attribution mixed ambiguous owner sets: %+v", artifacts)
	}
}

func TestPlannerContextIsRestrictedToPlannerDoersAndDisclosesMissingTasks(t *testing.T) {
	task := models.Task{ID: "self", Iteration: 2, DependsOn: []string{"missing"}, ArchRef: "arch.md#Scope 2"}
	state := &models.State{Tasks: []models.Task{task}}
	for _, role := range []string{models.RoleCoder, models.RoleCodeReviewer, models.RoleArchitectureReviewer} {
		data := &prompts.RoleContextData{RoleType: "doer", PriorRejection: "unrelated.md:1"}
		if err := populatePlannerContext(&task, state, SupervisorConfig{Role: role, ProjectRoot: "/nonexistent"}, data); err != nil || len(data.PlannerProviders) > 0 || len(data.PlannerReworkUnresolved) > 0 {
			t.Fatalf("role %s gained planner context: data=%+v, err=%v", role, data, err)
		}
	}
	data := &prompts.RoleContextData{RoleType: "reviewer", PriorRejection: "unrelated.md:1"}
	if err := populatePlannerContext(&task, state, SupervisorConfig{Role: models.RoleArchitect, ProjectRoot: "/nonexistent"}, data); err != nil || len(data.PlannerProviders) > 0 {
		t.Fatalf("reviewer gained planner context: %+v, %v", data, err)
	}
	data = &prompts.RoleContextData{RoleType: "doer"}
	if err := populatePlannerContext(&task, state, SupervisorConfig{Role: models.RoleArchitect}, data); err != nil {
		t.Fatal(err)
	}
	if data.ArchSection != "Scope 2" || len(data.PlannerProviders) != 1 || data.PlannerProviders[0].Status != "MISSING" || data.PlannerProviders[0].Unavailable != "task not found" {
		t.Fatalf("missing dependency projection=%+v", data)
	}
}

func TestPlannerReworkUsesLatestRetainedSnapshotAndFenceAwareExactSections(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	path := "specs/rework ' ; marker.md"
	body := "# Plan\n## Scope 2: Exact\nline three\n```markdown\n## Fake\nline six\n```\n## Scope 3\nline nine\n"
	writeReferenceFixture(t, repo, path, body)
	commit := commitReferenceFixture(t, repo, "test: retain rejected planner snapshot")
	// A later mutable worktree edit must never alter cited ranges.
	if err := os.WriteFile(filepath.Join(repo, path), []byte("# Entirely different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldReason := "old.md:1"
	latestReason := "`" + path + ":3` and `" + path + ":6`; `" + path + ":3` again"
	task := models.Task{Iteration: 2, History: []models.TaskHistoryEntry{
		{Event: models.TaskEventRejected, Commit: &commit, Reason: &oldReason},
		{Event: models.TaskEventReviewVerdictRejected, Commit: &commit, Reason: &latestReason},
	}}
	data := &prompts.RoleContextData{PriorRejection: latestReason, Worktree: repo}
	if err := populatePlannerRework(&task, git.New(repo), data); err != nil {
		t.Fatal(err)
	}
	if len(data.PlannerReworkSections) != 1 || len(data.PlannerReworkUnresolved) != 0 {
		t.Fatalf("sections=%+v, unresolved=%v", data.PlannerReworkSections, data.PlannerReworkUnresolved)
	}
	pointer := data.PlannerReworkSections[0]
	if pointer.Heading != "Scope 2: Exact" || pointer.StartLine != 2 || pointer.EndLine != 7 || pointer.Commit != commit || !reflect.DeepEqual(pointer.CitedLines, []int{3, 6}) {
		t.Fatalf("rework pointer=%+v", pointer)
	}
	if data.PriorRejection != latestReason {
		t.Fatal("navigation replaced complete rejection feedback")
	}
	command := "git -C " + data.ShellIntegrationWorktree() + " show " + pointer.ShellSnapshot() + " | sed -n " + pointer.ShellRange()
	output, err := exec.Command("sh", "-c", command).CombinedOutput()
	if err != nil || string(output) != "## Scope 2: Exact\nline three\n```markdown\n## Fake\nline six\n```\n" {
		t.Fatalf("shell-safe cited read: err=%v, output=%q", err, output)
	}
}

func TestPlannerReworkDisclosesMissingCommitFileAndAmbiguousOrOutOfRangeSections(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/ambiguous.md", "# Plan\n## Same\nfirst\n## Same\nsecond\n")
	commit := commitReferenceFixture(t, repo, "test: retain ambiguous planner sections")
	feedback := "specs/ambiguous.md:3 specs/ambiguous.md:99 missing.md:2"
	task := models.Task{Iteration: 2, History: []models.TaskHistoryEntry{{Event: models.TaskEventRejected, Commit: &commit, Reason: &feedback}}}
	data := &prompts.RoleContextData{PriorRejection: feedback}
	if err := populatePlannerRework(&task, git.New(repo), data); err != nil {
		t.Fatal(err)
	}
	if len(data.PlannerReworkSections) != 0 || len(data.PlannerReworkUnresolved) != 3 {
		t.Fatalf("fabricated navigation for unresolved citations: %+v", data)
	}
	missingCommit := strings.Repeat("e", 40)
	task.History[0].Commit = &missingCommit
	data = &prompts.RoleContextData{PriorRejection: feedback}
	if err := populatePlannerRework(&task, git.New(repo), data); err != nil || len(data.PlannerReworkSections) != 0 || len(data.PlannerReworkUnresolved) != 2 || !strings.Contains(strings.Join(data.PlannerReworkUnresolved, "\n"), "revision not found") {
		t.Fatalf("missing retained commit: %+v, err=%v", data, err)
	}
	task.History = append(task.History, models.TaskHistoryEntry{Event: models.TaskEventRejected})
	data = &prompts.RoleContextData{PriorRejection: feedback}
	if err := populatePlannerRework(&task, git.New(repo), data); err != nil || len(data.PlannerReworkSections) != 0 || !reflect.DeepEqual(data.PlannerReworkUnresolved, []string{"latest rejection has no retained commit"}) {
		t.Fatalf("silently reused older rejection: %+v, err=%v", data, err)
	}
	// An unavailable repository is an operational error, not advisory absence.
	task.History = task.History[:1]
	if err := populatePlannerRework(&task, git.New(filepath.Join(repo, "absent")), &prompts.RoleContextData{PriorRejection: feedback}); err == nil || !strings.Contains(err.Error(), "resolve planner rejection citations") {
		t.Fatalf("operational failure suppressed: %v", err)
	}
}
