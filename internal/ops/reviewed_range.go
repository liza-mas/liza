package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
)

// MergedReviewedRange classifies the retained review attribution of a merged task.
// Older tasks with no attribution remain on the legacy path; partial attribution
// is corrupt and fails closed.
func MergedReviewedRange(task *models.Task) (base, review string, present bool, err error) {
	if task == nil {
		return "", "", false, fmt.Errorf("merged task is nil")
	}
	merge := derefNonEmpty(task.MergeCommit)
	base = derefNonEmpty(task.BaseCommit)
	review = derefNonEmpty(task.ReviewCommit)
	if merge == "" && base == "" && review == "" {
		return "", "", false, nil
	}
	if merge == "" || base == "" || review == "" {
		return "", "", false, fmt.Errorf("merged task %q lacks reviewed change attribution", task.ID)
	}
	return base, review, true, nil
}

func derefNonEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
