package errors

import (
	"errors"
	"fmt"
)

// Exit codes used by liza commands
const (
	ExitSuccess  = 0  // Success
	ExitError    = 1  // General error
	ExitLock     = 2  // Lock timeout
	ExitNotFound = 3  // Entity or field not found
	ExitDegraded = 4  // init/setup completed with a check that is not ok (specs/protocols/outcome-checks.md)
	ExitRestart  = 42 // Agent restart request
)

// NotFoundError represents an error where an entity or field was not found
type NotFoundError struct {
	Entity string // "task", "agent", "config"
	ID     string // optional: "task-42", "orchestrator-1"
	Field  string // optional: field name
}

func (e *NotFoundError) Error() string {
	base := e.Entity
	if e.ID != "" {
		base += " " + e.ID
	}
	if e.Field != "" {
		return fmt.Sprintf("%s field '%s' not found", base, e.Field)
	}
	if e.ID != "" {
		return fmt.Sprintf("%s not found: %s", e.Entity, e.ID)
	}
	return fmt.Sprintf("%s not found", e.Entity)
}

// IsNotFound checks if an error is a NotFoundError.
// Supports wrapped errors (e.g. from bb.Modify).
func IsNotFound(err error) bool {
	var nfe *NotFoundError
	return errors.As(err, &nfe)
}

// AgentCollisionError is returned when an agent ID is already registered
// with a valid lease.
type AgentCollisionError struct {
	AgentID string
}

func (e *AgentCollisionError) Error() string {
	return fmt.Sprintf("agent ID collision: %s already registered with valid lease", e.AgentID)
}

// IsAgentCollision checks if an error is an AgentCollisionError.
// Supports wrapped errors (e.g. from bb.Modify).
func IsAgentCollision(err error) bool {
	var ace *AgentCollisionError
	return errors.As(err, &ace)
}

// ValidationError represents a client input validation failure (e.g. missing
// or invalid field values). Mapped to HTTP 400 by the API layer.
type ValidationError struct {
	Message string
	Err     error
}

func (e *ValidationError) Error() string { return e.Message }

func (e *ValidationError) Unwrap() error { return e.Err }

// CLIInputError represents a command-line input or precondition failure whose
// message is safe to expose in structured CLI output.
type CLIInputError struct {
	Message string
	Err     error
}

func (e *CLIInputError) Error() string { return e.Message }

func (e *CLIInputError) Unwrap() error { return e.Err }

// ProjectRootError indicates that Liza could not resolve the project root from
// the current execution context.
type ProjectRootError struct {
	Message      string
	Operation    string
	CurrentDir   string
	ExpectedRoot string
	Err          error
}

func (e *ProjectRootError) Error() string {
	if e.Message != "" {
		if e.Operation != "" {
			return fmt.Sprintf("%s: %s", e.Operation, e.Message)
		}
		return e.Message
	}
	if e.CurrentDir != "" && e.ExpectedRoot != "" {
		if e.Operation != "" {
			return fmt.Sprintf("%s: must be run from project root %s (current directory: %s)", e.Operation, e.ExpectedRoot, e.CurrentDir)
		}
		return fmt.Sprintf("must be run from project root %s (current directory: %s)", e.ExpectedRoot, e.CurrentDir)
	}
	if e.Operation != "" {
		return fmt.Sprintf("%s: failed to detect project root", e.Operation)
	}
	return "failed to detect project root"
}

func (e *ProjectRootError) Unwrap() error { return e.Err }

func (e *ProjectRootError) SafeDetails() map[string]any {
	details := map[string]any{}
	if e.Operation != "" {
		details["operation"] = e.Operation
	}
	if e.CurrentDir != "" {
		details["current_dir"] = e.CurrentDir
	}
	if e.ExpectedRoot != "" {
		details["project_root"] = e.ExpectedRoot
	}
	if e.Message != "" {
		details["message"] = e.Message
	}
	return details
}

// PipelineConfigError indicates that the frozen pipeline config could not be
// loaded or parsed for an operation that depends on role/status metadata.
type PipelineConfigError struct {
	Operation string
	Err       error
}

func (e *PipelineConfigError) Error() string {
	if e.Operation != "" {
		return fmt.Sprintf("%s: failed to load pipeline config", e.Operation)
	}
	return "failed to load pipeline config"
}

func (e *PipelineConfigError) Unwrap() error { return e.Err }

func (e *PipelineConfigError) SafeDetails() map[string]any {
	details := map[string]any{}
	if e.Operation != "" {
		details["operation"] = e.Operation
	}
	return details
}

// PermissionError indicates that an agent identity is not authorized for an
// operation under the active pipeline role rules.
type PermissionError struct {
	Operation string
	AgentID   string
	Role      string
	Reason    string
	Err       error
}

func (e *PermissionError) Error() string {
	if e.Reason != "" && e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Reason, e.Err)
	}
	if e.Reason != "" {
		return e.Reason
	}
	return "permission denied"
}

func (e *PermissionError) Unwrap() error { return e.Err }

func (e *PermissionError) SafeDetails() map[string]any {
	details := map[string]any{}
	if e.Operation != "" {
		details["operation"] = e.Operation
	}
	if e.AgentID != "" {
		details["agent_id"] = e.AgentID
	}
	if e.Role != "" {
		details["role"] = e.Role
	}
	return details
}

// StateSchemaError indicates that state.yaml could not be read as a valid Liza
// state document, before command-specific preconditions can be evaluated.
type StateSchemaError struct {
	Operation string
	Err       error
}

func (e *StateSchemaError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("state schema validation failed: %v", e.Err)
	}
	return "state schema validation failed"
}

func (e *StateSchemaError) Unwrap() error { return e.Err }

func (e *StateSchemaError) SafeDetails() map[string]any {
	details := map[string]any{}
	if e.Operation != "" {
		details["operation"] = e.Operation
	}
	return details
}

// WorktreeContextError indicates that the task's expected worktree context is
// missing or inconsistent for the requested operation.
type WorktreeContextError struct {
	Operation string
	TaskID    string
	Reason    string
	Err       error
}

func (e *WorktreeContextError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return "worktree context invalid"
}

func (e *WorktreeContextError) Unwrap() error { return e.Err }

func (e *WorktreeContextError) SafeDetails() map[string]any {
	details := map[string]any{}
	if e.Operation != "" {
		details["operation"] = e.Operation
	}
	if e.TaskID != "" {
		details["task_id"] = e.TaskID
	}
	return details
}
