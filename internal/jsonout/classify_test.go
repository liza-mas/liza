package jsonout

import (
	"fmt"
	"testing"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/ops"
)

func TestClassifyError_NotFoundError(t *testing.T) {
	err := &errors.NotFoundError{Entity: "task", ID: "T1"}
	code, msg := ClassifyError(err)
	if code != "not_found" {
		t.Errorf("code = %q, want %q", code, "not_found")
	}
	if msg != "resource not found" {
		t.Errorf("message = %q, want %q", msg, "resource not found")
	}
}

func TestClassifyError_PreconditionError(t *testing.T) {
	err := &ops.PreconditionError{Reason: "task is not IMPLEMENTING"}
	code, msg := ClassifyError(err)
	if code != "validation" {
		t.Errorf("code = %q, want %q", code, "validation")
	}
	if msg != "task is not IMPLEMENTING" {
		t.Errorf("message = %q, want %q", msg, "task is not IMPLEMENTING")
	}
}

func TestClassifyError_PostWriteValidationError(t *testing.T) {
	err := &ops.PostWriteValidationError{Err: fmt.Errorf("invariant broken")}
	code, msg := ClassifyError(err)
	if code != "validation" {
		t.Errorf("code = %q, want %q", code, "validation")
	}
	if msg != "validation failed: precondition not met" {
		t.Errorf("message = %q, want %q", msg, "validation failed: precondition not met")
	}
}

func TestClassifyError_OperationalError(t *testing.T) {
	err := &ops.OperationalError{Message: "git checkout failed", Err: fmt.Errorf("exit 1")}
	code, msg := ClassifyError(err)
	if code != "internal" {
		t.Errorf("code = %q, want %q", code, "internal")
	}
	if msg != "git checkout failed" {
		t.Errorf("message = %q, want %q", msg, "git checkout failed")
	}
}

func TestClassifyError_IntegrationFailedError(t *testing.T) {
	err := &ops.IntegrationFailedError{Reason: "merge conflict"}
	code, msg := ClassifyError(err)
	if code != "validation" {
		t.Errorf("code = %q, want %q", code, "validation")
	}
	want := "integration failed: merge conflict"
	if msg != want {
		t.Errorf("message = %q, want %q", msg, want)
	}
}

func TestClassifyError_LockTimeout(t *testing.T) {
	tests := []string{
		"lock acquisition timeout",
		"failed to acquire lock: timed out",
	}
	for _, s := range tests {
		err := fmt.Errorf("%s", s)
		code, msg := ClassifyError(err)
		if code != "lock_timeout" {
			t.Errorf("input=%q: code = %q, want %q", s, code, "lock_timeout")
		}
		if msg != "lock acquisition timed out" {
			t.Errorf("input=%q: message = %q, want %q", s, msg, "lock acquisition timed out")
		}
	}
}

func TestClassifyError_RaceCondition(t *testing.T) {
	tests := []string{
		"race condition detected",
		"state changed concurrently",
	}
	for _, s := range tests {
		err := fmt.Errorf("%s", s)
		code, msg := ClassifyError(err)
		if code != "race_condition" {
			t.Errorf("input=%q: code = %q, want %q", s, code, "race_condition")
		}
		if msg != "state changed concurrently, retry" {
			t.Errorf("input=%q: message = %q, want %q", s, msg, "state changed concurrently, retry")
		}
	}
}

func TestClassifyError_ValidationPatterns(t *testing.T) {
	patterns := []string{
		"task is not IMPLEMENTING",
		"task is not REVIEWING",
		"task is not READY_FOR_REVIEW",
		"task is not CODE_READY_FOR_REVIEW",
		"task is not CODE_APPROVED",
		"task is not APPROVED",
		"field must be non-empty",
		"agent_id is required",
		"invalid task ID format",
		"validation failed: bad input",
		"description must include rationale",
		"field mandatory for this operation",
	}
	for _, s := range patterns {
		err := fmt.Errorf("%s", s)
		code, msg := ClassifyError(err)
		if code != "validation" {
			t.Errorf("input=%q: code = %q, want %q", s, code, "validation")
		}
		if msg != "validation failed" {
			t.Errorf("input=%q: message = %q, want %q", s, msg, "validation failed")
		}
	}
}

func TestClassifyError_DefaultInternal(t *testing.T) {
	err := fmt.Errorf("something completely unexpected happened")
	code, msg := ClassifyError(err)
	if code != "internal" {
		t.Errorf("code = %q, want %q", code, "internal")
	}
	if msg != "internal error" {
		t.Errorf("message = %q, want %q", msg, "internal error")
	}
}

func TestClassifyError_NoRawLeak(t *testing.T) {
	// Untyped errors must never leak err.Error() in the message.
	untypedErrors := []error{
		fmt.Errorf("something completely unexpected happened"),
		fmt.Errorf("segfault at 0xdeadbeef"),
		fmt.Errorf("panic in goroutine 42"),
	}
	for _, err := range untypedErrors {
		_, msg := ClassifyError(err)
		if msg == err.Error() {
			t.Errorf("raw error leaked: message = %q equals err.Error()", msg)
		}
	}
}
