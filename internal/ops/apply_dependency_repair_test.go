package ops

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	activitylog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestApplyDependencyRepair_MultiTaskAtomicSuccess(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusBlocked, now)
	source.DependsOn = []string{"old-source"}
	source.RepairRequest = dependencyRepairRequest([]models.DependencyUpdate{
		{
			TaskID:            "repair-source",
			ExpectedDependsOn: []string{"old-source"},
			DesiredDependsOn:  []string{"replacement-old"},
		},
		{
			TaskID:            "consumer",
			ExpectedDependsOn: []string{"old-consumer"},
			DesiredDependsOn:  []string{},
		},
	})
	sourceHistoryLen := len(source.History)

	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusReady, now)
	consumer.DependsOn = []string{"old-consumer"}
	consumerHistoryLen := len(consumer.History)

	replacementOld := testhelpers.BuildTaskByStatus("replacement-old", models.TaskStatusSuperseded, now)
	replacementOld.RolePair = "coding-pair"
	replacementOld.SupersededBy = []string{"replacement-new"}
	replacementOld.RescopeReason = testhelpers.StringPtr("Canonical replacement")

	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Tasks = []models.Task{
		source,
		consumer,
		testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("old-consumer", models.TaskStatusMerged, now),
		replacementOld,
		testhelpers.BuildTaskByStatus("replacement-new", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ApplyDependencyRepair(tmpDir, "repair-source", "Apply stored graph repair", "orchestrator-1")
	if err != nil {
		t.Fatalf("ApplyDependencyRepair() error: %v", err)
	}
	if result.SourceTaskID != "repair-source" {
		t.Fatalf("SourceTaskID = %q, want repair-source", result.SourceTaskID)
	}
	wantUpdates := []AppliedDependencyUpdate{
		{TaskID: "repair-source", CanonicalDependencies: []string{"replacement-new"}},
		{TaskID: "consumer", CanonicalDependencies: []string{}},
	}
	if !reflect.DeepEqual(result.Updates, wantUpdates) {
		t.Fatalf("Updates = %#v, want %#v", result.Updates, wantUpdates)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", result.Warnings)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	updatedSource := readState.FindTask("repair-source")
	if updatedSource == nil {
		t.Fatal("repair-source not found")
	}
	if updatedSource.Status != models.TaskStatusBlocked {
		t.Fatalf("source status = %s, want BLOCKED", updatedSource.Status)
	}
	if updatedSource.RepairRequest != nil {
		t.Fatal("source repair request was not cleared")
	}
	if !slices.Equal(updatedSource.DependsOn, []string{"replacement-new"}) {
		t.Fatalf("source depends_on = %v, want [replacement-new]", updatedSource.DependsOn)
	}
	assertAppliedDependencyHistory(t, updatedSource, sourceHistoryLen, "repair-source", []any{"old-source"}, []any{"replacement-old"}, []any{"replacement-new"})

	updatedConsumer := readState.FindTask("consumer")
	if updatedConsumer == nil {
		t.Fatal("consumer not found")
	}
	if len(updatedConsumer.DependsOn) != 0 {
		t.Fatalf("consumer depends_on = %v, want empty", updatedConsumer.DependsOn)
	}
	assertAppliedDependencyHistory(t, updatedConsumer, consumerHistoryLen, "repair-source", []any{"old-consumer"}, []any{}, []any{})

	entries, err := activitylog.New(paths.New(tmpDir).LogPath()).Read()
	if err != nil {
		t.Fatalf("read activity log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("activity log entries = %d, want 1", len(entries))
	}
	if entries[0].Action != models.RepairOperationApplyDependencyRepair || entries[0].Agent != "orchestrator-1" {
		t.Fatalf("activity log action/agent = %q/%q", entries[0].Action, entries[0].Agent)
	}
}

func TestApplyDependencyRepair_PreservesSourceValidationReceipt(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusBlocked, now)
	source.DependsOn = []string{"old-source"}
	source.RepairRequest = dependencyRepairRequest([]models.DependencyUpdate{
		{
			TaskID:            "consumer",
			ExpectedDependsOn: []string{"old-consumer"},
			DesiredDependsOn:  []string{"new-consumer"},
		},
	})
	sourceHistoryLen := len(source.History)

	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusReady, now)
	consumer.DependsOn = []string{"old-consumer"}

	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Tasks = []models.Task{
		source,
		consumer,
		testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("old-consumer", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("new-consumer", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ApplyDependencyRepair(tmpDir, "repair-source", "Apply stored graph repair", "orchestrator-1")
	if err != nil {
		t.Fatalf("ApplyDependencyRepair() error: %v", err)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	updatedSource := readState.FindTask("repair-source")
	if updatedSource == nil {
		t.Fatal("repair-source not found")
	}
	if updatedSource.RepairRequest != nil {
		t.Fatal("source repair request was not cleared")
	}
	if !slices.Equal(updatedSource.DependsOn, []string{"old-source"}) {
		t.Fatalf("source depends_on = %v, want unchanged [old-source]", updatedSource.DependsOn)
	}
	if len(updatedSource.History) != sourceHistoryLen+1 {
		t.Fatalf("source history length = %d, want %d", len(updatedSource.History), sourceHistoryLen+1)
	}
	receipt := updatedSource.History[len(updatedSource.History)-1]
	if receipt.Event != "dependency_repair_applied" {
		t.Fatalf("source receipt event = %q, want dependency_repair_applied", receipt.Event)
	}
	if !reflect.DeepEqual(receipt.Extra["affected_task_ids"], []any{"consumer"}) {
		t.Fatalf("affected_task_ids = %#v, want [consumer]", receipt.Extra["affected_task_ids"])
	}
	if !reflect.DeepEqual(receipt.Extra["repair_validation"], []any{"validate repaired dependency graph"}) {
		t.Fatalf("repair_validation = %#v, want declared validation", receipt.Extra["repair_validation"])
	}
	if receipt.Extra["repair_request_cleared"] != true {
		t.Fatalf("repair_request_cleared = %#v, want true", receipt.Extra["repair_request_cleared"])
	}
}

func TestApplyDependencyRepair_StaleOrInvalidBatchRollsBack(t *testing.T) {
	t.Parallel()

	assertDependencyRepairBatchRollsBack(t, "stale expectation", models.TaskStatusReady, []models.DependencyUpdate{
		{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"new-source"}},
		{TaskID: "consumer", ExpectedDependsOn: []string{"stale-consumer"}, DesiredDependsOn: []string{"new-consumer"}},
	}, "consumer dependencies changed since the repair request was created")

	assertDependencyRepairBatchRollsBack(t, "invalid dependency", models.TaskStatusReady, []models.DependencyUpdate{
		{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"new-source"}},
		{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"missing-dependency"}},
	}, `desired dependency "missing-dependency" for task consumer does not exist`)

	assertDependencyRepairBatchRollsBack(t, "cyclic candidate", models.TaskStatusReady, []models.DependencyUpdate{
		{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"consumer"}},
		{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"repair-source"}},
	}, "circular dependency detected")

	assertDependencyRepairBatchRollsBack(t, "terminal affected task", models.TaskStatusMerged, []models.DependencyUpdate{
		{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"new-source"}},
		{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"new-consumer"}},
	}, "cannot apply dependency repair to terminal task consumer")
}

func assertDependencyRepairBatchRollsBack(t *testing.T, caseName string, consumerStatus models.TaskStatus, updates []models.DependencyUpdate, wantError string) {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusBlocked, now)
	source.DependsOn = []string{"old-source"}
	source.RepairRequest = dependencyRepairRequest(updates)
	consumer := testhelpers.BuildTaskByStatus("consumer", consumerStatus, now)
	consumer.DependsOn = []string{"old-consumer"}

	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Tasks = []models.Task{
		source,
		consumer,
		testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("old-consumer", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("new-source", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("new-consumer", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	before, err := bb.Read()
	if err != nil {
		t.Fatalf("%s: read initial state: %v", caseName, err)
	}

	_, err = ApplyDependencyRepair(tmpDir, "repair-source", "Apply stored graph repair", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, wantError)

	after, err := bb.Read()
	if err != nil {
		t.Fatalf("%s: read state after rejected repair: %v", caseName, err)
	}
	if !reflect.DeepEqual(after.Tasks, before.Tasks) {
		t.Fatalf("%s: tasks changed after rejected repair\nbefore: %#v\nafter:  %#v", caseName, before.Tasks, after.Tasks)
	}
	unchangedSource := after.FindTask("repair-source")
	if unchangedSource == nil || unchangedSource.Status != models.TaskStatusBlocked || unchangedSource.RepairRequest == nil {
		t.Fatalf("%s: source task changed after rejected repair: %#v", caseName, unchangedSource)
	}
}

func dependencyRepairRequest(updates []models.DependencyUpdate) *models.RepairRequest {
	return &models.RepairRequest{
		Operation:         models.RepairOperationApplyDependencyRepair,
		Target:            "repair-source",
		DependencyUpdates: updates,
		Evidence:          []string{"command=blocked-operation exit_code=1 stderr=orchestrator repair required"},
		Validation:        []string{"validate repaired dependency graph"},
	}
}

func assertAppliedDependencyHistory(t *testing.T, task *models.Task, initialLen int, sourceTaskID string, expected, desired, canonical []any) {
	t.Helper()
	if len(task.History) != initialLen+1 {
		t.Fatalf("%s history length = %d, want %d", task.ID, len(task.History), initialLen+1)
	}
	last := task.History[len(task.History)-1]
	if last.Event != models.TaskEventDependenciesRewritten {
		t.Fatalf("%s history event = %q, want %q", task.ID, last.Event, models.TaskEventDependenciesRewritten)
	}
	if last.Agent == nil || *last.Agent != "orchestrator-1" {
		t.Fatalf("%s history agent = %v, want orchestrator-1", task.ID, last.Agent)
	}
	if last.Reason == nil || *last.Reason != "Apply stored graph repair" {
		t.Fatalf("%s history reason = %v, want repair reason", task.ID, last.Reason)
	}
	if last.Extra["operation"] != models.RepairOperationApplyDependencyRepair {
		t.Fatalf("%s history operation = %v", task.ID, last.Extra["operation"])
	}
	if last.Extra["repair_source_task"] != sourceTaskID {
		t.Fatalf("%s repair_source_task = %v, want %s", task.ID, last.Extra["repair_source_task"], sourceTaskID)
	}
	if !reflect.DeepEqual(last.Extra["expected_dependencies"], expected) {
		t.Fatalf("%s expected_dependencies = %#v, want %#v", task.ID, last.Extra["expected_dependencies"], expected)
	}
	if !reflect.DeepEqual(last.Extra["desired_dependencies"], desired) {
		t.Fatalf("%s desired_dependencies = %#v, want %#v", task.ID, last.Extra["desired_dependencies"], desired)
	}
	if !reflect.DeepEqual(last.Extra["canonical_dependencies"], canonical) {
		t.Fatalf("%s canonical_dependencies = %#v, want %#v", task.ID, last.Extra["canonical_dependencies"], canonical)
	}
}

func TestInTransactionCores_ApplyDependencyUpdatesInStateMatchesCommand(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	seeded := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updates := []models.DependencyUpdate{
		{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"replacement-new"}},
		{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{}},
	}
	fixture := func() *models.State {
		source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusBlocked, seeded)
		source.DependsOn = []string{"old-source"}
		source.RepairRequest = dependencyRepairRequest(updates)
		consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusReady, seeded)
		consumer.DependsOn = []string{"old-consumer"}
		state := testhelpers.CreateValidState()
		state.Goal.SpecRef = "README.md"
		state.Tasks = []models.Task{
			source,
			consumer,
			testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, seeded),
			testhelpers.BuildTaskByStatus("old-consumer", models.TaskStatusMerged, seeded),
			testhelpers.BuildTaskByStatus("replacement-new", models.TaskStatusMerged, seeded),
		}
		return state
	}

	testhelpers.WriteInitialState(t, stateFile, fixture())
	if _, err := ApplyDependencyRepair(tmpDir, "repair-source", "Apply stored graph repair", "orchestrator-1"); err != nil {
		t.Fatalf("ApplyDependencyRepair() error: %v", err)
	}
	commandState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver() error: %v", err)
	}
	coreState := fixture()
	source := coreState.FindTask("repair-source")
	request, err := normalizeRepairRequest(source.RepairRequest, "repair-source")
	if err != nil {
		t.Fatalf("normalizeRepairRequest() error: %v", err)
	}
	consumed, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal repair request: %v", err)
	}
	applied, err := applyDependencyUpdatesInState(coreState, resolver, source, models.RepairOperationApplyDependencyRepair,
		request.DependencyUpdates, "orchestrator-1", "Apply stored graph repair", seeded, map[string]any{
			"manual":                 true,
			"operation":              models.RepairOperationApplyDependencyRepair,
			"repair_source_task":     "repair-source",
			"repair_evidence":        append([]string{}, request.Evidence...),
			"repair_validation":      append([]string{}, request.Validation...),
			"repair_request_cleared": true,
			"repair_request_digest":  lifecycleDigest(consumed),
		})
	if err != nil {
		t.Fatalf("applyDependencyUpdatesInState() error: %v", err)
	}
	// Clearing the consumed request is the caller's residual step, not the core's.
	source.RepairRequest = nil

	wantApplied := []AppliedDependencyUpdate{
		{TaskID: "repair-source", CanonicalDependencies: []string{"replacement-new"}},
		{TaskID: "consumer", CanonicalDependencies: []string{}},
	}
	if !reflect.DeepEqual(applied, wantApplied) {
		t.Fatalf("applied = %#v, want %#v", applied, wantApplied)
	}
	// Pinned independently of the command, so a core-only regression cannot hide
	// behind the differential comparison below.
	if !slices.Equal(source.DependsOn, []string{"replacement-new"}) {
		t.Fatalf("core source depends_on = %v, want [replacement-new]", source.DependsOn)
	}
	consumer := coreState.FindTask("consumer")
	if !slices.Equal(consumer.DependsOn, []string{}) {
		t.Fatalf("core consumer depends_on = %v, want empty", consumer.DependsOn)
	}
	audit := consumer.History[len(consumer.History)-1]
	if audit.Event != models.TaskEventDependenciesRewritten {
		t.Fatalf("core audit event = %q, want %q", audit.Event, models.TaskEventDependenciesRewritten)
	}
	auditKeys := make([]string, 0, len(audit.Extra))
	for key := range audit.Extra {
		auditKeys = append(auditKeys, key)
	}
	sort.Strings(auditKeys)
	wantKeys := []string{
		"affected_task_ids", "canonical_dependencies", "desired_dependencies", "expected_dependencies",
		"manual", "operation", "repair_evidence", "repair_request_cleared", "repair_request_digest",
		"repair_source_task", "repair_validation",
	}
	if !slices.Equal(auditKeys, wantKeys) {
		t.Fatalf("core audit keys = %v, want %v", auditKeys, wantKeys)
	}
	if !reflect.DeepEqual(audit.Extra["affected_task_ids"], []string{"repair-source", "consumer"}) {
		t.Fatalf("core affected_task_ids = %#v, want [repair-source consumer]", audit.Extra["affected_task_ids"])
	}

	for _, taskID := range []string{"repair-source", "consumer", "old-source", "old-consumer", "replacement-new"} {
		assertCoreMatchesCommand(t, coreState, commandState, taskID)
	}
}

func TestConsumeRepairRequest_RefusesOtherOperationAndBindsDigest(t *testing.T) {
	t.Parallel()

	source := &models.Task{ID: "source", RepairRequest: &models.RepairRequest{
		Operation: models.RepairOperationApplyDependencyRepair,
		Target:    "source",
		DependencyUpdates: []models.DependencyUpdate{
			{TaskID: "consumer", ExpectedDependsOn: []string{"old"}, DesiredDependsOn: []string{"new"}},
		},
		Evidence:   []string{"error=dependency selection no longer matches scope"},
		Validation: []string{"validate --json"},
	}}
	first, err := consumeRepairRequest(source)
	if err != nil {
		t.Fatalf("consumeRepairRequest: %v", err)
	}
	second, err := consumeRepairRequest(source)
	if err != nil || first.digest == "" || first.digest != second.digest {
		t.Fatalf("digest not stable: %q vs %q (%v)", first.digest, second.digest, err)
	}
	source.RepairRequest.DependencyUpdates[0].DesiredDependsOn = []string{"other"}
	changed, err := consumeRepairRequest(source)
	if err != nil || changed.digest == first.digest {
		t.Fatalf("digest ignores the consumed lists: %q (%v)", changed.digest, err)
	}

	other := &models.Task{ID: "source", RepairRequest: &models.RepairRequest{
		Operation:  "retarget-dependency",
		Target:     "source",
		Command:    "liza retarget-dependency source old new --reason x",
		Evidence:   []string{"error=stale edge"},
		Validation: []string{"validate --json"},
	}}
	_, err = consumeRepairRequest(other)
	var precondition *PreconditionError
	if !errors.As(err, &precondition) || !strings.Contains(precondition.Reason, `repair request operation is "retarget-dependency", want "apply-dependency-repair"`) {
		t.Fatalf("want operation mismatch precondition, got %v", err)
	}
}

func TestConsumedRepairAuditSource_OnlyWhenSourceListUnchanged(t *testing.T) {
	t.Parallel()

	consumed := consumedRepair{
		request: &models.RepairRequest{Evidence: []string{"evidence"}, Validation: []string{"validate --json"}},
		digest:  "digest-1",
	}
	now := time.Date(2026, 10, 25, 1, 2, 3, 0, time.UTC)
	updates := []AppliedDependencyUpdate{{TaskID: "a"}, {TaskID: "b"}}

	rewritten := &models.Task{ID: "a"}
	consumed.auditSource(rewritten, updates, "orchestrator-1", "why", now)
	if len(rewritten.History) != 0 {
		t.Fatalf("rewritten source must not gain a second entry: %+v", rewritten.History)
	}

	untouched := &models.Task{ID: "src"}
	consumed.auditSource(untouched, updates, "orchestrator-1", "why", now)
	if len(untouched.History) != 1 {
		t.Fatalf("want one source entry, got %+v", untouched.History)
	}
	entry := untouched.History[0]
	if entry.Event != models.TaskEventDependencyRepairApplied || !entry.Time.Equal(now) {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if got := entry.Extra["affected_task_ids"]; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("affected_task_ids = %v", got)
	}
	if entry.Extra["repair_request_digest"] != "digest-1" || entry.Extra["repair_request_cleared"] != true || entry.Extra["manual"] != true {
		t.Fatalf("audit keys = %v", entry.Extra)
	}
	if _, ok := entry.Extra["repair_source_task"]; ok {
		t.Fatalf("source entry must not carry repair_source_task: %v", entry.Extra)
	}
}

// writeApplyRepairState stores a source task in the given status holding the
// request, beside the consumer and the merged lists the request names.
func writeApplyRepairState(t *testing.T, status models.TaskStatus, request *models.RepairRequest) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()
	source := testhelpers.BuildTaskByStatus("repair-source", status, now)
	source.DependsOn = []string{"old-source"}
	source.RepairRequest = request
	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusBlocked, now)
	consumer.DependsOn = []string{"old-consumer"}
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Tasks = []models.Task{
		source,
		consumer,
		testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("old-consumer", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("new-consumer", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	return tmpDir, stateFile
}

func TestApplyDependencyRepair_LifecycleDecisions(t *testing.T) {
	t.Parallel()
	valid := []models.DependencyUpdate{{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"new-consumer"}}}
	for _, tc := range []struct {
		name       string
		status     models.TaskStatus
		request    *models.RepairRequest
		wantOutput string
		wantAction string
		wantErr    string
	}{
		{"source not blocked", models.TaskStatusReady, dependencyRepairRequest(valid), models.LifecycleAlreadyTransitioned, "stop", "must be BLOCKED"},
		{"no repair request", models.TaskStatusBlocked, nil, models.LifecycleStateChanged, "requery", "has no repair request"},
		{"stale expected list", models.TaskStatusBlocked, dependencyRepairRequest([]models.DependencyUpdate{{TaskID: "consumer", ExpectedDependsOn: []string{"stale"}, DesiredDependsOn: []string{"new-consumer"}}}), models.LifecycleStateChanged, "requery", "dependencies changed since the repair request was created"},
		{"unknown desired dependency", models.TaskStatusBlocked, dependencyRepairRequest([]models.DependencyUpdate{{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"nowhere"}}}), models.LifecycleInvalidInput, "correct_input", `desired dependency "nowhere"`},
		{"cyclic candidate", models.TaskStatusBlocked, dependencyRepairRequest([]models.DependencyUpdate{
			{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"consumer"}},
			{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"repair-source"}},
		}), models.LifecycleInvalidInput, "correct_input", "circular dependency"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmpDir, _ := writeApplyRepairState(t, tc.status, tc.request)
			_, err := ApplyDependencyRepair(tmpDir, "repair-source", "Apply stored graph repair", "orchestrator-1")
			testhelpers.RequireErrorContains(t, err, tc.wantErr)
			requireLifecycleError(t, err, tc.wantOutput, tc.wantAction, string(LifecycleEffectsNone))
		})
	}
}

// An identified request completes once; its exact repeat returns the original
// completion without a second history entry or a second rewrite.
func TestApplyDependencyRepair_IdentifiedRequestReplaysOriginalCompletion(t *testing.T) {
	t.Parallel()
	valid := []models.DependencyUpdate{{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{"new-consumer"}}}
	tmpDir, stateFile := writeApplyRepairState(t, models.TaskStatusBlocked, dependencyRepairRequest(valid))
	stored, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	opts := LifecycleRequestOptions{RequestID: "repair-replay", ExpectedTransition: models.TaskTransitionID(stored.FindTask("repair-source"))}

	first, err := ApplyDependencyRepairWithOptions(tmpDir, "repair-source", "Apply stored graph repair", "orchestrator-1", opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != models.LifecycleCompleted || first.SafeAction != string(LifecycleSafeActionContinue) || first.Effects != string(LifecycleEffectsCommitted) || first.Changed == nil || !*first.Changed {
		t.Fatalf("first outcome = %+v, want committed COMPLETED/continue", first.LifecycleOutcome)
	}
	committed := readDependencyUpdateState(t, stateFile)
	historyLen := len(committed.FindTask("consumer").History)

	replay, err := ApplyDependencyRepairWithOptions(tmpDir, "repair-source", "Apply stored graph repair", "orchestrator-1", opts)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Outcome != models.LifecycleAlreadyCompleted || replay.Changed == nil || *replay.Changed {
		t.Fatalf("replay outcome = %+v, want ALREADY_COMPLETED changed=false", replay.LifecycleOutcome)
	}
	if got := len(readDependencyUpdateState(t, stateFile).FindTask("consumer").History); got != historyLen {
		t.Fatalf("replay appended history: %d, want %d", got, historyLen)
	}
}
