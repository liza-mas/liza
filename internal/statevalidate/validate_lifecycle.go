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
	if task == nil || task.Lifecycle == nil {
		return nil
	}
	lifecycle := task.Lifecycle
	fail := func(reason string) error { return fmt.Errorf("task %s lifecycle: %s", task.ID, reason) }
	if lifecycle.CompletionSequence > lifecycle.Revision {
		return fail("completion sequence exceeds revision")
	}
	if len(lifecycle.Receipts) > models.LifecycleReceiptsPerTask {
		return fail("too many retained receipts")
	}
	counts := make(map[string]int)
	keys := make(map[models.LifecycleIdentity]bool)
	var previous uint64
	for _, receipt := range lifecycle.Receipts {
		if err := validateLifecycleIdentity(receipt.LifecycleIdentity); err != nil {
			return fail(err.Error())
		}
		if receipt.Sequence == 0 || receipt.Sequence <= previous || receipt.Sequence > lifecycle.CompletionSequence {
			return fail("receipt sequences must increase within the completion sequence")
		}
		if lifecycle.CompletionSequence >= models.LifecycleReceiptsPerTask && receipt.Sequence <= lifecycle.CompletionSequence-models.LifecycleReceiptsPerTask {
			return fail("receipt is outside the latest completion window")
		}
		previous = receipt.Sequence
		counts[receipt.Operation]++
		if counts[receipt.Operation] > models.LifecycleReceiptsPerOperation {
			return fail("too many receipts for one operation")
		}
		key := receipt.LifecycleIdentity
		key.PayloadDigest = ""
		if keys[key] {
			return fail("duplicate receipt identity")
		}
		keys[key] = true
		if !validLifecycleDigest(receipt.TransitionID, 64) {
			return fail("invalid receipt transition_id")
		}
		if err := validateLifecycleProjection(receipt.Operation, receipt.Projection); err != nil {
			return fail(err.Error())
		}
		if err := validateLifecycleSize(receipt); err != nil {
			return fail(err.Error())
		}
	}
	if lifecycle.Preparation != nil {
		preparation := lifecycle.Preparation
		if err := validateLifecycleIdentity(preparation.LifecycleIdentity); err != nil {
			return fail(err.Error())
		}
		if !validLifecycleDigest(preparation.Boundary, 64) {
			return fail("invalid preparation boundary")
		}
		if err := validateLifecycleSize(preparation); err != nil {
			return fail(err.Error())
		}
	}
	return nil
}

func validateLifecycleIdentity(identity models.LifecycleIdentity) error {
	if !models.IsLifecycleOperation(identity.Operation) {
		return fmt.Errorf("unknown operation")
	}
	if !validLifecycleIdentifier(identity.Actor) {
		return fmt.Errorf("invalid actor identifier")
	}
	if identity.RequestID != "" && !validLifecycleIdentifier(identity.RequestID) {
		return fmt.Errorf("invalid request_id")
	}
	if !validLifecycleDigest(identity.ExpectedTransition, 64) {
		return fmt.Errorf("invalid expected_transition")
	}
	if !validLifecycleDigest(identity.PayloadDigest, 64) {
		return fmt.Errorf("invalid payload_digest")
	}
	if identity.GenerationDigest != "" && !validLifecycleDigest(identity.GenerationDigest, 64) {
		return fmt.Errorf("invalid generation_digest")
	}
	return nil
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

func validateLifecycleProjection(operation string, projection models.LifecycleProjection) error {
	commits := []string{projection.InputCommit, projection.BaseCommit, projection.MergeCommit}
	// Reviewer assignment historically accepts stored identifiers for tasks
	// without a worktree. Retain that exact bounded value for replay; it is
	// neither a submitted SHA nor proof that Git resolved the identifier.
	if operation == "claim-reviewer-task" && projection.ReviewCommit != "" {
		if !validLifecycleIdentifier(projection.ReviewCommit) {
			return fmt.Errorf("invalid reviewer claim projection review_commit identifier")
		}
	} else {
		commits = append(commits, projection.ReviewCommit)
	}
	for _, commit := range commits {
		if commit != "" && !validLifecycleDigest(commit, 40) && !validLifecycleDigest(commit, 64) {
			return fmt.Errorf("projection commit must be a full lowercase Git object ID")
		}
	}
	if projection.LeaseExpires != "" {
		if _, err := time.Parse(time.RFC3339Nano, projection.LeaseExpires); err != nil {
			return fmt.Errorf("invalid projection lease_expires")
		}
	}
	if len(projection.SourceStatus) > models.LifecycleIdentifierMaxBytes {
		return fmt.Errorf("projection source_status is too long")
	}
	if projection.Attempt < 0 || projection.Iteration < 0 {
		return fmt.Errorf("negative projection counter")
	}
	if projection.Verdict != "" && projection.Verdict != "APPROVED" && projection.Verdict != "REJECTED" {
		return fmt.Errorf("invalid projection verdict")
	}
	return nil
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
