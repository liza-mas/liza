package ops

import (
	"fmt"
	"sync"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

var lifecycleBeforeModifyTestHook func()

// modifyLifecycleState keeps the staged non-CLI compatibility surface in one
// place while making authenticated command entry points select the generation
// fence for every transaction they can reach.
func modifyLifecycleState(
	bb *db.Blackboard,
	authority *models.AgentAuthority,
	mutate func(*models.State) error,
) error {
	if authority == nil {
		return bb.Modify(mutate)
	}
	if lifecycleBeforeModifyTestHook != nil {
		lifecycleBeforeModifyTestHook()
	}
	return ModifyWithAgentAuthority(bb, *authority, mutate)
}

func lifecycleAgentID(agentID string, authority *models.AgentAuthority) (string, error) {
	if authority == nil {
		return agentID, nil
	}
	if agentID != "" && agentID != authority.ID {
		return "", &PreconditionError{Reason: fmt.Sprintf(
			"agent ID %s does not match authority ID %s", agentID, authority.ID)}
	}
	return authority.ID, nil
}

// RequestBlackboard selects acquisition-only retry for an authenticated request
// whose explicit identity and original transition are preserved by its caller.
// Operator and legacy calls retain the ordinary bounded lock wait. The protected
// transaction runs once; callback failures and nested timeouts are not replayed.
func RequestBlackboard(statePath string, authority *models.AgentAuthority, opts LifecycleRequestOptions) *db.Blackboard {
	bb := db.For(statePath)
	if authority != nil && authority.ID != "" && authority.Generation != "" &&
		opts.RetryContext != nil && opts.RequestID != "" && opts.ExpectedTransition != "" &&
		ValidateLifecycleRequestOptions(opts) == nil {
		return bb.WithLockRetryContext(opts.RetryContext)
	}
	return bb
}

type stateMutation func(func(*models.State) error) error

var lifecycleMutationTestHooks sync.Map

// lifecycleMutation preserves legacy internal callers while authority-aware
// command paths compare their registration generation inside the same lock as
// the lifecycle write. Downstream supervisor work migrates the legacy callers.
func lifecycleMutation(bb *db.Blackboard, authority *models.AgentAuthority) stateMutation {
	if authority == nil {
		return bb.Modify
	}
	return func(mutate func(*models.State) error) error {
		runLifecycleMutationTestHook(bb)
		return ModifyWithAgentAuthority(bb, *authority, mutate)
	}
}

func requireAuthorityActor(authority models.AgentAuthority, actorID string) error {
	if actorID != authority.ID {
		return &PreconditionError{Reason: fmt.Sprintf("agent ID %q does not match authority agent %q", actorID, authority.ID)}
	}
	return nil
}

// setLifecycleMutationTestHook installs a transaction-adjacent barrier for one
// blackboard fixture. The per-blackboard key and sync.Map keep parallel tests
// on unrelated fixtures isolated and race-free.
func setLifecycleMutationTestHook(bb *db.Blackboard, hook func()) func() {
	lifecycleMutationTestHooks.Store(bb, hook)
	return func() { lifecycleMutationTestHooks.Delete(bb) }
}

func runLifecycleMutationTestHook(bb *db.Blackboard) {
	if hook, ok := lifecycleMutationTestHooks.Load(bb); ok {
		hook.(func())()
	}
}
