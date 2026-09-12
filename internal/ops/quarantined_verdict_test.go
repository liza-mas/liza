package ops

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestQuarantinedVerdictPreservesFencedFinding(t *testing.T) {
	taskID := "task-quarantine"
	const reviewerID = "code-reviewer-1"
	const reason = "api.go:42: non-create mutation omits expected_version; forward both concurrency values"
	const oldGeneration = "quarantine-old-registration-fixture"
	const currentGeneration = "quarantine-current-registration-fixture"
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReviewing, time.Now().UTC())
	commit := strings.Repeat("a", 40)
	task.ReviewCommit = &commit
	state.Tasks = []models.Task{task}
	state.Agents[reviewerID] = models.Agent{
		Role: "code-reviewer", Status: models.AgentStatusReviewing,
		Generation: currentGeneration, CurrentTask: &taskID,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	before, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}

	_, err = SubmitVerdictWithAuthority(projectRoot, taskID, "REJECTED", reason,
		models.AgentAuthority{ID: reviewerID, Generation: oldGeneration}, "", commit)
	if !IsAgentAuthorityError(err) {
		t.Fatalf("want generation fencing, got %T", err)
	}
	after, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
		t.Fatal("fenced verdict changed task or replacement agent")
	}

	// Read the durable format, independently of any new in-memory model field.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Findings []struct {
			TaskID       string `yaml:"task_id"`
			ReviewCommit string `yaml:"review_commit"`
			ReviewerID   string `yaml:"reviewer_id"`
			Verdict      string `yaml:"verdict"`
			Reason       string `yaml:"reason"`
		} `yaml:"quarantined_verdicts"`
	}
	if err := yaml.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Findings) != 1 {
		t.Fatalf("fenced substantive verdict lost: persisted %d findings, want 1", len(persisted.Findings))
	}
	finding := persisted.Findings[0]
	if finding.TaskID != taskID || finding.ReviewCommit != commit || finding.ReviewerID != reviewerID || finding.Verdict != "REJECTED" || finding.Reason != reason {
		t.Fatal("persisted finding lost its reviewed boundary, provenance, or substantive reason")
	}
}

func TestQuarantinedVerdictFailedIntegrationIsNotReportedAsMerged(t *testing.T) {
	f := newQuarantineFixture(t)
	f.mutate(t, func(state *models.State) {
		task := state.FindTask(f.taskID)
		task.Status = models.TaskStatusIntegrationFailed
		task.MergeCommit = &f.commit
	})
	before := f.read(t)
	_, err := SubmitVerdictWithAuthority(f.root, f.taskID, "REJECTED", "missing concurrency check", f.stale, "", f.commit)
	var saved *QuarantinedVerdictError
	if !errors.As(err, &saved) || !IsAgentAuthorityError(err) {
		t.Fatalf("want retained evidence and authority fence, got %v", err)
	}
	if saved.PostMerge || saved.SafeDetails()["post_merge"] != false || strings.Contains(err.Error(), "already merged") {
		t.Fatal("failed integration was incorrectly reported as merged")
	}
	after := f.read(t)
	if len(after.QuarantinedVerdicts) != 1 || after.QuarantinedVerdicts[0].ID != saved.FindingID {
		t.Fatal("failed integration lost the fenced finding")
	}
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
		t.Fatal("fenced finding changed failed integration or agent state")
	}
}

func TestQuarantinedVerdictAuthorityDiagnosticsDoNotExposeGenerations(t *testing.T) {
	const oldGeneration = "quarantine-old-registration-fixture"
	const currentGeneration = "quarantine-current-registration-fixture"
	state := &models.State{Agents: map[string]models.Agent{
		"code-reviewer-1": {Generation: currentGeneration},
	}}
	err := RequireAgentAuthority(state, models.AgentAuthority{ID: "code-reviewer-1", Generation: oldGeneration})
	var authorityErr *AgentAuthorityError
	if !errors.As(err, &authorityErr) {
		t.Fatalf("want authority error, got %T", err)
	}
	details, marshalErr := json.Marshal(authorityErr.SafeDetails())
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for name, diagnostic := range map[string]string{"text": err.Error(), "JSON details": string(details)} {
		if strings.Contains(diagnostic, oldGeneration) || strings.Contains(diagnostic, currentGeneration) {
			t.Errorf("%s exposes a reusable registration generation", name)
		}
		if !strings.Contains(diagnostic, "code-reviewer-1") {
			t.Errorf("%s lost the reviewer identity", name)
		}
		for _, generation := range []string{oldGeneration, currentGeneration} {
			want := fmt.Sprintf("%x", sha256.Sum256([]byte(generation)))
			if !strings.Contains(diagnostic, want) {
				t.Errorf("%s lost the SHA-256 generation fingerprint", name)
			}
		}
	}
	firstFingerprint := generationFingerprint(oldGeneration)
	if firstFingerprint == "" || firstFingerprint != generationFingerprint(oldGeneration) || firstFingerprint == generationFingerprint(currentGeneration) {
		t.Fatal("generation fingerprints must be present, deterministic, and distinguish generations")
	}
}

func TestQuarantinedVerdictCleanGlobalApprovalDoesNotReenterLock(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprintf("authenticated=%t", authenticated), func(t *testing.T) {
			fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseGlobal, nil)
			commit := strings.Repeat("a", 40)
			const generation = "clean-reviewer-registration-fixture"
			fixture.mutateState(t, func(state *models.State) {
				state.FindTask(fixture.taskID).ReviewCommit = &commit
				agent := state.Agents[fixture.reviewerID]
				agent.Generation = generation
				state.Agents[fixture.reviewerID] = agent
			})
			previousVerifier := verifyCleanIntegrationSourceForVerdict
			verifyCleanIntegrationSourceForVerdict = func(_ string, source string) (cleanIntegrationSourceVerification, error) {
				return cleanIntegrationSourceVerification{SourceCommit: source, IntegrationHEAD: source, Effective: true}, nil
			}
			t.Cleanup(func() { verifyCleanIntegrationSourceForVerdict = previousVerifier })
			started := time.Now()
			var err error
			if authenticated {
				_, err = SubmitVerdictWithAuthority(fixture.projectRoot, fixture.taskID, "APPROVED", "", models.AgentAuthority{ID: fixture.reviewerID, Generation: generation}, "", commit)
			} else {
				_, err = SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, "")
			}
			if err != nil {
				t.Fatalf("clean global approval: %v", err)
			}
			if time.Since(started) > 10*time.Second {
				t.Fatal("clean global approval exceeded lock re-entry deadline")
			}
			after := fixture.readState(t)
			if after.Goal.Integration.Closure == nil || after.FindTask(fixture.taskID).Status != models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN") {
				t.Fatal("clean global approval did not produce clean closure and terminal task")
			}
		})
	}
}
