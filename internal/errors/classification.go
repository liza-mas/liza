package errors

// Classification capabilities let a presentation adapter classify errors of
// the application layer without importing it. Each capability is one branch of
// the --json classification, exposes exactly the facts that branch reads, and
// is implemented only by the application error type that branch matches. The
// adapter evaluates them with errors.As in its own fixed order, so branch
// precedence stays with the adapter and never depends on chain order.
//
// Each method name is unique to its capability; no other type implements or
// embeds one.

// LifecycleOutcomeError is an operation's observed lifecycle outcome. Error is
// its safe, agent-facing text.
type LifecycleOutcomeError interface {
	error
	LifecycleOutcomeName() string
}

// StaleCallerError reports a caller that no longer owns its registration
// generation. Error is its safe, agent-facing text.
type StaleCallerError interface {
	error
	StaleCallerMarker()
}

// PostWriteValidationFailure reports a mutation that persisted but failed
// state validation immediately afterward.
type PostWriteValidationFailure interface {
	error
	PostWriteValidationMarker()
}

// PreconditionFailure reports a failed precondition whose reason is
// client-facing.
type PreconditionFailure interface {
	error
	PreconditionReason() string
}

// UnblockRebaseConflict reports an unblock whose preserved worktree could not
// be rebased.
type UnblockRebaseConflict interface {
	error
	UnblockRebaseConflictMarker()
}

// IntegrationFailure reports a failed merge or integration test run.
type IntegrationFailure interface {
	error
	IntegrationFailureReason() string
}

// OperationalFailure reports an infrastructure failure: an optional explicit
// code, a message safe to expose, and the wrapped cause.
type OperationalFailure interface {
	error
	OperationalCode() string
	OperationalMessage() string
	OperationalCause() error
}
