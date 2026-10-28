package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// Receipt bounds are checked at their literal edges, independent of the named
// constants that carry them.
func acceptanceReceiptTask(mappings, commands int, outputBytes int) *models.Task {
	sha1 := strings.Repeat("a", 40)
	sha256 := strings.Repeat("b", 64)
	source := models.AcceptanceSource{Ref: "plan.md#Task", Commit: sha1, Blob: sha1, ParentReviewCommit: sha1, ParentTask: "parent"}
	review := "review"
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	receipt := &models.AcceptanceReceipt{
		Version: 1, ReviewCommit: review, Source: source,
		ManifestPath: "acceptance/m.json", ManifestBlob: sha1,
		Mappings: make([]referencecontract.AcceptanceMapping, mappings),
	}
	for i := 0; i < commands; i++ {
		result := models.AcceptanceCommandResult{CommandSHA256: sha256, StartedAt: now, FinishedAt: now}
		if i == 0 {
			result.Output = strings.Repeat("o", outputBytes)
		}
		receipt.Commands = append(receipt.Commands, result)
	}
	return &models.Task{ID: "task-1", ReviewCommit: &review, AcceptanceSource: &source, AcceptanceReceipt: receipt}
}

func acceptanceViolations(task *models.Task) string {
	v := &violations{}
	validateAcceptanceState(v, task)
	if err := v.err(); err != nil {
		return err.Error()
	}
	return ""
}

func TestAcceptanceReceiptBoundEdges(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		mappings, commands, output int
		want                       string // the complete ordered diagnostics, one per line
	}{
		{"256 mappings", 256, 1, 0, ""},
		{"257 mappings", 257, 1, 0, "task task-1 acceptance_receipt has invalid manifest or result bounds (mappings)"},
		{"64 commands", 1, 64, 0, ""},
		{"65 commands", 1, 65, 0, "task task-1 acceptance_receipt has invalid manifest or result bounds (commands)"},
		{"1048576 output bytes", 1, 1, 1048576, ""},
		{"1048577 output bytes", 1, 1, 1048577, "task task-1 acceptance_receipt output exceeds 1 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := acceptanceViolations(acceptanceReceiptTask(tc.mappings, tc.commands, tc.output)); got != tc.want {
				t.Fatalf("violations = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAcceptanceDigestLengthEdges(t *testing.T) {
	task := acceptanceReceiptTask(1, 1, 0)
	task.AcceptanceReceipt.ManifestBlob = strings.Repeat("a", 41)
	if got, want := acceptanceViolations(task), "task task-1 acceptance_receipt requires manifest blob identity"; got != want {
		t.Fatalf("41-char blob: violations = %q, want %q", got, want)
	}
	task = acceptanceReceiptTask(1, 1, 0)
	task.AcceptanceReceipt.Commands[0].CommandSHA256 = strings.Repeat("b", 63)
	if got, want := acceptanceViolations(task), "task task-1 acceptance_receipt requires canonical command identity (command 0)"; got != want {
		t.Fatalf("63-char command digest: violations = %q, want %q", got, want)
	}
}

// Every bound broken at once pins the emission order: receipt bounds, manifest
// blob, per-command identity, then total output.
func TestAcceptanceReceiptMultiDefectOrder(t *testing.T) {
	task := acceptanceReceiptTask(257, 65, 1048577)
	task.AcceptanceReceipt.ManifestBlob = strings.Repeat("a", 41)
	task.AcceptanceReceipt.Commands[1].CommandSHA256 = strings.Repeat("b", 63)
	want := "task task-1 acceptance_receipt has invalid manifest or result bounds (mappings)\n" +
		"task task-1 acceptance_receipt has invalid manifest or result bounds (commands)\n" +
		"task task-1 acceptance_receipt requires manifest blob identity\n" +
		"task task-1 acceptance_receipt requires canonical command identity (command 1)\n" +
		"task task-1 acceptance_receipt output exceeds 1 MiB"
	if got := acceptanceViolations(task); got != want {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}
