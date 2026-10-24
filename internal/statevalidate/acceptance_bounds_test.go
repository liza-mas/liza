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
		want                       string
	}{
		{"256 mappings", 256, 1, 0, ""},
		{"257 mappings", 257, 1, 0, "invalid manifest or result bounds (mappings)"},
		{"64 commands", 1, 64, 0, ""},
		{"65 commands", 1, 65, 0, "invalid manifest or result bounds (commands)"},
		{"1048576 output bytes", 1, 1, 1048576, ""},
		{"1048577 output bytes", 1, 1, 1048577, "output exceeds 1 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := acceptanceViolations(acceptanceReceiptTask(tc.mappings, tc.commands, tc.output))
			if tc.want == "" && got != "" {
				t.Fatalf("unexpected violations: %s", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("violations %q lack %q", got, tc.want)
			}
		})
	}
}

func TestAcceptanceDigestLengthEdges(t *testing.T) {
	task := acceptanceReceiptTask(1, 1, 0)
	task.AcceptanceReceipt.ManifestBlob = strings.Repeat("a", 41)
	if got := acceptanceViolations(task); !strings.Contains(got, "requires manifest blob identity") {
		t.Fatalf("41-char blob accepted: %q", got)
	}
	task = acceptanceReceiptTask(1, 1, 0)
	task.AcceptanceReceipt.Commands[0].CommandSHA256 = strings.Repeat("b", 63)
	if got := acceptanceViolations(task); !strings.Contains(got, "requires canonical command identity") {
		t.Fatalf("63-char command digest accepted: %q", got)
	}
}
