package models

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func baseRejectionRCARequest() RejectionRCARequest {
	return RejectionRCARequest{
		SchemaVersion: RejectionRCASchemaVersion,
		Summary:       "Three rejections: two product defects and one capability failure.",
		Contributions: []RejectionRCAContribution{
			{
				RejectionIndex: 1,
				Categories:     []string{RejectionCauseProductDefect},
				Evidence:       []string{"review-1: identity scalar mismatch"},
			},
			{
				RejectionIndex: 2,
				Categories:     []string{RejectionCauseCapabilityFailure, RejectionCauseLifecycleRetry},
				Evidence:       []string{"review-2: no real Postgres available", "lease expired mid-review"},
			},
		},
	}
}

func TestRejectionRCAFingerprint(t *testing.T) {
	base := baseRejectionRCARequest()
	baseFingerprint := RejectionRCAFingerprint(base)
	if baseFingerprint == "" {
		t.Fatal("RejectionRCAFingerprint() returned an empty digest")
	}

	equivalent := []struct {
		name    string
		request RejectionRCARequest
	}{
		{
			name: "whitespace",
			request: RejectionRCARequest{
				SchemaVersion: RejectionRCASchemaVersion,
				Summary:       "  Three rejections:  two product defects  and one capability failure.\n",
				Contributions: []RejectionRCAContribution{
					{
						RejectionIndex: 1,
						Categories:     []string{" product_defect "},
						Evidence:       []string{"review-1:   identity scalar mismatch  "},
					},
					{
						RejectionIndex: 2,
						Categories:     []string{"capability_failure\t", "\nlifecycle_retry"},
						Evidence:       []string{" review-2: no real Postgres available", "lease expired  mid-review "},
					},
				},
			},
		},
		{
			name: "category order",
			request: RejectionRCARequest{
				SchemaVersion: base.SchemaVersion,
				Summary:       base.Summary,
				Contributions: []RejectionRCAContribution{
					base.Contributions[0],
					{
						RejectionIndex: 2,
						Categories:     []string{RejectionCauseLifecycleRetry, RejectionCauseCapabilityFailure},
						Evidence:       base.Contributions[1].Evidence,
					},
				},
			},
		},
		{
			name: "contribution order",
			request: RejectionRCARequest{
				SchemaVersion: base.SchemaVersion,
				Summary:       base.Summary,
				Contributions: []RejectionRCAContribution{base.Contributions[1], base.Contributions[0]},
			},
		},
	}
	for _, tc := range equivalent {
		t.Run("equivalent/"+tc.name, func(t *testing.T) {
			if got := RejectionRCAFingerprint(tc.request); got != baseFingerprint {
				t.Fatalf("RejectionRCAFingerprint() = %s, want %s", got, baseFingerprint)
			}
		})
	}

	t.Run("equivalent/json key order", func(t *testing.T) {
		ordered := `{"schema_version":1,"summary":"S","contributions":[{"rejection_index":1,"categories":["product_defect"],"evidence":["e"]}]}`
		shuffled := `{"contributions":[{"evidence":["e"],"categories":["product_defect"],"rejection_index":1}],"summary":"S","schema_version":1}`
		var first, second RejectionRCARequest
		if err := json.Unmarshal([]byte(ordered), &first); err != nil {
			t.Fatalf("unmarshal ordered request: %v", err)
		}
		if err := json.Unmarshal([]byte(shuffled), &second); err != nil {
			t.Fatalf("unmarshal shuffled request: %v", err)
		}
		if RejectionRCAFingerprint(first) != RejectionRCAFingerprint(second) {
			t.Fatal("key-order-equivalent request files produced different fingerprints")
		}
	})

	different := []struct {
		name    string
		request RejectionRCARequest
	}{
		{
			name: "changed category",
			request: RejectionRCARequest{
				SchemaVersion: base.SchemaVersion,
				Summary:       base.Summary,
				Contributions: []RejectionRCAContribution{
					{
						RejectionIndex: 1,
						Categories:     []string{RejectionCauseLifecycleRetry},
						Evidence:       base.Contributions[0].Evidence,
					},
					base.Contributions[1],
				},
			},
		},
		{
			name: "changed evidence entry",
			request: RejectionRCARequest{
				SchemaVersion: base.SchemaVersion,
				Summary:       base.Summary,
				Contributions: []RejectionRCAContribution{
					{
						RejectionIndex: 1,
						Categories:     base.Contributions[0].Categories,
						Evidence:       []string{"review-1: concurrency defect"},
					},
					base.Contributions[1],
				},
			},
		},
		{
			name: "changed summary",
			request: RejectionRCARequest{
				SchemaVersion: base.SchemaVersion,
				Summary:       base.Summary + " Rerouting validation.",
				Contributions: base.Contributions,
			},
		},
	}
	for _, tc := range different {
		t.Run("distinct/"+tc.name, func(t *testing.T) {
			if got := RejectionRCAFingerprint(tc.request); got == baseFingerprint {
				t.Fatalf("RejectionRCAFingerprint() = %s, want a digest different from the base request", got)
			}
		})
	}

	t.Run("record projection reproduces the request digest", func(t *testing.T) {
		normalized := NormalizeRejectionRCARequest(base)
		recordedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
		record := RejectionRCARecord{
			SchemaVersion:  RejectionRCASchemaVersion,
			Threshold:      4,
			RejectionCount: 4,
			GatedAt:        time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC),
			GatingCommit:   "0123456789abcdef0123456789abcdef01234567",
			Fingerprint:    baseFingerprint,
			RecordedAt:     &recordedAt,
			RecordedBy:     "orchestrator-1",
			Summary:        normalized.Summary,
			Contributions:  normalized.Contributions,
			Disposition: &RejectionRCADisposition{
				RecoveryPath:     RecoveryCapabilityReroute,
				RestoreMode:      RestoreModeAssign,
				Actor:            "orchestrator-1",
				LifecycleVersion: 7,
				DecidedAt:        recordedAt,
				Rationale:        "reroute validation to a session with Postgres",
				IterationExempt:  true,
			},
		}
		if got := RejectionRCAFingerprint(record.Request()); got != baseFingerprint {
			t.Fatalf("RejectionRCAFingerprint(record.Request()) = %s, want %s", got, baseFingerprint)
		}
	})

	t.Run("normalization does not mutate its input", func(t *testing.T) {
		input := baseRejectionRCARequest()
		input.Contributions[1].Categories = []string{RejectionCauseLifecycleRetry, RejectionCauseCapabilityFailure}
		before := baseRejectionRCARequest()
		before.Contributions[1].Categories = []string{RejectionCauseLifecycleRetry, RejectionCauseCapabilityFailure}

		NormalizeRejectionRCARequest(input)
		RejectionRCAFingerprint(input)

		if !reflect.DeepEqual(input, before) {
			t.Fatalf("input mutated: got %+v, want %+v", input, before)
		}
	})
}

func TestRejectionRCAGateState(t *testing.T) {
	gatedAt := time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC)
	openRecord := func() *RejectionRCARecord {
		return &RejectionRCARecord{SchemaVersion: RejectionRCASchemaVersion, Threshold: 4, RejectionCount: 4, GatedAt: gatedAt}
	}
	closedRecord := func() *RejectionRCARecord {
		record := openRecord()
		record.Disposition = &RejectionRCADisposition{
			RecoveryPath: RecoveryImplementationCorrection,
			RestoreMode:  RestoreModeClaimable,
			Actor:        "orchestrator-1",
			DecidedAt:    gatedAt,
		}
		return record
	}

	t.Run("gate open", func(t *testing.T) {
		cases := []struct {
			name   string
			record *RejectionRCARecord
			want   bool
		}{
			{name: "no record", record: nil, want: false},
			{name: "record without disposition", record: openRecord(), want: true},
			{name: "record with disposition", record: closedRecord(), want: false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				task := &Task{ID: "task-1", RejectionRCA: tc.record}
				if got := task.RejectionRCAGateOpen(); got != tc.want {
					t.Fatalf("RejectionRCAGateOpen() = %v, want %v", got, tc.want)
				}
			})
		}
	})

	t.Run("gate due", func(t *testing.T) {
		cases := []struct {
			name       string
			record     *RejectionRCARecord
			rejections int
			want       bool
		}{
			{name: "no record below threshold", record: nil, rejections: 3, want: false},
			{name: "no record at threshold", record: nil, rejections: 4, want: true},
			{name: "open gate never re-fires", record: openRecord(), rejections: 12, want: false},
			{name: "closed gate one below the next cycle", record: closedRecord(), rejections: 7, want: false},
			{name: "closed gate at the next cycle", record: closedRecord(), rejections: 8, want: true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				task := &Task{ID: "task-1", RejectionRCA: tc.record, ReviewCyclesTotal: tc.rejections}
				if got := task.RejectionRCAGateDue(4); got != tc.want {
					t.Fatalf("RejectionRCAGateDue(4) with %d rejections = %v, want %v", tc.rejections, got, tc.want)
				}
			})
		}
	})

	t.Run("restore mode per recovery path", func(t *testing.T) {
		cases := map[string]string{
			RecoveryCapabilityReroute:        RestoreModeAssign,
			RecoveryLifecycleRepair:          RestoreModeAssign,
			RecoveryImplementationCorrection: RestoreModeClaimable,
			RecoveryHumanOverride:            RestoreModeClaimable,
			RecoveryRescope:                  RestoreModeNone,
		}
		for path, want := range cases {
			if got := RejectionRCARestoreMode(path); got != want {
				t.Fatalf("RejectionRCARestoreMode(%q) = %q, want %q", path, got, want)
			}
			if !IsRecoveryPath(path) {
				t.Fatalf("IsRecoveryPath(%q) = false, want true", path)
			}
		}
		if got := RejectionRCARestoreMode("teleport"); got != "" {
			t.Fatalf("RejectionRCARestoreMode(unknown) = %q, want an empty mode", got)
		}
		if IsRecoveryPath("teleport") {
			t.Fatal("IsRecoveryPath(\"teleport\") = true, want false")
		}
	})

	t.Run("unrecognized causes survive round-trip", func(t *testing.T) {
		for _, known := range []string{RejectionCauseProductDefect, RejectionCauseCapabilityFailure, RejectionCauseLifecycleRetry, RejectionCauseUnknown} {
			if !IsKnownRejectionCause(known) {
				t.Fatalf("IsKnownRejectionCause(%q) = false, want true", known)
			}
		}
		if IsKnownRejectionCause("toolchain_drift") {
			t.Fatal("IsKnownRejectionCause(\"toolchain_drift\") = true, want false")
		}
		request := RejectionRCARequest{
			SchemaVersion: RejectionRCASchemaVersion,
			Summary:       "one unrecognized cause",
			Contributions: []RejectionRCAContribution{{
				RejectionIndex: 1,
				Categories:     []string{"toolchain_drift"},
			}},
		}
		normalized := NormalizeRejectionRCARequest(request)
		if got := normalized.Contributions[0].Categories; len(got) != 1 || got[0] != "toolchain_drift" {
			t.Fatalf("normalized categories = %v, want the verbatim unrecognized cause", got)
		}
	})
}

func TestDurableRejectionCount(t *testing.T) {
	at := func(minute int) time.Time {
		return time.Date(2026, 9, 18, 10, minute, 0, 0, time.UTC)
	}
	entry := func(event string, minute int) TaskHistoryEntry {
		return TaskHistoryEntry{Time: at(minute), Event: event}
	}

	cases := []struct {
		name string
		task Task
		want int
	}{
		{
			name: "review cycles total wins over history",
			task: Task{
				ReviewCyclesTotal: 5,
				History:           []TaskHistoryEntry{entry(TaskEventRejected, 1)},
			},
			want: 5,
		},
		{
			name: "history fallback counts both rejection events",
			task: Task{History: []TaskHistoryEntry{
				entry(TaskEventRejected, 1),
				entry(TaskEventReviewVerdictRejected, 2),
				entry(TaskEventApproved, 3),
				entry(TaskEventSubmittedForReview, 4),
				entry(TaskEventBlocked, 5),
				entry(TaskEventReviewVerdictRejected, 6),
			}},
			want: 3,
		},
		{
			name: "history fallback survives a new attempt reset",
			task: Task{
				Iteration:           0,
				ReviewCyclesCurrent: 0,
				History: []TaskHistoryEntry{
					entry(TaskEventRejected, 1),
					entry(TaskEventRejected, 2),
					entry(TaskEventNewAttempt, 3),
					entry(TaskEventReviewVerdictRejected, 4),
				},
			},
			want: 3,
		},
		{
			name: "no rejections",
			task: Task{History: []TaskHistoryEntry{entry(TaskEventClaimed, 1)}},
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := tc.task
			if got := task.DurableRejectionCount(); got != tc.want {
				t.Fatalf("DurableRejectionCount() = %d, want %d", got, tc.want)
			}
		})
	}

	t.Run("total survives the new-attempt counter reset", func(t *testing.T) {
		task := Task{
			ReviewCyclesTotal:   4,
			ReviewCyclesCurrent: 0,
			Iteration:           0,
			History:             []TaskHistoryEntry{entry(TaskEventNewAttempt, 1)},
		}
		if got := task.DurableRejectionCount(); got != 4 {
			t.Fatalf("DurableRejectionCount() after new_attempt = %d, want 4", got)
		}
	})
}
