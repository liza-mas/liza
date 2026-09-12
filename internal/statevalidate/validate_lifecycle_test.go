package statevalidate

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func validLifecycleTask() *models.Task {
	digest := strings.Repeat("a", 64)
	return &models.Task{ID: "task-1", Lifecycle: &models.TaskLifecycle{Revision: 1, CompletionSequence: 1, Receipts: []models.LifecycleReceipt{{
		LifecycleIdentity: models.LifecycleIdentity{Operation: "submit-for-review", Actor: "coder-1", RequestID: "request-1", ExpectedTransition: digest, PayloadDigest: digest, GenerationDigest: digest},
		Sequence:          1, TransitionID: digest, Projection: models.LifecycleProjection{InputCommit: strings.Repeat("b", 40)},
	}}}}
}

func TestValidateTaskLifecycleAcceptsLegacyAndRoundTrip(t *testing.T) {
	t.Parallel()
	var legacy models.Task
	if err := yaml.Unmarshal([]byte("id: legacy\nstatus: DRAFT\n"), &legacy); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskLifecycle(&legacy); err != nil {
		t.Fatalf("old state rejected: %v", err)
	}
	task := validLifecycleTask()
	data, err := yaml.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var restored models.Task
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskLifecycle(&restored); err != nil {
		t.Fatalf("valid persisted receipt rejected: %v", err)
	}
	if restored.Lifecycle.Receipts[0].GenerationDigest != task.Lifecycle.Receipts[0].GenerationDigest {
		t.Fatal("receipt generation lost on round trip")
	}
}

func TestValidateTaskLifecycleRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*models.TaskLifecycle)
		want   string
	}{
		{"sequence exceeds revision", func(l *models.TaskLifecycle) { l.CompletionSequence = 2 }, "exceeds revision"},
		{"unknown operation", func(l *models.TaskLifecycle) { l.Receipts[0].Operation = "future-operation" }, "unknown operation"},
		{"invalid actor", func(l *models.TaskLifecycle) { l.Receipts[0].Actor = "bad actor" }, "actor"},
		{"oversized request", func(l *models.TaskLifecycle) { l.Receipts[0].RequestID = strings.Repeat("r", 129) }, "request_id"},
		{"invalid generation", func(l *models.TaskLifecycle) { l.Receipts[0].GenerationDigest = "do-not-echo-this" }, "generation_digest"},
		{"zero sequence", func(l *models.TaskLifecycle) { l.Receipts[0].Sequence = 0 }, "sequences"},
		{"evicted global boundary", func(l *models.TaskLifecycle) { l.Revision, l.CompletionSequence = 17, 17 }, "completion window"},
		{"short commit", func(l *models.TaskLifecycle) { l.Receipts[0].Projection.InputCommit = "abc1234" }, "full lowercase Git"},
		{"invalid verdict", func(l *models.TaskLifecycle) { l.Receipts[0].Projection.Verdict = "IGNORE" }, "verdict"},
		{"negative attempt", func(l *models.TaskLifecycle) { l.Receipts[0].Projection.Attempt = -1 }, "counter"},
		{"invalid lease", func(l *models.TaskLifecycle) { l.Receipts[0].Projection.LeaseExpires = "tomorrow" }, "lease_expires"},
		{"invalid preparation", func(l *models.TaskLifecycle) {
			l.Preparation = &models.LifecyclePreparation{LifecycleIdentity: l.Receipts[0].LifecycleIdentity, Boundary: "bad"}
		}, "preparation boundary"},
		{"duplicate identity", func(l *models.TaskLifecycle) {
			receipt := l.Receipts[0]
			receipt.Sequence = 2
			l.Revision, l.CompletionSequence = 2, 2
			l.Receipts = append(l.Receipts, receipt)
		}, "duplicate receipt identity"},
		{"operation capacity", func(l *models.TaskLifecycle) {
			for i := uint64(2); i <= 5; i++ {
				receipt := l.Receipts[0]
				receipt.Sequence = i
				receipt.RequestID = strings.Repeat("r", int(i))
				l.Receipts = append(l.Receipts, receipt)
			}
			l.Revision, l.CompletionSequence = 5, 5
		}, "too many receipts for one operation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := validLifecycleTask()
			tt.mutate(task.Lifecycle)
			err := ValidateTaskLifecycle(task)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q rejection, got %v", tt.want, err)
			}
			if strings.Contains(err.Error(), "do-not-echo-this") {
				t.Fatal("validator disclosed authority material")
			}
		})
	}
}

func TestLifecycleReviewerClaimLegacyCommitScope(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim-reviewer-task", "submit-for-review", "submit-verdict", "wt-merge"} {
		for _, value := range []string{"abc123", "review123", "bad commit", strings.Repeat("r", 129)} {
			t.Run(operation+"/"+value, func(t *testing.T) {
				task := validLifecycleTask()
				receipt := &task.Lifecycle.Receipts[0]
				receipt.Operation = operation
				receipt.Projection.ReviewCommit = value
				err := ValidateTaskLifecycle(task)
				valid := operation == "claim-reviewer-task" && (value == "abc123" || value == "review123")
				if (err == nil) != valid {
					t.Fatalf("operation %s review_commit %q: error=%v, want valid=%v", operation, value, err, valid)
				}
			})
		}
	}
	for _, field := range []string{"input", "base", "merge"} {
		t.Run("reviewer-claim/"+field, func(t *testing.T) {
			task := validLifecycleTask()
			receipt := &task.Lifecycle.Receipts[0]
			receipt.Operation = "claim-reviewer-task"
			receipt.Projection.ReviewCommit = "abc123"
			switch field {
			case "input":
				receipt.Projection.InputCommit = "abc123"
			case "base":
				receipt.Projection.BaseCommit = "abc123"
			case "merge":
				receipt.Projection.MergeCommit = "abc123"
			}
			if err := ValidateTaskLifecycle(task); err == nil {
				t.Fatalf("reviewer claim accepted abbreviated %s commit", field)
			}
		})
	}
}
