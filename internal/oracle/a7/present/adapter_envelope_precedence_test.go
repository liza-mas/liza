package present

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// TestOracleEnvelopePrecedence pins the cross-slot precedence of
// jsonout.ClassifyError (gap G-f): the single-branch classification cases do
// not show which classification wins when one error chain, nested or joined,
// matches several branches.
func TestOracleEnvelopePrecedence(t *testing.T) {
	skipUnlessLinux(t)
	for _, name := range fixtureLines(t, "gap/precedence-cases.txt") {
		t.Run(name, func(t *testing.T) {
			err, ok := precedenceCase(name)
			if !ok {
				t.Fatalf("unknown precedence fixture %s", name)
			}
			var out bytes.Buffer
			_ = jsonout.WriteResult(&out, nil, nil, err)
			oracle.Check(t, oracle.JSON, "envelope-precedence/"+name+".json", out.Bytes())
		})
	}
}

func precedenceLifecycle(outcome string, cause error) error {
	return &ops.LifecycleError{Outcome: models.LifecycleOutcome{Operation: "fixture", TaskID: "task-executing", Outcome: outcome, SafeAction: "stop", Effects: "none", TaskStatus: "IMPLEMENTING_CODE"}, Err: cause}
}

func precedenceCase(name string) (error, bool) {
	operational := func(cause error) error {
		return &ops.OperationalError{Message: "fixture safe message", Err: cause}
	}
	coded := func(cause error) error {
		return &ops.OperationalError{Code: "fixture_operation", Message: "fixture coded message", Err: cause}
	}
	lifecycle := precedenceLifecycle("FORBIDDEN", nil)
	authority := &ops.AgentAuthorityError{AgentID: "coder-1", LosingGeneration: "old", CurrentGeneration: "new"}
	precondition := &ops.PreconditionError{Reason: "fixture precondition", Details: map[string]any{"field": "fixture"}}
	postWrite := &ops.PostWriteValidationError{Err: errors.New("fixture write")}
	unblock := &ops.UnblockRebaseConflictError{TaskID: "task-blocked", TargetRef: "integration", TargetSHA: "1111111111111111111111111111111111111111", Cause: errors.New("fixture conflict")}
	integration := &ops.IntegrationFailedError{Reason: "fixture integration"}
	notFound := &lizaerrors.NotFoundError{Entity: "task", ID: "task-missing"}
	lock := &filelock.LockError{Type: filelock.LockErrorTimeout, Message: "fixture lock"}

	switch name {
	case "operational-wrapping-lifecycle":
		return operational(lifecycle), true
	case "operational-coded-wrapping-lifecycle":
		return coded(lifecycle), true
	case "joined-operational-lifecycle":
		return errors.Join(operational(nil), precedenceLifecycle("CONFLICT", nil)), true
	case "joined-lifecycle-operational":
		return errors.Join(precedenceLifecycle("CONFLICT", nil), coded(nil)), true
	case "lifecycle-wrapping-not-found":
		return precedenceLifecycle("INVALID_INPUT", notFound), true
	case "lifecycle-wrapping-operational":
		return precedenceLifecycle("CONFLICT", coded(errors.New("fixture raw cause"))), true
	case "lifecycle-wrapping-authority":
		return precedenceLifecycle("FORBIDDEN", authority), true
	case "joined-lifecycle-authority":
		return errors.Join(precedenceLifecycle("CONFLICT", nil), authority), true
	case "joined-authority-lifecycle":
		return errors.Join(authority, precedenceLifecycle("CONFLICT", nil)), true
	case "validation-wrapping-precondition":
		return &lizaerrors.ValidationError{Message: "fixture validation", Err: precondition}, true
	case "operational-wrapping-precondition":
		return operational(precondition), true
	case "operational-wrapping-not-found":
		return coded(notFound), true
	case "operational-coded-wrapping-validation":
		return coded(&lizaerrors.ValidationError{Message: "fixture validation"}), true
	case "integration-wrapping-operational":
		return &ops.IntegrationFailedError{Reason: "fixture integration", Cause: coded(nil)}, true
	case "joined-integration-precondition":
		return errors.Join(integration, precondition), true
	case "joined-operational-integration":
		return errors.Join(coded(nil), integration), true
	case "operational-uncoded-wrapping-lock":
		return operational(fmt.Errorf("fixture wrap: %w", lock)), true
	case "operational-uncoded-wrapping-untyped-not-found":
		return operational(errors.New("fixture: not found")), true
	case "operational-uncoded-wrapping-untyped-validation":
		return operational(errors.New("fixture value must be set")), true
	case "operational-uncoded-wrapping-untyped-internal":
		return operational(errors.New("unclassified fixture cause")), true
	case "joined-post-write-precondition":
		return errors.Join(postWrite, precondition), true
	case "joined-precondition-post-write":
		return errors.Join(precondition, postWrite), true
	case "joined-precondition-unblock-conflict":
		return errors.Join(unblock, precondition), true
	case "joined-unblock-conflict-integration":
		return errors.Join(integration, unblock), true
	case "operational-wrapping-unblock-conflict":
		return operational(unblock), true
	case "joined-lock-operational":
		return errors.Join(lock, operational(nil)), true
	}
	return nil, false
}
