package statevalidate

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

var lifecycleIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// ValidateTaskLifecycle accepts pre-receipt states and bounds optional replay
// metadata. Errors identify the invalid field without echoing authority hashes
// or payload data. The same checks run before receipt helpers commit metadata.
func ValidateTaskLifecycle(task *models.Task) error {
	return collectErr(func(v *violations) { validateTaskLifecycle(v, task) })
}

// validateTaskLifecycle checks each receipt and each of its fields on its
// own, so one malformed receipt does not hide another.
func validateTaskLifecycle(v *violations, task *models.Task) {
	if task == nil || task.Lifecycle == nil {
		return
	}
	lifecycle := task.Lifecycle
	fail := func(reason string) { v.add(fmt.Errorf("task %s lifecycle: %s", task.ID, reason)) }
	if lifecycle.CompletionSequence > lifecycle.Revision {
		fail("completion sequence exceeds revision")
	}
	if len(lifecycle.Receipts) > models.LifecycleReceiptsPerTask {
		fail("too many retained receipts")
	}
	counts := make(map[string]int)
	keys := make(map[models.LifecycleIdentity]bool)
	var previous uint64
	for _, receipt := range lifecycle.Receipts {
		// Receipts are pruned to a window, so an index is not a stable owner;
		// the sequence is, and it is what a duplicate or disorder would name.
		v.within(fmt.Sprintf("task %s lifecycle receipt %d", task.ID, receipt.Sequence), func(v *violations) {
			fail := func(reason string) { v.add(fmt.Errorf("task %s lifecycle: %s", task.ID, reason)) }
			validateLifecycleReceipt(receipt, lifecycle, previous, counts, keys, fail)
		})
		previous = receipt.Sequence
	}
	if lifecycle.Preparation != nil {
		v.within(fmt.Sprintf("task %s lifecycle preparation", task.ID), func(v *violations) {
			fail := func(reason string) { v.add(fmt.Errorf("task %s lifecycle: %s", task.ID, reason)) }
			preparation := lifecycle.Preparation
			validateLifecycleIdentity(preparation.LifecycleIdentity, fail)
			if !validLifecycleDigest(preparation.Boundary, 64) {
				fail("invalid preparation boundary")
			}
			if err := validateLifecycleSize(preparation); err != nil {
				fail(err.Error())
			}
		})
	}
}

func validateLifecycleReceipt(receipt models.LifecycleReceipt, lifecycle *models.TaskLifecycle, previous uint64, counts map[string]int, keys map[models.LifecycleIdentity]bool, fail func(string)) {
	validateLifecycleIdentity(receipt.LifecycleIdentity, fail)
	for _, check := range []struct {
		name   string
		broken bool
	}{
		{"zero", receipt.Sequence == 0},
		{"not increasing", receipt.Sequence <= previous},
		{"beyond completion", receipt.Sequence > lifecycle.CompletionSequence},
	} {
		if check.broken {
			fail("receipt sequences must increase within the completion sequence (" + check.name + ")")
		}
	}
	if lifecycle.CompletionSequence >= models.LifecycleReceiptsPerTask && receipt.Sequence <= lifecycle.CompletionSequence-models.LifecycleReceiptsPerTask {
		fail("receipt is outside the latest completion window")
	}
	counts[receipt.Operation]++
	if counts[receipt.Operation] > models.LifecycleReceiptsPerOperation {
		fail("too many receipts for one operation")
	}
	key := receipt.LifecycleIdentity
	key.PayloadDigest = ""
	if keys[key] {
		fail("duplicate receipt identity")
	}
	keys[key] = true
	if !validLifecycleDigest(receipt.TransitionID, 64) {
		fail("invalid receipt transition_id")
	}
	validateLifecycleProjection(receipt.Operation, receipt.Projection, fail)
	if err := validateLifecycleSize(receipt); err != nil {
		fail(err.Error())
	}
}

func validateLifecycleIdentity(identity models.LifecycleIdentity, fail func(string)) {
	if !models.IsLifecycleOperation(identity.Operation) {
		fail("unknown operation")
	}
	if !validLifecycleIdentifier(identity.Actor) {
		fail("invalid actor identifier")
	}
	if identity.RequestID != "" && !validLifecycleIdentifier(identity.RequestID) {
		fail("invalid request_id")
	}
	if !validLifecycleDigest(identity.ExpectedTransition, 64) {
		fail("invalid expected_transition")
	}
	if !validLifecycleDigest(identity.PayloadDigest, 64) {
		fail("invalid payload_digest")
	}
	if identity.GenerationDigest != "" && !validLifecycleDigest(identity.GenerationDigest, 64) {
		fail("invalid generation_digest")
	}
}

func validLifecycleIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= models.LifecycleIdentifierMaxBytes && lifecycleIdentifierPattern.MatchString(value)
}

func validLifecycleDigest(value string, length int) bool {
	if len(value) != length {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func validateLifecycleProjection(operation string, projection models.LifecycleProjection, fail func(string)) {
	commits := []struct{ field, value string }{
		{"input_commit", projection.InputCommit},
		{"base_commit", projection.BaseCommit},
		{"merge_commit", projection.MergeCommit},
	}
	// Reviewer assignment historically accepts stored identifiers for tasks
	// without a worktree. Retain that exact bounded value for replay; it is
	// neither a submitted SHA nor proof that Git resolved the identifier.
	if operation == "claim-reviewer-task" && projection.ReviewCommit != "" {
		if !validLifecycleIdentifier(projection.ReviewCommit) {
			fail("invalid reviewer claim projection review_commit identifier")
		}
	} else {
		commits = append(commits, struct{ field, value string }{"review_commit", projection.ReviewCommit})
	}
	for _, commit := range commits {
		if commit.value != "" && !validLifecycleDigest(commit.value, 40) && !validLifecycleDigest(commit.value, 64) {
			fail(fmt.Sprintf("projection commit must be a full lowercase Git object ID (%s)", commit.field))
		}
	}
	if projection.LeaseExpires != "" {
		if _, err := time.Parse(time.RFC3339Nano, projection.LeaseExpires); err != nil {
			fail("invalid projection lease_expires")
		}
	}
	if len(projection.SourceStatus) > models.LifecycleIdentifierMaxBytes {
		fail("projection source_status is too long")
	}
	if projection.Attempt < 0 {
		fail("negative projection counter (attempt)")
	}
	if projection.Iteration < 0 {
		fail("negative projection counter (iteration)")
	}
	if projection.Verdict != "" && projection.Verdict != "APPROVED" && projection.Verdict != "REJECTED" {
		fail("invalid projection verdict")
	}
}

func validateLifecycleSize(value any) error {
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("lifecycle metadata cannot be encoded")
	}
	if len(encoded) > models.LifecycleProjectionMaxBytes {
		return fmt.Errorf("receipt or preparation exceeds 1 KiB")
	}
	return nil
}
