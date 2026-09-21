package usage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// Fixture helpers carry a tr prefix: package usage holds several test files
// and their package-level identifiers share one namespace.
var (
	trT0 = time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	trT1 = trT0.Add(1 * time.Hour)
	trT2 = trT0.Add(2 * time.Hour)
	trT3 = trT0.Add(3 * time.Hour)
	trT4 = trT0.Add(4 * time.Hour)
)

func trEntry(at time.Time, event string) models.TaskHistoryEntry {
	return models.TaskHistoryEntry{Time: at, Event: event}
}

func trTaskWith(id string, revision uint64, entries ...models.TaskHistoryEntry) *models.Task {
	return &models.Task{
		ID:        id,
		Lifecycle: &models.TaskLifecycle{Revision: revision},
		History:   entries,
	}
}

func TestUsefulTransitionRule(t *testing.T) {
	useful := []string{
		models.TaskEventCreated, models.TaskEventClaimed,
		models.TaskEventPreExecutionCheckpoint, models.TaskEventOutputSet,
		models.TaskEventSubmittedForReview, models.TaskEventReviewCommitUpdated,
		models.TaskEventApproved, models.TaskEventRejected,
		models.TaskEventReviewVerdictApproved, models.TaskEventReviewVerdictRejected,
		models.TaskEventMerged, models.TaskEventSuperseded, models.TaskEventAbandoned,
		models.TaskEventBlocked, models.TaskEventUnblocked,
		models.TaskEventIntegrationFailed, models.TaskEventTransitionExecuted,
		models.TaskEventDependenciesRewritten, models.TaskEventDependencyRepairApplied,
		models.TaskEventReplacementCommitted,
		models.TaskEventRejectionRCARecorded, models.TaskEventRejectionRCAResumed,
		models.TaskEventHandoffInitiated,
	}
	notUseful := []string{
		models.TaskEventOrchestratorAssessment, models.TaskEventClaimReleased,
		models.TaskEventDoerClaimReleased, models.TaskEventReviewClaimReleased,
		models.TaskEventReclaimedAfterRejection, models.TaskEventReassignedAfterRejection,
		models.TaskEventNewAttempt, models.TaskEventOwnedTaskResumed,
		models.TaskEventHandoffResumed, models.TaskEventWorktreeRecovered,
		models.TaskEventClaimedForIntegrationFix, models.TaskEventTransitionCycleBlocked,
		models.TaskEventTransitionCrashRecov, models.TaskEventPlanning,
		models.TaskEventInitialization, models.TaskEventReplanned,
		models.TaskEventAcceptanceCommitsRemapped,
	}
	if len(useful) != 23 || len(notUseful) != 17 {
		t.Fatalf("documented lists have %d useful and %d not-useful names; want 23 and 17", len(useful), len(notUseful))
	}

	t.Run("documented lists classify as documented", func(t *testing.T) {
		for _, name := range useful {
			gotUseful, classified := IsUsefulEvent(name)
			if !gotUseful || !classified {
				t.Errorf("IsUsefulEvent(%q) = (%v, %v); want (true, true)", name, gotUseful, classified)
			}
		}
		for _, name := range notUseful {
			gotUseful, classified := IsUsefulEvent(name)
			if gotUseful || !classified {
				t.Errorf("IsUsefulEvent(%q) = (%v, %v); want (false, true)", name, gotUseful, classified)
			}
		}
	})

	t.Run("unknown event is unclassified and not useful", func(t *testing.T) {
		for _, name := range []string{"invented_event", ""} {
			gotUseful, classified := IsUsefulEvent(name)
			if gotUseful || classified {
				t.Errorf("IsUsefulEvent(%q) = (%v, %v); want (false, false)", name, gotUseful, classified)
			}
		}
		tasks := []models.Task{
			*trTaskWith("a", 1, trEntry(trT0, models.TaskEventCreated), trEntry(trT1, "invented_event")),
			*trTaskWith("b", 1, trEntry(trT0, "zeta_event"), trEntry(trT1, "invented_event"), trEntry(trT2, models.TaskEventMerged)),
		}
		got := UnclassifiedEvents(tasks)
		want := []string{"invented_event", "zeta_event"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("UnclassifiedEvents = %v; want %v (sorted, deduplicated)", got, want)
		}
		if got := UnclassifiedEvents(nil); len(got) != 0 {
			t.Errorf("UnclassifiedEvents(nil) = %v; want empty", got)
		}
	})

	t.Run("latest useful event wins over later bookkeeping", func(t *testing.T) {
		task := trTaskWith("task", 7,
			trEntry(trT0, models.TaskEventCreated),
			trEntry(trT1, models.TaskEventClaimed),
			trEntry(trT2, models.TaskEventOrchestratorAssessment),
			trEntry(trT3, models.TaskEventNewAttempt),
			trEntry(trT4, "invented_event"),
		)
		got, ok := LastUsefulTransition(task, nil, time.Time{})
		want := Transition{Evidence: Evidence{Event: models.TaskEventClaimed, Time: trT1, Revision: 7}, TaskID: "task"}
		if !ok || got != want {
			t.Errorf("LastUsefulTransition = (%+v, %v); want (%+v, true)", got, ok, want)
		}
	})

	t.Run("as_of excludes later useful events", func(t *testing.T) {
		task := trTaskWith("task", 3,
			trEntry(trT0, models.TaskEventCreated),
			trEntry(trT2, models.TaskEventClaimed),
		)
		got, ok := LastUsefulTransition(task, nil, trT1)
		if !ok || got.Event != models.TaskEventCreated || !got.Time.Equal(trT0) {
			t.Errorf("LastUsefulTransition(as_of=trT1) = (%+v, %v); want created@trT0", got, ok)
		}
		if _, ok := LastUsefulTransition(trTaskWith("only-polling", 1, trEntry(trT0, models.TaskEventOrchestratorAssessment)), nil, time.Time{}); ok {
			t.Error("LastUsefulTransition with only polling entries reported a transition")
		}
		if _, ok := LastUsefulTransition(nil, nil, time.Time{}); ok {
			t.Error("LastUsefulTransition(nil task) reported a transition")
		}
	})

	t.Run("dependency reaching MERGED counts for the dependent", func(t *testing.T) {
		dependent := trTaskWith("dependent", 2,
			trEntry(trT0, models.TaskEventCreated),
			trEntry(trT2, models.TaskEventOrchestratorAssessment),
			trEntry(trT4, models.TaskEventOrchestratorAssessment),
		)
		dependent.DependsOn = []string{"dep"}
		dep := trTaskWith("dep", 9,
			trEntry(trT0, models.TaskEventCreated),
			trEntry(trT1, models.TaskEventSubmittedForReview),
			trEntry(trT3, models.TaskEventMerged),
		)
		got, ok := LastUsefulTransition(dependent, []*models.Task{dep}, time.Time{})
		want := Transition{Evidence: Evidence{Event: models.TaskEventMerged, Time: trT3, Revision: 9}, TaskID: "dep"}
		if !ok || got != want {
			t.Errorf("LastUsefulTransition with merged dependency = (%+v, %v); want (%+v, true)", got, ok, want)
		}

		// Only the dependency's merged event counts, not its other useful events.
		got, ok = LastUsefulTransition(dependent, []*models.Task{dep}, trT2)
		if !ok || got.Event != models.TaskEventCreated || got.TaskID != "dependent" {
			t.Errorf("LastUsefulTransition(as_of=trT2) = (%+v, %v); want dependent's created@trT0", got, ok)
		}
	})
}

func TestDeclaredTaskEventsClassified(t *testing.T) {
	// Derive coverage from declarations, independently of the classification
	// maps and the per-name semantic assertions above, so omissions fail here.
	file, err := parser.ParseFile(token.NewFileSet(), "../models/history.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, decl := range file.Decls {
		constants, ok := decl.(*ast.GenDecl)
		if !ok || constants.Tok != token.CONST {
			continue
		}
		for _, spec := range constants.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "TaskEvent") {
					continue
				}
				// Fail explicitly if declarations change shape; never skip an
				// event merely because this guard cannot read its value.
				if i >= len(value.Values) {
					t.Fatalf("%s: expected explicit string value", name.Name)
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s: expected string literal", name.Name)
				}
				event, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("%s: %v", name.Name, err)
				}
				count++
				_, useful := usefulEvents[event]
				_, notUseful := notUsefulEvents[event]
				if useful == notUseful {
					t.Errorf("%s (%q): must be in exactly one classification map; useful=%v notUseful=%v", name.Name, event, useful, notUseful)
				}
			}
		}
	}
	if count == 0 {
		t.Fatal("no task-event constants found in history.go")
	}
}

func TestAcceptanceCommitsRemappedReportClassification(t *testing.T) {
	for _, tc := range []struct {
		event       string
		wantWarning bool
	}{
		{models.TaskEventAcceptanceCommitsRemapped, false},
		{"invented_event", true},
	} {
		t.Run(tc.event, func(t *testing.T) {
			task := rpTask("parent", rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventMerged), rpEv(2, tc.event))
			records := []Record{rpRec(task.ID, "coder", 3, 10, 20, 5)}
			report := rpBuild(t, Input{
				Records: records, LoadStats: rpAvailable(records),
				State:  &models.State{Tasks: []models.Task{task}},
				Window: Window{Since: rpT0, Until: rpAt(24)},
			})
			if got := rpHasWarning(report, "unclassified task events:"); got != tc.wantWarning {
				t.Errorf("unclassified warning = %v; want %v; warnings: %v", got, tc.wantWarning, report.Warnings)
			}
			if row := rpRow(t, report, OutcomeMerged, "coder"); row.Tasks != 1 || row.Records != 1 {
				t.Errorf("merged row = %+v; want one referenced task and record", row)
			}
			if transition, ok := LastUsefulTransition(&task, nil, time.Time{}); !ok || transition.Event != models.TaskEventMerged {
				t.Errorf("last useful transition = (%+v, %v); want merged", transition, ok)
			}
		})
	}
}

func TestOutcomeClassification(t *testing.T) {
	inFlight := trTaskWith("active", 4,
		trEntry(trT0, models.TaskEventCreated),
		trEntry(trT1, models.TaskEventClaimed),
		trEntry(trT2, models.TaskEventBlocked),
		trEntry(trT3, models.TaskEventUnblocked),
	)
	inFlight.Status = models.TaskStatusImplementing

	cases := []struct {
		name     string
		task     *models.Task
		asOf     time.Time
		want     OutcomeClass
		evidence Evidence
	}{
		{
			name: "merged",
			task: trWithStatus(trTaskWith("m", 5, trEntry(trT0, models.TaskEventCreated), trEntry(trT3, models.TaskEventMerged)), models.TaskStatusMerged),
			want: OutcomeMerged, evidence: Evidence{Event: models.TaskEventMerged, Time: trT3, Revision: 5},
		},
		{
			name: "superseded",
			task: trWithStatus(trTaskWith("s", 6, trEntry(trT0, models.TaskEventCreated), trEntry(trT2, models.TaskEventSuperseded)), models.TaskStatusSuperseded),
			want: OutcomeSuperseded, evidence: Evidence{Event: models.TaskEventSuperseded, Time: trT2, Revision: 6},
		},
		{
			name: "abandoned",
			task: trWithStatus(trTaskWith("a", 2, trEntry(trT0, models.TaskEventCreated), trEntry(trT1, models.TaskEventAbandoned)), models.TaskStatusAbandoned),
			want: OutcomeAbandoned, evidence: Evidence{Event: models.TaskEventAbandoned, Time: trT1, Revision: 2},
		},
		{
			name: "blocked",
			task: trWithStatus(trTaskWith("b", 3, trEntry(trT0, models.TaskEventCreated), trEntry(trT2, models.TaskEventBlocked)), models.TaskStatusBlocked),
			want: OutcomeBlocked, evidence: Evidence{Event: models.TaskEventBlocked, Time: trT2, Revision: 3},
		},
		{
			name: "active after unblocked",
			task: inFlight,
			want: OutcomeActive, evidence: Evidence{Event: models.TaskEventUnblocked, Time: trT3, Revision: 4},
		},
		{
			name: "unattributed for nil task",
			task: nil,
			want: OutcomeUnattributed,
		},
		{
			name: "unattributed for empty task id",
			task: trTaskWith("", 1, trEntry(trT0, models.TaskEventMerged)),
			want: OutcomeUnattributed,
		},
		{
			name: "as_of before merged reproduces blocked",
			task: trWithStatus(trTaskWith("later", 8,
				trEntry(trT0, models.TaskEventCreated),
				trEntry(trT1, models.TaskEventBlocked),
				trEntry(trT3, models.TaskEventUnblocked),
				trEntry(trT4, models.TaskEventMerged),
			), models.TaskStatusMerged),
			asOf: trT2,
			want: OutcomeBlocked, evidence: Evidence{Event: models.TaskEventBlocked, Time: trT1, Revision: 8},
		},
		{
			name: "as_of before any deciding event is active with latest evidence",
			task: trWithStatus(trTaskWith("early", 8, trEntry(trT0, models.TaskEventCreated), trEntry(trT4, models.TaskEventMerged)), models.TaskStatusMerged),
			asOf: trT1,
			want: OutcomeActive, evidence: Evidence{Event: models.TaskEventCreated, Time: trT0, Revision: 8},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, evidence := ClassifyOutcome(tc.task, tc.asOf)
			if got != tc.want {
				t.Errorf("ClassifyOutcome = %q; want %q", got, tc.want)
			}
			if evidence != tc.evidence {
				t.Errorf("evidence = %+v; want %+v", evidence, tc.evidence)
			}
		})
	}

	t.Run("same history yields both readings", func(t *testing.T) {
		task := trWithStatus(trTaskWith("recon", 8,
			trEntry(trT0, models.TaskEventCreated),
			trEntry(trT1, models.TaskEventBlocked),
			trEntry(trT4, models.TaskEventMerged),
		), models.TaskStatusMerged)
		earlier, _ := ClassifyOutcome(task, trT2)
		later, _ := ClassifyOutcome(task, time.Time{})
		if earlier != OutcomeBlocked || later != OutcomeMerged {
			t.Errorf("as_of readings = (%q, %q); want (blocked, merged)", earlier, later)
		}
	})
}

func trWithStatus(task *models.Task, status models.TaskStatus) *models.Task {
	task.Status = status
	return task
}

func TestFailureCategory(t *testing.T) {
	since := trT2
	base := func(extra ...models.TaskHistoryEntry) *models.Task {
		entries := []models.TaskHistoryEntry{
			trEntry(trT0, models.TaskEventCreated),
			trEntry(trT1, models.TaskEventNewAttempt), // before the point: must not count
			trEntry(trT2, models.TaskEventSubmittedForReview),
		}
		return trTaskWith("task", 3, append(entries, extra...)...)
	}
	circuit := []models.Anomaly{{Timestamp: trT3, Task: "task", Type: models.AnomalyTypeReviewerClaimCircuitOpen}}
	otherTaskCircuit := []models.Anomaly{{Timestamp: trT3, Task: "other", Type: models.AnomalyTypeReviewerClaimCircuitOpen}}
	earlyCircuit := []models.Anomaly{{Timestamp: trT1, Task: "task", Type: models.AnomalyTypeReviewerClaimCircuitOpen}}

	cases := []struct {
		name          string
		task          *models.Task
		anomalies     []models.Anomaly
		authoritative bool
		want          FailureCategory
	}{
		{
			name:      "rejection_rca_recorded wins first",
			task:      base(trEntry(trT3, models.TaskEventRejectionRCARecorded), trEntry(trT4, models.TaskEventOrchestratorAssessment), trEntry(trT4, models.TaskEventNewAttempt)),
			anomalies: circuit, authoritative: true, want: FailureRejectionRCA,
		},
		{
			name:          "rejection_rca at the point itself counts",
			task:          trTaskWith("task", 3, trEntry(trT0, models.TaskEventCreated), trEntry(trT2, models.TaskEventRejectionRCARecorded)),
			authoritative: true, want: FailureRejectionRCA,
		},
		{
			name:      "circuit_open anomaly naming the task",
			task:      base(trEntry(trT3, models.TaskEventOrchestratorAssessment), trEntry(trT4, models.TaskEventNewAttempt)),
			anomalies: circuit, authoritative: true, want: FailureCircuitOpen,
		},
		{
			name:      "circuit_open anomaly on another task is ignored",
			task:      base(trEntry(trT3, models.TaskEventOrchestratorAssessment)),
			anomalies: otherTaskCircuit, authoritative: true, want: FailureDuplicateAssessment,
		},
		{
			name: "circuit_open anomaly before the point is ignored",
			task: base(), anomalies: earlyCircuit, authoritative: true, want: FailureRetryTail,
		},
		{
			name:          "duplicate_assessment from post-transition assessments",
			task:          base(trEntry(trT3, models.TaskEventOrchestratorAssessment), trEntry(trT4, models.TaskEventNewAttempt)),
			authoritative: true, want: FailureDuplicateAssessment,
		},
		{
			name:          "lifecycle_retry from new_attempt",
			task:          base(trEntry(trT3, models.TaskEventNewAttempt)),
			authoritative: true, want: FailureLifecycleRetry,
		},
		{
			name:          "lifecycle_retry from claim release",
			task:          base(trEntry(trT3, models.TaskEventReviewClaimReleased)),
			authoritative: true, want: FailureLifecycleRetry,
		},
		{
			name:          "lifecycle_retry from reclaim",
			task:          base(trEntry(trT3, models.TaskEventReclaimedAfterRejection)),
			authoritative: true, want: FailureLifecycleRetry,
		},
		{
			name: "pre-point retry entries do not count",
			task: base(), authoritative: true, want: FailureRetryTail,
		},
		{
			name:          "retry_tail when none of the evidence matches",
			task:          base(trEntry(trT3, models.TaskEventHandoffResumed), trEntry(trT4, "invented_event")),
			authoritative: true, want: FailureRetryTail,
		},
		{
			name: "unknown_provenance when records are not authoritative",
			task: base(), authoritative: false, want: FailureUnknownProvenance,
		},
		{
			name:          "durable evidence outranks unknown provenance",
			task:          base(trEntry(trT3, models.TaskEventNewAttempt)),
			authoritative: false, want: FailureLifecycleRetry,
		},
		{
			name: "nil task with authoritative records is a retry tail",
			task: nil, authoritative: true, want: FailureRetryTail,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyFailure(tc.task, tc.anomalies, since, tc.authoritative)
			if got != tc.want {
				t.Errorf("ClassifyFailure = %q; want %q", got, tc.want)
			}
		})
	}

	t.Run("categories are the documented six in first-match order", func(t *testing.T) {
		want := []FailureCategory{
			FailureRejectionRCA, FailureCircuitOpen, FailureDuplicateAssessment,
			FailureLifecycleRetry, FailureRetryTail, FailureUnknownProvenance,
		}
		wantNames := []string{"rejection_rca", "circuit_open", "duplicate_assessment", "lifecycle_retry", "retry_tail", "unknown_provenance"}
		if len(FailureCategories) != len(want) {
			t.Fatalf("FailureCategories has %d entries; want %d", len(FailureCategories), len(want))
		}
		for i := range want {
			if FailureCategories[i] != want[i] || string(FailureCategories[i]) != wantNames[i] {
				t.Errorf("FailureCategories[%d] = %q; want %q", i, FailureCategories[i], wantNames[i])
			}
		}
		wantOutcomes := []string{"merged", "superseded", "abandoned", "blocked", "active", "unattributed"}
		if len(OutcomeClasses) != len(wantOutcomes) {
			t.Fatalf("OutcomeClasses has %d entries; want %d", len(OutcomeClasses), len(wantOutcomes))
		}
		for i, name := range wantOutcomes {
			if string(OutcomeClasses[i]) != name {
				t.Errorf("OutcomeClasses[%d] = %q; want %q", i, OutcomeClasses[i], name)
			}
		}
	})
}
