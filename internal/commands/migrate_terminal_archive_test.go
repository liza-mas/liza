package commands

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/archiveobject"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestTerminalArchiveMigrationRetainsUnnormalizedLogicalEvidence(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	created := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	oversized := strings.Repeat("legacy evidence ", 400)
	task := models.Task{
		ID: "legacy-terminal", Type: models.TaskTypeCoding, RolePair: "coding-pair", Status: models.TaskStatusMerged,
		Description: "preserved complete record", Scope: "migration", DoneWhen: "legacy fields repaired", SpecRef: "specs/vision.md", Created: created,
		History: []models.TaskHistoryEntry{{Time: created, Event: models.TaskEventOrchestratorAssessment, Reason: &oversized}},
		Output:  []models.OutputEntry{{Desc: "preserved output", DoneWhen: "verified", Scope: "child", SpecRef: "specs/vision.md"}},
		Extra:   map[string]any{"attempted": []any{"coder-previous"}},
	}
	data, err := db.EncodeTerminalTask(&task)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := archiveobject.Write(paths.New(root).ArchiveDir(), data, nil)
	if err != nil {
		t.Fatal(err)
	}
	state := testhelpers.CreateValidState()
	state.Config.TerminalTaskArchival = true
	state.Tasks = []models.Task{{ID: task.ID, Status: task.Status, Created: created, TerminalArchive: &models.TerminalArchiveRef{SHA256: digest, ArchivedAt: created.Add(time.Hour)}}}
	state.Agents["code-reviewer-1"] = models.Agent{Role: "code_reviewer"}
	physical, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	// Build the stored projection manually: bb.Write must reject an un-restored
	// physical row, and zero-value task fields do not belong in a real stub.
	var mapping map[string]any
	if err := yaml.Unmarshal(physical, &mapping); err != nil {
		t.Fatal(err)
	}
	mapping["tasks"] = []any{map[string]any{"id": task.ID, "status": task.Status, "created": created, "terminal_archive": state.Tasks[0].TerminalArchive}}
	physical, err = yaml.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, physical, 0o644); err != nil {
		t.Fatal(err)
	}
	bb := db.For(statePath)
	before, err := bb.ReadSnapshot() // Normalized ordinary reads must not hide migration work.
	if err != nil {
		t.Fatal(err)
	}
	beforeID := models.TaskTransitionID(before.FindTask(task.ID))
	changed, err := MigrateCommand(statePath)
	if err != nil || !changed {
		t.Fatalf("migration = changed %v, %v", changed, err)
	}
	logical, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	got := logical.FindTask(task.ID)
	if got.Attempt != 2 || got.Description != task.Description || len(got.Output) != 1 || len(got.History) != 1 || models.TaskTransitionID(got) != beforeID {
		t.Fatal("migration lost terminal record or changed its logical boundary")
	}
	if _, found := got.Extra["attempted"]; found || logical.Agents["code-reviewer-1"].Role != "code-reviewer" {
		t.Fatal("archive restoration normalized legacy fields before migration could detect them")
	}
	if got.History[0].Reason == nil || len(*got.History[0].Reason) > statehygiene.MaxStateTextBytes || *got.History[0].Reason == oversized {
		t.Fatal("migration failed to scrub legacy oversized terminal evidence")
	}
	if got.TerminalArchive == nil || got.TerminalArchive.SHA256 == digest {
		t.Fatal("migrated terminal evidence did not publish a new cold object")
	}
	oldPath, err := archiveobject.Path(paths.New(root).ArchiveDir(), digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("migration removed immutable old evidence: %v", err)
	}
}
