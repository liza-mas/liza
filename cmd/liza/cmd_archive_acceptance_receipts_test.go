package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupArchiveCLIProject(t *testing.T, ids ...string) (string, string) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	state := testhelpers.CreateValidState()
	for _, id := range ids {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
		source := models.AcceptanceSource{
			Ref: "specs/plan.md#Boundary", Commit: strings.Repeat("1", 40), Blob: strings.Repeat("2", 40),
			ParentTask: "plan-1", ParentReviewCommit: strings.Repeat("3", 40),
		}
		reviewCommit := strings.Repeat("4", 40)
		task.ReviewCommit = &reviewCommit
		task.AcceptanceSource = &source
		task.AcceptanceReceipt = &models.AcceptanceReceipt{
			Version: 1, ReviewCommit: reviewCommit, Source: source,
			ManifestPath: "acceptance/boundary.json", ManifestBlob: strings.Repeat("5", 40),
			Mappings: []referencecontract.AcceptanceMapping{{ObligationID: "AC-" + id}},
		}
		state.Tasks = append(state.Tasks, task)
	}
	testhelpers.WriteInitialState(t, statePath, state)
	return root, statePath
}

func TestArchiveAcceptanceReceiptsCLIRequiresOperator(t *testing.T) {
	for _, envName := range []string{brand.EnvName("AGENT_ID"), brand.LegacyEnvName("AGENT_ID")} {
		t.Run(envName, func(t *testing.T) {
			resetRootCmdForTest(t)
			t.Setenv(brand.EnvName("AGENT_ID"), "")
			t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
			t.Setenv(envName, "coder-1")
			root, statePath := setupArchiveCLIProject(t, "done")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			_, err = executeRootCommandCapture(t, root, "archive-acceptance-receipts")
			if err == nil || !strings.Contains(err.Error(), "operator-only") {
				t.Fatalf("agent session archive = %v, want operator-only refusal", err)
			}
			if after, _ := os.ReadFile(statePath); !bytes.Equal(before, after) {
				t.Fatal("refused agent session changed state")
			}
			if _, err := os.Stat(paths.New(root).ArchiveDir()); !os.IsNotExist(err) {
				t.Fatalf("refused agent session touched the archive: %v", err)
			}
		})
	}
}

func TestArchiveAcceptanceReceiptsCLIDrainsInBatches(t *testing.T) {
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	// More tasks than one default batch holds, so a single default call cannot pass.
	ids := make([]string, ops.DefaultArchiveLimit.MaxTasks+2)
	for i := range ids {
		ids[i] = fmt.Sprintf("t%02d", i)
	}
	root, statePath := setupArchiveCLIProject(t, ids...)
	// The summary goes to the command's output, which resetRootCmdForTest
	// discards; capture it the way cmd_cleanup_test does.
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--project-root", root, "archive-acceptance-receipts", "--max-tasks", "3"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("operator archive = %v", err)
	}
	want := fmt.Sprintf("Archived %d acceptance receipts in 4 batches", len(ids))
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("summary = %q, want %q", stdout, want)
	}
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if task := state.FindTask(id); task.AcceptanceReceipt != nil || len(task.Archived) != 1 {
			t.Fatalf("task %s not archived: %+v", id, task.Archived)
		}
	}
	objects, _ := filepath.Glob(filepath.Join(paths.New(root).ArchiveDir(), "objects", "*", "*.json"))
	if len(objects) != len(ids) {
		t.Fatalf("archive objects = %d, want %d", len(objects), len(ids))
	}
}

func TestArchiveAcceptanceReceiptsCLIJSONRefusal(t *testing.T) {
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "coder-1")
	root, statePath := setupArchiveCLIProject(t, "done")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := executeRootCommandCapture(t, root, "archive-acceptance-receipts", "--json")
	if err == nil {
		t.Fatal("agent session archive succeeded")
	}
	envelope := parseEnvelope(t, stdout)
	if envelope["ok"] != false || !strings.Contains(stdout, "operator-only") || !strings.Contains(stdout, models.LifecycleForbidden) {
		t.Fatalf("refusal envelope = %s", stdout)
	}
	if after, _ := os.ReadFile(statePath); !bytes.Equal(before, after) {
		t.Fatal("refused agent session changed state")
	}
}
