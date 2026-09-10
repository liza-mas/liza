package ops

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestMergedReviewedRange(t *testing.T) {
	t.Parallel()

	completeMerge, completeBase, completeReview := "merge", "base", "review"
	partialBase := "base"

	for _, tc := range []struct {
		name        string
		task        *models.Task
		wantBase    string
		wantReview  string
		wantPresent bool
		wantErr     string
	}{
		{name: "complete", task: &models.Task{ID: "task-1", MergeCommit: &completeMerge, BaseCommit: &completeBase, ReviewCommit: &completeReview}, wantBase: completeBase, wantReview: completeReview, wantPresent: true},
		{name: "legacy all absent", task: &models.Task{ID: "task-legacy"}},
		{name: "partial", task: &models.Task{ID: "task-corrupt", BaseCommit: &partialBase}, wantErr: "lacks reviewed change attribution"},
		{name: "nil", wantErr: "merged task is nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, review, present, err := MergedReviewedRange(tc.task)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("MergedReviewedRange() error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || base != tc.wantBase || review != tc.wantReview || present != tc.wantPresent {
				t.Fatalf("MergedReviewedRange() = (%q, %q, %v, %v), want (%q, %q, %v, nil)", base, review, present, err, tc.wantBase, tc.wantReview, tc.wantPresent)
			}
		})
	}
}
