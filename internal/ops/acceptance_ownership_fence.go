package ops

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// acceptanceOwnershipPollInterval is how often canonical execution re-reads
// task ownership; tests shorten it.
var acceptanceOwnershipPollInterval = 15 * time.Second

// startAcceptanceOwnershipFence returns a context cancelled once a state
// snapshot shows the submitting claim is gone, so canonical execution stops
// instead of running for a publication the final transaction would refuse.
// Supervisor exit releases a claim with a direct state write while this
// command keeps running outside the provider's process group (D-38).
// A snapshot only stops work; the locked final transaction still decides
// publication. Read failures never cancel. stop ends the fence and reports
// whether it fired.
func startAcceptanceOwnershipFence(bb *db.Blackboard, taskID string, status models.TaskStatus, agentID string, authority *models.AgentAuthority) (ctx context.Context, stop func() bool) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lost := false // Read only after joining the observer.
	go func() {
		defer close(done)
		ticker := time.NewTicker(acceptanceOwnershipPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				state, err := bb.ReadSnapshot()
				if err != nil {
					log.Printf("WARNING: acceptance ownership check for task %s failed; will retry: %v", taskID, err)
					continue
				}
				if !acceptanceClaimHeld(state, taskID, status, agentID, authority) {
					lost = true
					cancel()
					return
				}
			}
		}
	}()
	return ctx, sync.OnceValue(func() bool { cancel(); <-done; return lost })
}

func acceptanceClaimHeld(state *models.State, taskID string, status models.TaskStatus, agentID string, authority *models.AgentAuthority) bool {
	task := state.FindTask(taskID)
	if task == nil || task.Status != status || task.AssignedTo == nil || *task.AssignedTo != agentID {
		return false
	}
	return authority == nil || RequireAgentAuthority(state, *authority) == nil
}
