package commands

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-199 / D-93 / D-213 (2026-10 shapes): an approved task's approving reviewer
// stopped before wt-merge and no other live, healthy code-reviewer was
// registered. Any live reviewer would take the merge over, but pool repair
// counted only review-claimable work, so it started nobody and no alert named
// the approval. An approved task without an eligible merge owner is demand for
// one reviewer of its role.

const mergeOwnerRole = "code-reviewer"

// approvedByDeparted is an approved task whose last approval is code-reviewer-1.
func approvedByDeparted(id string, now time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusApproved, now)
	task.Approvals = []models.Approval{{Agent: "code-reviewer-1", Provider: "claude", Timestamp: now}}
	return task
}

// mergeOwnerReviewer is a registered code-reviewer; a past lease makes it
// departed, a current task makes it busy.
func mergeOwnerReviewer(now, leaseExpires time.Time, currentTask string) models.Agent {
	agent := models.Agent{
		Role:         mergeOwnerRole,
		Status:       models.AgentStatusIdle,
		Provider:     "claude",
		PID:          deadPID,
		Heartbeat:    now,
		RegisteredAt: now,
		LeaseExpires: testhelpers.TimePtr(leaseExpires),
		Generation:   testhelpers.TestAgentGeneration,
	}
	if currentTask != "" {
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = testhelpers.StringPtr(currentTask)
	}
	return agent
}

func degradedHealth(agent models.Agent) models.AgentHealth {
	return models.AgentHealth{
		State:        models.AgentHealthDegraded,
		Role:         agent.Role,
		Provider:     agent.Provider,
		PID:          agent.PID,
		RegisteredAt: testhelpers.TimePtr(agent.RegisteredAt),
		DegradedAt:   agent.RegisteredAt,
		Reason:       "claim_worktree_setup_failed",
	}
}

func roleWorkFor(missing []MissingRoleWork, role string) (MissingRoleWork, bool) {
	for _, work := range missing {
		if work.Role == role {
			return work, true
		}
	}
	return MissingRoleWork{}, false
}

type mergeOwnerCase struct {
	name   string
	build  func(now time.Time) *models.State
	cap    int
	block  bool
	wantN  int      // starts for code-reviewer; 0 means no Missing entry
	wantID []string // listed code-reviewer task IDs when wantN > 0
	// wantBlocked lists the code-reviewer tasks set aside as provider-blocked.
	wantBlocked []string
}

func mergeOwnerCases() []mergeOwnerCase {
	departed := func(now time.Time) models.Agent { return mergeOwnerReviewer(now, now.Add(-time.Minute), "") }
	withTasks := func(tasks ...models.Task) *models.State {
		state := testhelpers.CreateValidState()
		state.Tasks = tasks
		state.Agents = map[string]models.Agent{}
		return state
	}
	return []mergeOwnerCase{
		{
			name: "departed approver and no other reviewer start one reviewer",
			build: func(now time.Time) *models.State {
				state := withTasks(approvedByDeparted("approved", now))
				state.Agents["code-reviewer-1"] = departed(now)
				return state
			},
			wantN: 1, wantID: []string{"approved"},
		},
		{
			name: "several ownerless approvals need one owner",
			build: func(now time.Time) *models.State {
				return withTasks(approvedByDeparted("approved-a", now), approvedByDeparted("approved-b", now))
			},
			wantN: 1, wantID: []string{"approved-a", "approved-b"},
		},
		{
			name: "busy healthy reviewer is the owner",
			build: func(now time.Time) *models.State {
				state := withTasks(approvedByDeparted("approved", now))
				state.Agents["code-reviewer-2"] = mergeOwnerReviewer(now, now.Add(30*time.Minute), "other")
				return state
			},
		},
		{
			name: "degraded live reviewer is not an owner",
			build: func(now time.Time) *models.State {
				state := withTasks(approvedByDeparted("approved", now))
				reviewer := mergeOwnerReviewer(now, now.Add(30*time.Minute), "")
				state.Agents["code-reviewer-2"] = reviewer
				state.AgentHealth = map[string]models.AgentHealth{"code-reviewer-2": degradedHealth(reviewer)}
				return state
			},
			wantN: 1, wantID: []string{"approved"},
		},
		{
			name: "role at its cap starts nothing",
			build: func(now time.Time) *models.State {
				state := withTasks(approvedByDeparted("approved", now))
				reviewer := mergeOwnerReviewer(now, now.Add(30*time.Minute), "")
				state.Agents["code-reviewer-2"] = reviewer
				state.AgentHealth = map[string]models.AgentHealth{"code-reviewer-2": degradedHealth(reviewer)}
				return state
			},
			cap: 1,
		},
		{
			name: "quota-blocked role CLI sets the merge aside",
			build: func(now time.Time) *models.State {
				return withTasks(approvedByDeparted("approved", now))
			},
			block:       true,
			wantBlocked: []string{"approved"},
		},
		{
			name: "review start already planned covers the merge",
			build: func(now time.Time) *models.State {
				return withTasks(approvedByDeparted("approved", now),
					testhelpers.BuildTaskByStatus("review", models.TaskStatusReadyForReview, now))
			},
			wantN: 1, wantID: []string{"approved", "review"},
		},
	}
}

func TestFindRoleCapacity_OwnerlessApprovedMerge(t *testing.T) {
	pr := loadRepairResolver(t)
	for _, tt := range mergeOwnerCases() {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			state := tt.build(now)
			if tt.cap > 0 {
				state.Config.MaxInstances = tt.cap
			}
			var blocked ProviderBlock
			if tt.block {
				blocked = func(string) (time.Time, bool) { return now.Add(time.Hour), true }
			}

			capacity := FindRoleCapacity(state, pr, RepairCLI{Explicit: "claude"}, nil, blocked, now)

			work, found := roleWorkFor(capacity.Missing, mergeOwnerRole)
			if tt.wantN == 0 {
				if found {
					t.Fatalf("code-reviewer starts = %d for tasks %v, want none", work.SpawnCount, work.TaskIDs)
				}
			} else {
				if !found {
					t.Fatalf("no code-reviewer demand, want %d start(s) for %v", tt.wantN, tt.wantID)
				}
				if work.SpawnCount != tt.wantN || !slices.Equal(work.TaskIDs, tt.wantID) || work.TaskCount != len(tt.wantID) {
					t.Fatalf("code-reviewer starts = %d, tasks = %v (count %d), want %d start(s) for %v",
						work.SpawnCount, work.TaskIDs, work.TaskCount, tt.wantN, tt.wantID)
				}
				if len(work.AgentIDs) > work.SpawnCount {
					t.Fatalf("code-reviewer agent IDs = %v, more than %d start(s)", work.AgentIDs, work.SpawnCount)
				}
			}

			var gotBlocked []string
			for _, b := range capacity.ProviderBlocked {
				if b.Role == mergeOwnerRole {
					gotBlocked = append(gotBlocked, b.TaskIDs...)
				}
			}
			if !slices.Equal(gotBlocked, tt.wantBlocked) {
				t.Fatalf("provider-blocked code-reviewer tasks = %v, want %v", gotBlocked, tt.wantBlocked)
			}
		})
	}
}

// The review branch keeps its planned reviewer ID and models.yaml item when a
// merge rides along.
func TestFindRoleCapacity_OwnerlessMergeKeepsPlannedReviewStart(t *testing.T) {
	pr := loadSlotResolver(t, 2)
	repairCLI := RepairCLI{RoleModels: slotReviewerFile(t)}
	now := time.Now().UTC()
	reviewOnly := testhelpers.CreateValidState()
	reviewOnly.Agents = map[string]models.Agent{}
	reviewOnly.Tasks = []models.Task{secondReviewTask("review-second", now)}
	mixed := testhelpers.CreateValidState()
	mixed.Agents = map[string]models.Agent{}
	mixed.Tasks = []models.Task{approvedByDeparted("approved", now), reviewOnly.Tasks[0]}

	base, _ := roleWorkFor(FindRoleCapacity(reviewOnly, pr, repairCLI, nil, nil, now).Missing, mergeOwnerRole)
	if len(base.AgentIDs) != 1 || !slices.Equal(base.Items, []int{2}) {
		t.Fatalf("fixture: review-only plan IDs = %v, items = %v, want one planned item-2 start", base.AgentIDs, base.Items)
	}
	got, _ := roleWorkFor(FindRoleCapacity(mixed, pr, repairCLI, nil, nil, now).Missing, mergeOwnerRole)
	if got.SpawnCount != base.SpawnCount || !slices.Equal(got.AgentIDs, base.AgentIDs) || !slices.Equal(got.Items, base.Items) {
		t.Fatalf("mixed demand starts = %d, IDs = %v, items = %v; want the review-only plan %d, %v, %v",
			got.SpawnCount, got.AgentIDs, got.Items, base.SpawnCount, base.AgentIDs, base.Items)
	}
}

func TestFindMissingRolesWithClaimableWork_OwnerlessApprovedMerge(t *testing.T) {
	projectRoot := writeRepairAgentPoolState(t, testhelpers.CreateValidState())
	resolver, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}
	want := map[string][]string{
		"departed approver and no other reviewer start one reviewer": {"approved"},
		"busy healthy reviewer is the owner":                         nil,
		"role at its cap starts nothing":                             {"approved"},
	}
	for _, tt := range mergeOwnerCases() {
		wantIDs, checked := want[tt.name]
		if !checked {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			state := tt.build(time.Now().UTC())
			if tt.cap > 0 {
				state.Config.MaxInstances = tt.cap
			}
			work, found := roleWorkFor(FindMissingRolesWithClaimableWork(state, resolver, rolemodels.File{}), mergeOwnerRole)
			if !slices.Equal(work.TaskIDs, wantIDs) {
				t.Fatalf("missing code-reviewer (reported %v) tasks = %v, want %v", found, work.TaskIDs, wantIDs)
			}
		})
	}
}

// The repair preview reports merge-only demand as reviewer work with its tasks
// and start count; the orchestrator line stays for demand without tasks.
func TestPrintRepairAgentPoolResult_OwnerlessMergeIsReviewerWork(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{approvedByDeparted("approved", now)}
	state.Agents = map[string]models.Agent{"code-reviewer-1": mergeOwnerReviewer(now, now.Add(-time.Minute), "")}
	root := writeRepairAgentPoolState(t, state)

	result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, CLI: "claude", DryRun: true})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	result.Missing = append(result.Missing, orchestratorRoleWork(orchestratorPresence{Roles: []string{models.RoleOrchestrator}}))

	stdout := captureStdout(t, func() { printRepairAgentPoolResult(result) })
	wantReviewer := "  code-reviewer: 1 task(s) (approved); starting 1 agent(s): approved task(s) with no live, healthy merge owner"
	if !strings.Contains(stdout, wantReviewer+"\n") || strings.Contains(stdout, "Missing orchestrator: code-reviewer") ||
		!strings.Contains(stdout, "Missing orchestrator: orchestrator (") {
		t.Fatalf("output must list the merge as code-reviewer work and keep the orchestrator line:\n%s", stdout)
	}
}
