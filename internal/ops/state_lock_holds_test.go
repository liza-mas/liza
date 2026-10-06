package ops

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestStateLockHoldWake(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		sequence uint64
		after    uint64
		untagged bool
		stale    string
		guard    string
		want     int
	}{
		{name: "later successful publication", sequence: 43, after: 42, want: 1},
		{name: "own publication", sequence: 42, after: 42},
		{name: "future publication", sequence: 43, after: 44},
		{name: "prose alone", sequence: 43, after: 42, untagged: true},
		{name: "different blocked episode", sequence: 43, after: 42, stale: "episode"},
		{name: "different reason", sequence: 43, after: 42, stale: "reason"},
		{name: "different questions", sequence: 43, after: 42, stale: "questions"},
		{name: "human owned", sequence: 43, after: 42, guard: "human"},
		{name: "repair request", sequence: 43, after: 42, guard: "repair"},
		{name: "open rejection RCA", sequence: 43, after: 42, guard: "rca"},
		{name: "unfinished preparation", sequence: 43, after: 42, guard: "preparation"},
		{name: "missing dependency", sequence: 43, after: 42, guard: "missing dependency"},
		{name: "pending dependency", sequence: 43, after: 42, guard: "pending dependency"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stateFile, _ := metadataLifecycleFixture(t, "assess-blocked")
			var episode time.Time
			var blockerReason string
			var blockerQuestions []string
			if err := db.For(stateFile).Modify(func(state *models.State) error {
				task := state.FindTask("target")
				episode = task.Created.UTC()
				blockerReason = "state lock timed out while submitting completed work"
				blockerQuestions = []string{"Can submission resume when contention clears?"}
				task.BlockedReason = &blockerReason
				task.BlockedQuestions = blockerQuestions
				task.AssignedTo, task.LeaseExpires = nil, nil
				task.History = []models.TaskHistoryEntry{{Time: episode, Event: models.TaskEventBlocked, Reason: &blockerReason}}
				switch tc.guard {
				case "human":
					task.History[0].Extra = map[string]any{models.AwaitingHumanExtraKey: "Operator must inspect the lock holder"}
				case "repair":
					task.RepairRequest = &models.RepairRequest{
						Operation: "recover-task", Target: task.ID,
						Evidence:   []string{"error=repair requires operator intervention"},
						Validation: []string{"verify repaired state"},
					}
				case "rca":
					task.RejectionRCA = &models.RejectionRCARecord{
						SchemaVersion: models.RejectionRCASchemaVersion, Threshold: 3,
						RejectionCount: 3, GatedAt: episode,
					}
				case "preparation":
					request, err := NewLifecycleRequest("submit-for-review", task, "coder-1", nil, LifecycleRequestOptions{}, "unfinished submission")
					if err != nil {
						return err
					}
					task.Lifecycle = &models.TaskLifecycle{Preparation: &models.LifecyclePreparation{
						LifecycleIdentity: request, Boundary: models.TaskTransitionID(task),
					}}
				case "missing dependency":
					task.DependsOn = []string{"missing"}
				case "pending dependency":
					task.DependsOn = []string{"pending"}
					pending := testhelpers.BuildTaskByStatus("pending", models.TaskStatusReady, episode)
					pending.SpecRef = task.SpecRef
					state.Tasks = append(state.Tasks, pending)
					// append may relocate the task slice.
					task = state.FindTask("target")
				}
				candidate := currentBlockerCandidate(state, task)
				candidate.Note = "Hold until state contention clears"
				fingerprint := BuildAssessmentFingerprint(state, task, candidate)
				extra := map[string]any{AssessmentFingerprintExtraKey: fingerprint}
				if candidate.HumanAction != "" {
					extra[models.AwaitingHumanExtraKey] = candidate.HumanAction
				}
				task.History = append(task.History, models.TaskHistoryEntry{
					Time: episode.Add(time.Second), Event: models.TaskEventOrchestratorAssessment,
					Note: &candidate.Note, Extra: extra,
				})
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			// Physical YAML exercises the public reader before the new model fields
			// exist; no fake recovery function or unimplemented Go type is needed.
			data, err := os.ReadFile(stateFile)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := yaml.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			document["mutation_sequence"] = tc.sequence
			if !tc.untagged {
				reason, questions := blockerReason, blockerQuestions
				if tc.stale == "reason" {
					reason = "earlier timeout reason"
				}
				if tc.stale == "questions" {
					questions = []string{"An earlier question?"}
				}
				canonical, err := json.Marshal(struct {
					Reason    string   `json:"reason"`
					Questions []string `json:"questions"`
				}{reason, questions})
				if err != nil {
					t.Fatal(err)
				}
				digest := fmt.Sprintf("%x", sha256.Sum256(canonical))
				holdEpisode := episode
				if tc.stale == "episode" {
					holdEpisode = episode.Add(-time.Minute)
				}
				for _, raw := range document["tasks"].([]any) {
					task := raw.(map[string]any)
					if task["id"] == "target" {
						task["state_lock_hold"] = map[string]any{
							"episode_at": holdEpisode, "blocker_digest": digest, "after_sequence": tc.after,
						}
					}
				}
			}
			data, err = yaml.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(stateFile, data, 0o644); err != nil {
				t.Fatal(err)
			}
			state, err := db.For(stateFile).ReadSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if got := CountActionableBlockedTasks(state); got != tc.want {
				t.Fatalf("actionable blocked tasks = %d, want %d (publication=%d, hold=%d, guard=%q, stale=%q)", got, tc.want, tc.sequence, tc.after, tc.guard, tc.stale)
			}
		})
	}
}
