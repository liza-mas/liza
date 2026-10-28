package present

import (
	"bytes"
	"errors"
	"strings"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

func envelopeCase(name string) ([]byte, bool) {
	var err error
	switch name {
	case "lifecycle-invalid-input", "lifecycle-forbidden", "lifecycle-conflict":
		outcome := strings.ToUpper(strings.TrimPrefix(name, "lifecycle-"))
		outcome = strings.ReplaceAll(outcome, "-", "_")
		err = &ops.LifecycleError{Outcome: models.LifecycleOutcome{Operation: "fixture", TaskID: "task-executing", Outcome: outcome, SafeAction: "stop", Effects: "none", TaskStatus: "IMPLEMENTING_CODE"}, Err: &ops.PreconditionError{Reason: "fixture precondition"}}
	case "authority":
		err = &ops.AgentAuthorityError{AgentID: "coder-1", LosingGeneration: "old", CurrentGeneration: "new"}
	case "not-found":
		err = &lizaerrors.NotFoundError{Entity: "task", ID: "task-missing"}
	case "project-root":
		err = &lizaerrors.ProjectRootError{Operation: "fixture", Message: "fixture root unavailable", CurrentDir: "/fixture/current", ExpectedRoot: "/fixture/project"}
	case "pipeline-config":
		err = &lizaerrors.PipelineConfigError{Operation: "fixture", Err: errors.New("fixture pipeline")}
	case "permission":
		err = &lizaerrors.PermissionError{Operation: "fixture", AgentID: "coder-1", Role: "coder", Reason: "fixture permission denied"}
	case "state-schema":
		err = &lizaerrors.StateSchemaError{Operation: "fixture", Err: errors.New("fixture schema")}
	case "worktree-context":
		err = &lizaerrors.WorktreeContextError{Operation: "fixture", TaskID: "task-executing", Reason: "fixture worktree absent"}
	case "validation":
		err = &lizaerrors.ValidationError{Message: "fixture validation"}
	case "cli-input":
		err = &lizaerrors.CLIInputError{Message: "fixture CLI input"}
	case "post-write":
		err = &ops.PostWriteValidationError{Err: errors.New("fixture write")}
	case "precondition":
		err = &ops.PreconditionError{Reason: "fixture precondition", Details: map[string]any{"field": "fixture"}}
	case "unblock-rebase-conflict":
		err = &ops.UnblockRebaseConflictError{TaskID: "task-blocked", TargetRef: "integration", TargetSHA: "1111111111111111111111111111111111111111", Cause: errors.New("fixture conflict")}
	case "integration-failed":
		err = &ops.IntegrationFailedError{Reason: "fixture integration"}
	case "operational-code":
		err = &ops.OperationalError{Code: "fixture_operation", Message: "fixture operation", Phase: "fixture", Details: map[string]any{"task_id": "task-executing"}}
	case "operational-lock":
		err = &ops.OperationalError{Message: "fixture operation", Err: &filelock.LockError{Type: filelock.LockErrorTimeout}}
	case "operational-untyped":
		err = &ops.OperationalError{Message: "fixture operation", Err: errors.New("not found")}
	case "operational-internal":
		err = &ops.OperationalError{Message: "fixture safe message", Err: errors.New("unclassified fixture cause")}
	case "operational-no-inner":
		err = &ops.OperationalError{Message: "fixture safe message"}
	case "lock-timeout", "lock-permission", "lock-disk-full", "lock-filesystem", "lock-stale", "lock-unknown":
		types := map[string]filelock.LockErrorType{"lock-timeout": filelock.LockErrorTimeout, "lock-permission": filelock.LockErrorPermission, "lock-disk-full": filelock.LockErrorDiskFull, "lock-filesystem": filelock.LockErrorFilesystem, "lock-stale": filelock.LockErrorStale, "lock-unknown": filelock.LockErrorType(99)}
		err = &filelock.LockError{Type: types[name], Message: "fixture lock"}
	case "warning":
		var out bytes.Buffer
		_ = jsonout.WriteResult(&out, map[string]any{"fixture": "ok"}, []string{"fixture warning"}, nil)
		return out.Bytes(), true
	default:
		pattern, ok := envelopeStringPatterns[name]
		if !ok {
			return nil, false
		}
		err = errors.New(pattern)
	}
	var out bytes.Buffer
	_ = jsonout.WriteResult(&out, nil, nil, err)
	return out.Bytes(), true
}

// Every literal fallback pattern is exercised separately: changing one must
// change that case's golden, even if another pattern retains the same code.
var envelopeStringPatterns = map[string]string{
	"string-lock-timeout": "lock timeout", "string-lock-timed-out": "lock timed out",
	"string-not-found": "not found", "string-does-not-exist": "does not exist",
	"string-race-condition": "race condition", "string-changed-concurrently": "changed concurrently",
	"string-concurrent-modification": "concurrent modification", "string-sentinel-replaced": "sentinel replaced",
	"string-not-implementing": "not IMPLEMENTING", "string-not-reviewing": "not REVIEWING",
	"string-not-ready-for-review": "not READY_FOR_REVIEW", "string-not-code-ready-for-review": "not CODE_READY_FOR_REVIEW",
	"string-not-code-to-review": "not CODE_TO_REVIEW", "string-not-code-approved": "not CODE_APPROVED",
	"string-not-approved": "not APPROVED", "string-must-be": "must be", "string-is-required": "is required",
	"string-invalid-task-id": "invalid task ID", "string-validation-failed": "validation failed",
	"string-must-include": "must include", "string-mandatory": "mandatory",
	"string-internal": "unclassified fixture cause",
}
