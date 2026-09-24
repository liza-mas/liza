package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func archiveInspectTask(id string, status models.TaskStatus, now time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, now)
	source := models.AcceptanceSource{
		Ref: "specs/plan.md#Boundary", Commit: strings.Repeat("1", 40), Blob: strings.Repeat("2", 40),
		ParentTask: "plan-1", ParentReviewCommit: strings.Repeat("3", 40),
	}
	reviewCommit := strings.Repeat("4", 40)
	index := 0
	task.ReviewCommit = &reviewCommit
	task.AcceptanceSource = &source
	task.AcceptanceReceipt = &models.AcceptanceReceipt{
		Version: 1, ReviewCommit: reviewCommit, Source: source,
		ManifestPath: "acceptance/boundary.json", ManifestBlob: strings.Repeat("5", 40),
		Mappings: []referencecontract.AcceptanceMapping{{ObligationID: "AC-" + id, CommandIndex: &index}},
		Commands: []models.AcceptanceCommandResult{{
			Command: "run-tests", CommandSHA256: strings.Repeat("6", 64), StartedAt: now, FinishedAt: now.Add(time.Second),
			Output: "  kept verbatim for " + id + "\n",
		}},
	}
	return task
}

func TestInspectRestoresArchivedAcceptanceReceipts(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	original := archiveInspectTask("done", models.TaskStatusMerged, now)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{original}
	testhelpers.WriteInitialState(t, statePath, state)
	if result, err := ops.ArchiveTerminalAcceptanceReceipts(root, nil, ops.DefaultArchiveLimit); err != nil || len(result.Archived) != 1 {
		t.Fatalf("archive setup = %+v, %v", result, err)
	}

	type receiptView struct {
		Receipt  *models.AcceptanceReceipt `json:"acceptance_receipt"`
		Archived []models.ArchivedFieldRef `json:"archived"`
	}
	check := func(t *testing.T, label string, view receiptView, wantRefs bool) {
		t.Helper()
		if view.Receipt == nil || view.Receipt.Commands[0].Output != original.AcceptanceReceipt.Commands[0].Output || view.Receipt.Mappings[0].ObligationID != "AC-done" {
			t.Fatalf("%s receipt = %+v, want the archived receipt restored", label, view.Receipt)
		}
		if wantRefs && len(view.Archived) != 1 {
			t.Fatalf("%s archived refs = %+v, want the traceability ref", label, view.Archived)
		}
	}

	single, err := InspectCommand([]string{"tasks", "done"}, InspectOptions{Format: "json", ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var view receiptView
	if err := json.Unmarshal([]byte(single), &view); err != nil {
		t.Fatal(err)
	}
	check(t, "single task", view, true)

	list, err := InspectCommand([]string{"tasks"}, InspectOptions{Format: "json", ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var views []receiptView
	if err := json.Unmarshal([]byte(list), &views); err != nil || len(views) != 1 {
		t.Fatalf("task list = %q, %v", list, err)
	}
	check(t, "task list", views[0], true)

	field, err := InspectCommand([]string{"task.done.acceptance_receipt"}, InspectOptions{Format: "json", ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var receipt models.AcceptanceReceipt
	if err := json.Unmarshal([]byte(field), &receipt); err != nil {
		t.Fatalf("receipt field = %q, %v", field, err)
	}
	check(t, "dotted field", receiptView{Receipt: &receipt}, false)
}

// A compact T1 whose archive object is missing or corrupt must fail only the
// queries that output T1's receipt.
func TestInspectArchiveFailuresAreScopedToReceiptOutput(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	sha := strings.Repeat("d", 64)
	objectPath := filepath.Join(paths.ProjectDirName(), "archive", "objects", sha[:2], sha+".json")
	for _, variant := range []string{"missing", "corrupt"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			compact := archiveInspectTask("t1", models.TaskStatusMerged, now)
			compact.AcceptanceReceipt = nil
			compact.Archived = []models.ArchivedFieldRef{{Field: models.ArchivedFieldAcceptanceReceipt, SHA256: sha, ArchivedAt: now}}
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{compact, archiveInspectTask("t2", models.TaskStatusMerged, now)}
			testhelpers.WriteInitialState(t, statePath, state)
			if variant == "corrupt" {
				full := filepath.Join(root, objectPath)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(`{"format_version":1}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			for _, tc := range []struct {
				args []string
				opts InspectOptions
			}{
				{[]string{"tasks.completion_rate"}, InspectOptions{Format: "json"}},
				{[]string{"tasks"}, InspectOptions{Summary: true, Format: "json"}},
				{[]string{"tasks"}, InspectOptions{OutputSummary: true, Format: "json"}},
				{[]string{"tasks"}, InspectOptions{Format: "table"}},
				{[]string{"tasks", "t1"}, InspectOptions{Format: "json", Fields: []string{"status,history"}}},
				{[]string{"task.t1.status"}, InspectOptions{Format: "json"}},
				{[]string{"tasks", "t2"}, InspectOptions{Format: "json"}},
			} {
				tc.opts.ProjectRoot = root
				if _, err := InspectCommand(tc.args, tc.opts); err != nil {
					t.Errorf("%v %+v failed on an unrelated archive problem: %v", tc.args, tc.opts, err)
				}
			}

			for _, tc := range []struct {
				args []string
				opts InspectOptions
			}{
				{[]string{"tasks", "t1"}, InspectOptions{Format: "json"}},
				{[]string{"tasks", "t1"}, InspectOptions{Format: "yaml"}},
				{[]string{"tasks"}, InspectOptions{Format: "json"}},
				{[]string{"task.t1.acceptance_receipt"}, InspectOptions{Format: "json"}},
			} {
				tc.opts.ProjectRoot = root
				_, err := InspectCommand(tc.args, tc.opts)
				var archiveErr *ops.ArchiveObjectError
				if !errors.As(err, &archiveErr) || archiveErr.TaskID != "t1" || archiveErr.SHA256 != sha {
					t.Errorf("%v %+v error = %v, want an explicit archive error for t1", tc.args, tc.opts, err)
				}
			}
		})
	}
}

// Restoration on the remaining surfaces that carry the receipt: structured
// (Internal) results, receipt projections, YAML, and dotted task IDs — with
// lifecycle redaction intact alongside the restored receipt.
func TestInspectRestoresArchivedReceiptOnEverySurface(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	original := archiveInspectTask("work.part", models.TaskStatusMerged, now)
	original.Lifecycle = &models.TaskLifecycle{Preparation: &models.LifecyclePreparation{LifecycleIdentity: models.LifecycleIdentity{GenerationDigest: "secret-generation-digest"}}}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{original}
	testhelpers.WriteInitialState(t, statePath, state)
	if result, err := ops.ArchiveTerminalAcceptanceReceipts(root, nil, ops.DefaultArchiveLimit); err != nil || len(result.Archived) != 1 {
		t.Fatalf("archive setup = %+v, %v", result, err)
	}
	wantOutput := original.AcceptanceReceipt.Commands[0].Output

	live, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	internal, err := inspectTask(live, "work.part", inspectTasksOptions{Internal: true, ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if info, ok := internal.(taskInfo); !ok || info.AcceptanceReceipt == nil || info.AcceptanceReceipt.Commands[0].Output != wantOutput {
		t.Fatalf("internal task info = %#v, want the restored receipt", internal)
	}
	list, err := inspectTasks(live, inspectTasksOptions{Internal: true, ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if infos, ok := list.([]taskInfo); !ok || len(infos) != 1 || infos[0].AcceptanceReceipt == nil {
		t.Fatalf("internal task list = %#v, want the restored receipt", list)
	}

	for _, tc := range []struct {
		name string
		args []string
		opts InspectOptions
	}{
		{"yaml task", []string{"tasks", "work.part"}, InspectOptions{Format: "yaml"}},
		{"dotted-id field", []string{"task.work.part.acceptance_receipt"}, InspectOptions{Format: "yaml"}},
		{"receipt projection", []string{"tasks", "work.part"}, InspectOptions{Format: "yaml", Fields: []string{"acceptance_receipt,lifecycle"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.ProjectRoot = root
			got, err := InspectCommand(tc.args, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, strings.TrimSpace(wantOutput)) {
				t.Fatalf("output lacks the restored receipt:\n%s", got)
			}
			if strings.Contains(got, "secret-generation-digest") {
				t.Fatalf("restoration bypassed lifecycle redaction:\n%s", got)
			}
		})
	}
}
