package statevalidate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// agentAssignments maps an agent ID to the IDs of the active tasks assigned to
// it, so one agent holding several tasks is reported once after all tasks are
// seen.
type agentAssignments map[string][]string

// collect records the task's assignment. Dormant assignments still reserve an
// agent; terminal ones are history.
func (a agentAssignments) collect(task *models.Task, resolver *pipeline.Resolver) {
	terminal := task.Status.IsTerminal()
	if resolver != nil {
		terminal = models.IsOperationallyTerminal(task, resolver)
	}
	if task.AssignedTo != nil && *task.AssignedTo != "" && !strings.HasPrefix(*task.AssignedTo, "$") && !terminal {
		a[*task.AssignedTo] = append(a[*task.AssignedTo], task.ID)
	}
}

// validate reports every pair of active tasks sharing an agent, agents and
// task IDs in sorted order.
func (a agentAssignments) validate(v *violations) {
	agents := make([]string, 0, len(a))
	for agent := range a {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	for _, agent := range agents {
		taskIDs := a[agent]
		sort.Strings(taskIDs)
		// One violation per conflicting pair: releasing one task leaves the
		// other pairs' identities intact, and assigning a further task adds
		// pairs of its own.
		for i := range taskIDs {
			for _, other := range taskIDs[i+1:] {
				v.add(fmt.Errorf("agent %s assigned to multiple active tasks simultaneously: [%s %s]", agent, taskIDs[i], other))
			}
		}
	}
}
