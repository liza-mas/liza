package ops

import (
	"bytes"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func TestTerminalArchiveMaintenanceOptInBoundsAndReceiptCompatibility(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	root, statePath := setupArchiveProject(t,
		archiveTestTask("merged", models.TaskStatusMerged, now),
		archiveTestTask("abandoned", models.TaskStatusAbandoned, now),
		archiveTestTask("superseded", models.TaskStatusSuperseded, now),
		archiveTestTask("active", models.TaskStatusReadyForReview, now),
	)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ArchiveTerminalTasks(root, nil, DefaultArchiveLimit); err != nil || len(result.Archived) != 0 {
		t.Fatalf("disabled maintenance = %+v, %v", result, err)
	}
	if after, err := os.ReadFile(statePath); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("disabled maintenance published a mutation: %v", err)
	}
	if _, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit); err != nil {
		t.Fatal(err)
	}
	first, err := EnableAndArchiveTerminalTasks(root, ArchiveLimit{MaxTasks: 1, MaxBytes: 1})
	if err != nil || !slices.Equal(first.Archived, []string{"merged"}) || first.Remaining != 2 || first.Bytes <= 1 {
		t.Fatalf("first-object progress and count bounds = %+v, %v", first, err)
	}
	second, err := ArchiveTerminalTasks(root, nil, DefaultArchiveLimit)
	if err != nil || !slices.Equal(second.Archived, []string{"abandoned", "superseded"}) || second.Remaining != 0 {
		t.Fatalf("remaining batch = %+v, %v", second, err)
	}
	bb := db.New(statePath)
	logical, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !logical.Config.TerminalTaskArchival || logical.FindTask("active").TerminalArchive != nil {
		t.Fatal("maintenance did not enable config or archived an active task")
	}
	merged := logical.FindTask("merged")
	if merged.AcceptanceReceipt != nil || len(merged.Archived) != 1 {
		t.Fatal("whole-task restoration changed receipt inspection-only behavior")
	}
	copy := *merged
	if err := HydrateArchivedFields(root, &copy); err != nil || copy.AcceptanceReceipt == nil {
		t.Fatalf("receipt object compatibility = %v", err)
	}
	before, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ArchiveTerminalTasks(root, nil, DefaultArchiveLimit); err != nil || len(result.Archived) != 0 {
		t.Fatalf("empty maintenance = %+v, %v", result, err)
	}
	if after, err := os.ReadFile(statePath); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("empty maintenance changed publication sequence/state: %v", err)
	}
	restored, err := RestoreTerminalTasksInline(root)
	if err != nil || !slices.Equal(restored, []string{"merged", "abandoned", "superseded"}) {
		t.Fatalf("restore-inline = %v, %v", restored, err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var inline models.State
	if err := yaml.Unmarshal(data, &inline); err != nil {
		t.Fatal(err)
	}
	if inline.Config.TerminalTaskArchival || inline.FindTask("merged").TerminalArchive != nil || !reflect.DeepEqual(inline.FindTask("merged").History, merged.History) {
		t.Fatal("rollback did not publish complete inline history without archive references")
	}
}

func TestTerminalArchiveEnableWithoutBacklogPublishesConfig(t *testing.T) {
	root, path := setupArchiveProject(t)
	result, err := EnableAndArchiveTerminalTasks(root, DefaultArchiveLimit)
	if err != nil || len(result.Archived) != 0 {
		t.Fatalf("enable without backlog = %+v, %v", result, err)
	}
	state, err := db.New(path).ReadSnapshot()
	if err != nil || !state.Config.TerminalTaskArchival || state.MutationSequence != 1 {
		t.Fatalf("enable did not publish configuration: state=%v error=%v", state, err)
	}
}
