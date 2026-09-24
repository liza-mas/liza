package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func archivedReceiptTask(status models.TaskStatus) models.Task {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	task := testhelpers.BuildTaskByStatus("archived", status, now)
	source := models.AcceptanceSource{
		Ref: "specs/plan.md#Boundary", Commit: strings.Repeat("1", 40), Blob: strings.Repeat("2", 40),
		ParentTask: "plan-1", ParentReviewCommit: strings.Repeat("3", 40),
	}
	reviewCommit := strings.Repeat("4", 40)
	task.ReviewCommit = &reviewCommit
	task.AcceptanceSource = &source
	task.Archived = []models.ArchivedFieldRef{{Field: models.ArchivedFieldAcceptanceReceipt, SHA256: strings.Repeat("a", 64), ArchivedAt: now}}
	return task
}

func TestValidateAcceptanceStateAcceptsArchivedReceipt(t *testing.T) {
	for _, status := range []models.TaskStatus{models.TaskStatusMerged, models.TaskStatusSuperseded, models.TaskStatusAbandoned} {
		task := archivedReceiptTask(status)
		if err := validateAcceptanceState(&task); err != nil {
			t.Errorf("%s compact task rejected: %v", status, err)
		}
	}
}

func TestValidateAcceptanceStateRejectsInvalidArchivedRefs(t *testing.T) {
	cases := map[string]func(*models.Task){
		"unknown field":  func(task *models.Task) { task.Archived[0].Field = "history" },
		"short digest":   func(task *models.Task) { task.Archived[0].SHA256 = strings.Repeat("a", 63) },
		"upper digest":   func(task *models.Task) { task.Archived[0].SHA256 = strings.Repeat("A", 64) },
		"path in digest": func(task *models.Task) { task.Archived[0].SHA256 = "../" + strings.Repeat("a", 61) },
		"zero time":      func(task *models.Task) { task.Archived[0].ArchivedAt = time.Time{} },
		"second ref for one field": func(task *models.Task) {
			second := task.Archived[0]
			second.SHA256 = strings.Repeat("b", 64)
			task.Archived = append(task.Archived, second)
		},
		"duplicate ref": func(task *models.Task) { task.Archived = append(task.Archived, task.Archived[0]) },
		"non-terminal":  func(task *models.Task) { task.Status = models.TaskStatusReadyForReview },
		"live receipt too": func(task *models.Task) {
			task.AcceptanceReceipt = &models.AcceptanceReceipt{Version: 1, ReviewCommit: *task.ReviewCommit, Source: *task.AcceptanceSource, ManifestPath: "m.json", ManifestBlob: strings.Repeat("5", 40), Mappings: []referencecontract.AcceptanceMapping{{ObligationID: "AC-1"}}}
		},
		"no acceptance source": func(task *models.Task) { task.AcceptanceSource = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			task := archivedReceiptTask(models.TaskStatusMerged)
			mutate(&task)
			if err := validateAcceptanceState(&task); err == nil {
				t.Fatal("invalid archived ref accepted")
			}
		})
	}
}
