package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// discoveryDisposition is a non-task-ID disposition of a discovery entry. The
// stored ConvertedToTask stays a string; only the sentinel decision is typed.
type discoveryDisposition string

const (
	discoveryDispositionDeferred  discoveryDisposition = "deferred"
	discoveryDispositionDismissed discoveryDisposition = "dismissed"
)

// isSentinel reports whether the disposition is one of the two non-task values.
func (d discoveryDisposition) isSentinel() bool {
	return d == discoveryDispositionDeferred || d == discoveryDispositionDismissed
}

// SetDiscoveryDisposition sets the converted_to_task field on a discovery entry.
// Valid dispositions: a task ID (when converted to task), "deferred", or "dismissed".
func SetDiscoveryDisposition(projectRoot, discoveryID, disposition string) error {
	if discoveryID == "" {
		return &PreconditionError{Reason: "discovery_id is required"}
	}
	if disposition == "" {
		return &PreconditionError{Reason: "disposition is required"}
	}
	isTaskRef := !discoveryDisposition(disposition).isSentinel()
	if isTaskRef {
		if err := paths.ValidateTaskID(disposition); err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("invalid disposition %q: must be a valid task ID, %q, or %q", disposition, discoveryDispositionDeferred, discoveryDispositionDismissed)}
		}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	return bb.Modify(func(state *models.State) error {
		// Verify referenced task exists when disposition is a task ID.
		if isTaskRef && state.FindTask(disposition) == nil {
			return &PreconditionError{Reason: fmt.Sprintf("disposition references task %q which does not exist", disposition)}
		}

		for i := range state.Discovered {
			if state.Discovered[i].ID == discoveryID {
				state.Discovered[i].ConvertedToTask = &disposition
				return nil
			}
		}
		return &PreconditionError{Reason: fmt.Sprintf("discovery %q not found", discoveryID)}
	})
}
