package jsonout

import (
	"errors"
	"fmt"
	"strings"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
)

// errorCode is a --json error code assigned by this package. It is converted
// to string at ClassifyError's return, so ErrorDetail.Code stays a string.
// Codes supplied by an operational failure are an open set and pass through
// unchanged; a lifecycle outcome's code is its lower-cased name.
type errorCode string

const (
	codeNotFound          errorCode = "not_found"
	codeRaceCondition     errorCode = "race_condition"
	codeValidation        errorCode = "validation"
	codePermissionDenied  errorCode = "permission_denied"
	codeStaleCaller       errorCode = "stale_caller"
	codeProjectRoot       errorCode = "project_root"
	codePipelineConfig    errorCode = "pipeline_config"
	codeStateSchema       errorCode = "state_schema"
	codeWorktreeContext   errorCode = "worktree_context"
	codeUnblockConflict   errorCode = "unblock_rebase_conflict"
	codeLockTimeout       errorCode = "lock_timeout"
	codeLockStale         errorCode = "lock_stale"
	codeFilesystem        errorCode = "filesystem"
	codeInternal          errorCode = "internal"
	messageInternal                 = "internal error"
	messagePostWriteFails           = "validation failed: precondition not met"
)

// classificationSlot classifies the error chain when it holds the slot's
// error kind, and reports ok=false otherwise.
type classificationSlot func(err error) (code errorCode, message string, ok bool)

// classificationSlots are evaluated in order; the first slot whose error kind
// appears anywhere in the chain (also through errors.Join) decides. The order
// is the frozen precedence of the --json classification: an outer wrapper does
// not outrank an inner error of an earlier slot.
var classificationSlots = []classificationSlot{
	classifyLifecycle,
	classifyStaleCaller,
	classifyNotFoundError,
	classifyProjectRoot,
	classifyPipelineConfig,
	classifyPermission,
	classifyStateSchema,
	classifyWorktreeContext,
	classifyValidation,
	classifyCLIInput,
	classifyPostWrite,
	classifyPrecondition,
	classifyUnblockConflict,
	classifyIntegration,
	classifyOperational,
}

// classifyLifecycle: the operation's observed outcome outranks a wrapped
// legacy precondition. In particular, an already-transitioned task is not
// malformed input.
func classifyLifecycle(err error) (errorCode, string, bool) {
	var lifecycle lizaerrors.LifecycleOutcomeError
	if !errors.As(err, &lifecycle) {
		return "", "", false
	}
	outcome := lifecycle.LifecycleOutcomeName()
	code := errorCode(strings.ToLower(outcome))
	switch outcome {
	case models.LifecycleInvalidInput:
		code = codeValidation
	case models.LifecycleForbidden:
		code = codePermissionDenied
	}
	return code, lifecycle.Error(), true
}

func classifyStaleCaller(err error) (errorCode, string, bool) {
	var stale lizaerrors.StaleCallerError
	if !errors.As(err, &stale) {
		return "", "", false
	}
	return codeStaleCaller, stale.Error(), true
}

func classifyNotFoundError(err error) (errorCode, string, bool) {
	var target *lizaerrors.NotFoundError
	if !errors.As(err, &target) {
		return "", "", false
	}
	return codeNotFound, "resource not found", true
}

func classifyProjectRoot(err error) (errorCode, string, bool) {
	var target *lizaerrors.ProjectRootError
	if !errors.As(err, &target) {
		return "", "", false
	}
	return codeProjectRoot, target.Error(), true
}

func classifyPipelineConfig(err error) (errorCode, string, bool) {
	var target *lizaerrors.PipelineConfigError
	if !errors.As(err, &target) {
		return "", "", false
	}
	return codePipelineConfig, target.Error(), true
}

func classifyPermission(err error) (errorCode, string, bool) {
	var target *lizaerrors.PermissionError
	if !errors.As(err, &target) {
		return "", "", false
	}
	return codePermissionDenied, target.Error(), true
}

func classifyStateSchema(err error) (errorCode, string, bool) {
	var target *lizaerrors.StateSchemaError
	if !errors.As(err, &target) {
		return "", "", false
	}
	return codeStateSchema, target.Error(), true
}

func classifyWorktreeContext(err error) (errorCode, string, bool) {
	var target *lizaerrors.WorktreeContextError
	if !errors.As(err, &target) {
		return "", "", false
	}
	return codeWorktreeContext, target.Error(), true
}

func classifyValidation(err error) (errorCode, string, bool) {
	var target *lizaerrors.ValidationError
	if !errors.As(err, &target) {
		return "", "", false
	}
	return codeValidation, target.Message, true
}

func classifyCLIInput(err error) (errorCode, string, bool) {
	var target *lizaerrors.CLIInputError
	if !errors.As(err, &target) {
		return "", "", false
	}
	return codeValidation, target.Message, true
}

func classifyPostWrite(err error) (errorCode, string, bool) {
	var postWrite lizaerrors.PostWriteValidationFailure
	if !errors.As(err, &postWrite) {
		return "", "", false
	}
	return codeValidation, messagePostWriteFails, true
}

func classifyPrecondition(err error) (errorCode, string, bool) {
	var precondition lizaerrors.PreconditionFailure
	if !errors.As(err, &precondition) {
		return "", "", false
	}
	return codeValidation, precondition.PreconditionReason(), true
}

func classifyUnblockConflict(err error) (errorCode, string, bool) {
	var conflict lizaerrors.UnblockRebaseConflict
	if !errors.As(err, &conflict) {
		return "", "", false
	}
	return codeUnblockConflict, "unblock rebase conflict: task remains BLOCKED with repair_request", true
}

func classifyIntegration(err error) (errorCode, string, bool) {
	var integration lizaerrors.IntegrationFailure
	if !errors.As(err, &integration) {
		return "", "", false
	}
	return codeValidation, fmt.Sprintf("integration failed: %s", integration.IntegrationFailureReason()), true
}

// classifyOperational: an explicit operation code is a trusted classification.
// Without one, transient semantics of a recognizable inner error are
// preserved before defaulting to the failure's safe message.
func classifyOperational(err error) (errorCode, string, bool) {
	var operational lizaerrors.OperationalFailure
	if !errors.As(err, &operational) {
		return "", "", false
	}
	if code := operational.OperationalCode(); code != "" {
		return errorCode(code), operational.OperationalMessage(), true
	}
	if inner := operational.OperationalCause(); inner != nil {
		if code, message, ok := classifyFileLock(inner); ok {
			return code, message, true
		}
		if code, message := classifyUntyped(inner); code != codeInternal {
			return code, message, true
		}
	}
	return codeInternal, operational.OperationalMessage(), true
}
