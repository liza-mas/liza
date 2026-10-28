package commands

import (
	"maps"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers"
)

// C1 lease cases: the configured lease_duration, and the unset path (0), which
// must give the 1800 s default. Expected deltas are literals, independent of
// the resolver under test.
var c1LeaseCases = []struct {
	name       string
	configured int
	want       time.Duration
}{
	{"configured_600", 600, 600 * time.Second},
	{"default_unset", 0, 1800 * time.Second},
}

// The planner's hypothetical reviewer rows carry the configured lease, for the
// spawn-CLI row and for every models.yaml slot row alike, and the projection
// never changes the input state's agents.
func TestC1ReviewerStartPlannerRowsHonourConfiguredLease(t *testing.T) {
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	for _, tc := range c1LeaseCases {
		for _, variant := range []struct {
			name     string
			cli      RepairCLI
			wantRows int
		}{
			{"spawn_cli", RepairCLI{Explicit: "claude"}, 1},
			{"slot_rows", RepairCLI{RoleModels: slotReviewerFile(t)}, 2},
		} {
			t.Run(tc.name+"/"+variant.name, func(t *testing.T) {
				state := testhelpers.CreateValidState()
				state.Config.LeaseDuration = tc.configured
				inputAgents := maps.Clone(state.Agents)

				planner := newReviewerStartPlanner(state, loadSlotResolver(t, 3), variant.cli, "code-reviewer", nil, now)
				if planner == nil {
					t.Fatal("newReviewerStartPlanner returned nil")
				}
				if len(planner.rows) != variant.wantRows {
					t.Fatalf("rows = %d, want %d", len(planner.rows), variant.wantRows)
				}
				for i, row := range planner.rows {
					if row.LeaseExpires == nil {
						t.Fatalf("row %d has no lease", i)
					}
					if got := row.LeaseExpires.Sub(now); got != tc.want {
						t.Errorf("row %d lease = now + %v, want + %v", i, got, tc.want)
					}
				}
				if !reflect.DeepEqual(state.Agents, inputAgents) {
					t.Error("input state agents changed by the projection")
				}
			})
		}
	}
}
