package jsonout

import (
	"fmt"
	"testing"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/statevalidate"
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

func TestClassifyError_StructuredOperationalCategories(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
		wantMsg  string
	}{
		{
			name:     "project root",
			err:      &errors.ProjectRootError{Operation: "claim-task", Err: fmt.Errorf("not a git repository")},
			wantCode: "project_root",
			wantMsg:  "claim-task: failed to detect project root",
		},
		{
			name:     "pipeline config",
			err:      &errors.PipelineConfigError{Operation: "claim-task", Err: fmt.Errorf("invalid yaml")},
			wantCode: "pipeline_config",
			wantMsg:  "claim-task: failed to load pipeline config",
		},
		{
			name: "permission",
			err: &errors.PermissionError{
				Operation: "claim-task",
				AgentID:   "orchestrator-1",
				Role:      "orchestrator",
				Reason:    `operation "claim-task" not allowed for role "orchestrator" (agent orchestrator-1)`,
			},
			wantCode: "permission_denied",
			wantMsg:  `operation "claim-task" not allowed for role "orchestrator" (agent orchestrator-1)`,
		},
		{
			name:     "state schema",
			err:      &errors.StateSchemaError{Operation: "validate", Err: fmt.Errorf("failed to parse state.yaml")},
			wantCode: "state_schema",
			wantMsg:  "state schema validation failed: failed to parse state.yaml",
		},
		{
			name: "worktree context",
			err: &errors.WorktreeContextError{
				Operation: "submit-for-review",
				TaskID:    "task-1",
				Reason:    "worktree directory does not exist",
			},
			wantCode: "worktree_context",
			wantMsg:  "worktree directory does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := ClassifyError(tt.err)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if msg != tt.wantMsg {
				t.Errorf("message = %q, want %q", msg, tt.wantMsg)
			}
		})
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

func TestClassifyError_UnblockRebaseConflictError(t *testing.T) {
	err := &ops.UnblockRebaseConflictError{
		TaskID:    "task-1",
		TargetRef: "integration",
		TargetSHA: "abc123",
		Cause:     fmt.Errorf("rebase conflict"),
	}

	code, msg := ClassifyError(err)
	if code != "unblock_rebase_conflict" {
		t.Errorf("code = %q, want %q", code, "unblock_rebase_conflict")
	}
	if msg != "unblock rebase conflict: task remains BLOCKED with repair_request" {
		t.Errorf("message = %q", msg)
	}
	details := ErrorDetails(err)
	if details["task_status"] != "BLOCKED" {
		t.Errorf("task_status = %v, want BLOCKED", details["task_status"])
	}
	if details["repair_request"] != true {
		t.Errorf("repair_request = %v, want true", details["repair_request"])
	}
}

func TestClassifyError_CLIInputError(t *testing.T) {
	err := &errors.CLIInputError{
		Message: "reading tasks file: open missing.json: no such file or directory",
		Err:     fmt.Errorf("open missing.json: no such file or directory"),
	}
	code, msg := ClassifyError(err)
	if code != "validation" {
		t.Errorf("code = %q, want %q", code, "validation")
	}
	if msg != err.Message {
		t.Errorf("message = %q, want %q", msg, err.Message)
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

func TestErrorDetails_UnwrapsValidationError(t *testing.T) {
	inputErr := &errors.ValidationError{
		Message: "plan_ref file not found: specs/plans/missing.md (task: task-1)",
		Err: &statevalidate.ArtifactRefError{
			Field:  "plan_ref",
			Value:  "specs/plans/missing.md",
			TaskID: "task-1",
			Cause:  "file_not_found",
		},
	}

	details := ErrorDetails(inputErr)
	if details["field"] != "plan_ref" {
		t.Errorf("field = %v, want plan_ref", details["field"])
	}
	if details["task_id"] != "task-1" {
		t.Errorf("task_id = %v, want task-1", details["task_id"])
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

func TestClassifyError_OperationalErrorWithCode(t *testing.T) {
	err := &ops.OperationalError{
		Code:    "git_operation",
		Phase:   "resolve-integration-head",
		Message: "failed to resolve integration branch HEAD",
		Details: map[string]any{
			"operation": "submit-for-review",
		},
		Err: fmt.Errorf("exit status 128"),
	}

	code, msg := ClassifyError(err)
	if code != "git_operation" {
		t.Errorf("code = %q, want %q", code, "git_operation")
	}
	if msg != "failed to resolve integration branch HEAD" {
		t.Errorf("message = %q, want actionable operational message", msg)
	}

	details := ErrorDetails(err)
	if details["phase"] != "resolve-integration-head" {
		t.Errorf("phase = %v, want resolve-integration-head", details["phase"])
	}
	if details["operation"] != "submit-for-review" {
		t.Errorf("operation = %v, want submit-for-review", details["operation"])
	}
}

func TestClassifyError_OperationalErrorPreservesTypedInnerError(t *testing.T) {
	err := &ops.OperationalError{
		Code:    "state_write",
		Phase:   "write-state",
		Message: "failed to submit task for review",
		Err:     &ops.PreconditionError{Reason: "task task-1 is not IMPLEMENTING"},
	}

	code, msg := ClassifyError(err)
	if code != "validation" {
		t.Errorf("code = %q, want validation", code)
	}
	if msg != "task task-1 is not IMPLEMENTING" {
		t.Errorf("message = %q, want wrapped precondition reason", msg)
	}
}

func TestClassifyError_OperationalErrorCodeBeatsUntypedInnerHeuristics(t *testing.T) {
	err := &ops.OperationalError{
		Code:    "state_write",
		Phase:   "write-state",
		Message: "failed to submit task for review",
		Err:     fmt.Errorf("task task-1 not found"),
	}

	code, msg := ClassifyError(err)
	if code != "state_write" {
		t.Errorf("code = %q, want state_write", code)
	}
	if msg != "failed to submit task for review" {
		t.Errorf("message = %q, want operational message", msg)
	}
}

func TestClassifyError_OperationalErrorCodeBeatsTypedInnerErrors(t *testing.T) {
	err := &ops.OperationalError{
		Code:    "state_write",
		Phase:   "write-state",
		Message: "failed to submit task for review",
		Err:     filelock.NewLockTimeout(fmt.Errorf("lock unavailable after 10s")),
	}

	code, msg := ClassifyError(err)
	if code != "state_write" {
		t.Errorf("code = %q, want state_write", code)
	}
	if msg != "failed to submit task for review" {
		t.Errorf("message = %q, want operational message", msg)
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

func TestClassifyError_FileLockErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
		wantMsg  string
	}{
		{
			name:     "timeout",
			err:      filelock.NewLockTimeout(fmt.Errorf("lock unavailable after 10s")),
			wantCode: "lock_timeout",
			wantMsg:  "lock acquisition timed out",
		},
		{
			name: "filesystem",
			err: &filelock.LockError{
				Type:    filelock.LockErrorFilesystem,
				Message: "filesystem error",
				Err:     fmt.Errorf("open state.yaml.lock: no such file or directory"),
			},
			wantCode: "filesystem",
			wantMsg:  "lock filesystem error",
		},
		{
			name:     "stale",
			err:      filelock.NewLockStale(12345),
			wantCode: "lock_stale",
			wantMsg:  "lock owner metadata is stale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := ClassifyError(fmt.Errorf("submit verdict: %w", tt.err))
			if code != tt.wantCode {
				t.Fatalf("code = %q, want %q", code, tt.wantCode)
			}
			if msg != tt.wantMsg {
				t.Fatalf("message = %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestClassifyError_RaceCondition(t *testing.T) {
	tests := []string{
		"race condition detected",
		"state changed concurrently",
		"phase 3 failed: sentinel replaced: expected $transitioning, got coder-2 (concurrent modification)",
	}
	for _, s := range tests {
		err := fmt.Errorf("%s", s)
		code, msg := ClassifyError(err)
		if code != "race_condition" {
			t.Errorf("input=%q: code = %q, want %q", s, code, "race_condition")
		}
		if msg != "state changed concurrently, requery current state" {
			t.Errorf("input=%q: message = %q, want %q", s, msg, "state changed concurrently, requery current state")
		}
	}
}

func TestClassifyError_ValidationPatterns(t *testing.T) {
	patterns := []string{
		"task is not IMPLEMENTING",
		"task is not REVIEWING",
		"task is not READY_FOR_REVIEW",
		"task is not CODE_READY_FOR_REVIEW",
		"task is not CODE_TO_REVIEW",
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

func TestClassifyError_OperationalErrorPreservesTransientCodes(t *testing.T) {
	tests := []struct {
		name     string
		inner    error
		wantCode string
		wantMsg  string
	}{
		{
			name:     "lock timeout surfaces through OperationalError",
			inner:    filelock.NewLockTimeout(fmt.Errorf("lock unavailable after 10s")),
			wantCode: "lock_timeout",
			wantMsg:  "lock acquisition timed out",
		},
		{
			name:     "race condition surfaces through OperationalError",
			inner:    fmt.Errorf("state changed concurrently"),
			wantCode: "race_condition",
			wantMsg:  "state changed concurrently, requery current state",
		},
		{
			name:     "generic inner error falls back to OperationalError message",
			inner:    fmt.Errorf("disk full"),
			wantCode: "internal",
			wantMsg:  "failed to read state",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ops.OperationalError{Message: "failed to read state", Err: tt.inner}
			code, msg := ClassifyError(err)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if msg != tt.wantMsg {
				t.Errorf("message = %q, want %q", msg, tt.wantMsg)
			}
		})
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
