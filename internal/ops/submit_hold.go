package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// submitHoldPath names one caller's submit hold for a task: its agent and
// generation, so a restarted session is never held up by an abandoned one.
func submitHoldPath(statePath, taskID, agentID string, authority *models.AgentAuthority) string {
	generation := ""
	if authority != nil {
		generation = authority.Generation
	}
	sum := sha256.Sum256([]byte(taskID + "\x00" + agentID + "\x00" + generation))
	return statePath + ".submit-" + hex.EncodeToString(sum[:8])
}

// holdSubmitTask takes the caller's submit hold on the task for the whole
// invocation, slot wait included (ADR-0210). While another submit of the same
// caller holds it, this one queues and prepares nothing and refuses at once,
// as a duplicate of a pending preparation does: that call's result settles
// the request. The hold is a flock, so a killed submit frees it for a retry.
func holdSubmitTask(projectRoot, taskID, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions) (*filelock.Held, error) {
	held, acquired, err := filelock.New(submitHoldPath(paths.New(projectRoot).StatePath(), taskID, agentID, authority)).TryHold(integrationOperationSubmitForReview)
	if err != nil || acquired {
		return held, err
	}
	return nil, &LifecycleError{
		Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, readLifecycleTask(projectRoot, taskID, authority, opts), models.LifecycleStateChanged, "requery", "unknown"),
		Err:     fmt.Errorf("another submit-for-review of task %s by this agent is still running; wait for its result instead of submitting again", taskID),
	}
}
