package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/statehygiene"
	"gopkg.in/yaml.v3"
)

// MigrateCommand normalizes legacy state.yaml records written by an older
// binary: underscore-form role names, legacy attempted fields, ownership
// tuples left half-populated, and oversized or raw transcript text. Returns
// (changed, error) where changed indicates whether any modifications were made.
//
// Uses ReadRaw + manual unmarshal to bypass db.Read()'s read-path
// normalization, which would hide the underscore roles we need to detect.
func MigrateCommand(statePath string) (bool, error) {
	bb := db.For(statePath)
	raw, err := bb.ReadRaw()
	if err != nil {
		return false, fmt.Errorf("failed to read state file: %w", err)
	}

	var state models.State
	if err := yaml.Unmarshal(raw, &state); err != nil {
		return false, fmt.Errorf("failed to parse state file: %w", err)
	}

	changed := false
	for agentID, agent := range state.Agents {
		normalized := roles.NormalizeRoleName(agent.Role)
		if normalized != agent.Role {
			agent.Role = normalized
			state.Agents[agentID] = agent
			changed = true
		}
	}

	for i := range state.Tasks {
		if state.Tasks[i].MigrateAttemptedField() {
			changed = true
		}
		// Validation requires `assigned_to` and `lease_expires` together for
		// rejected statuses (statevalidate.validate_task.go), and requires an
		// assignee wherever a lease is required at all; the sweep below is
		// deliberately broader, because a lease on any unassigned task is
		// meaningless. Versions before the renewLease guard could renew a lease
		// on a task whose doer had already exited, and the surviving record
		// blocks every later mutation, because post-mutation validation is
		// global — including the repairs that would clear it. The mirror half,
		// an assignee with no lease, is not repaired here: every path that
		// clears a lease clears the assignee in the same mutation, so it is not
		// reachable, and inventing a lease expiry would be worse than leaving
		// it for inspection.
		if state.Tasks[i].AssignedTo == nil && state.Tasks[i].LeaseExpires != nil {
			state.Tasks[i].LeaseExpires = nil
			changed = true
		}
	}
	if statehygiene.ScrubStateForMigration(&state) {
		changed = true
	}

	if !changed {
		return false, nil
	}

	if err := bb.Write(&state); err != nil {
		return false, fmt.Errorf("failed to write migrated state: %w", err)
	}

	return true, nil
}
