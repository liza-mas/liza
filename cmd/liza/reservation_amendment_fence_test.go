package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReservationOnlyWriterFencedUntilArchitectureCorrectionAdopted(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	const carrier = "specs/architecture.md"
	writeAmendmentFile(t, root, carrier, "# Architecture\n\n## Scope One\nOwned boundary.\n\n### CONTRACT\nCallers use the reviewed interface.\n")
	testhelpers.MustGit(t, root, "add", carrier)
	testhelpers.MustGit(t, root, "commit", "-m", "test: reviewed architecture contract")
	review := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	testhelpers.MustGit(t, root, "branch", "-f", "integration", review)
	if err := db.For(statePath).Modify(func(s *models.State) error {
		s.Agents["coder-3"] = mutationTestAgent("coder")
		for _, role := range []string{"architect", "architecture-reviewer"} {
			s.Agents[role+"-1"] = mutationTestAgent(role)
		}
		parent := s.FindTask("original")
		parent.Type, parent.RolePair = models.TaskTypeArchitecture, "architecture-pair"
		parent.ArchRef = carrier + "#Scope One"
		parent.Output = []models.OutputEntry{{Desc: "implement boundary", DoneWhen: "reviewed", Scope: "boundary", SpecRef: parent.SpecRef, ArchRef: parent.ArchRef}}
		parent.ReviewCommit, parent.MergeCommit = &review, &review
		parent.TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
		s.FindTask("started-consumer").ProviderDependencies[0].Transition = "architecture-to-code-plan"
		for i := range parent.History {
			parent.History[i].Commit = &review
		}
		now := time.Now().UTC()
		child := testhelpers.BuildTaskByStatus("original-cp-0", models.TaskStatusMerged, now)
		child.Type, child.RolePair = models.TaskTypePlanning, "code-planning-pair"
		child.ParentTasks = []string{parent.ID}
		for _, id := range []string{"reservation-ready", "reservation-running"} {
			writer := testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, now)
			s.Tasks = append(s.Tasks, writer)
			s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, id)
		}
		s.Tasks = append(s.Tasks, child)
		s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, child.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}
	for _, id := range []string{"reservation-ready", "reservation-running"} {
		if _, err := ops.ReserveProviderWithAuthority(root, ops.ReserveProviderInput{TaskID: id, ProviderTask: "original", Transition: "architecture-to-code-plan", Reason: "wait for reviewed boundary"}, authority); err != nil {
			t.Fatal(err)
		}
	}
	runAmendmentCLI(t, root, "claim-task", "reservation-running", "coder-1", "--json")
	runAmendmentCLI(t, root, "write-checkpoint", "reservation-running", "--intent", "Document reviewed boundary", "--validation-plan", "Review boundary documentation", "--files-to-modify", "README.md", "--tdd-not-required", "Documentation-only candidate; this test covers lifecycle admission", "--agent-id", "coder-1", "--json")
	writerWorktree := *readState(t, statePath).FindTask("reservation-running").Worktree
	if !filepath.IsAbs(writerWorktree) {
		writerWorktree = filepath.Join(root, writerWorktree)
	}
	writeAmendmentFile(t, writerWorktree, "README.md", "# Documented boundary\n")
	testhelpers.MustGit(t, writerWorktree, "add", "README.md")
	testhelpers.MustGit(t, writerWorktree, "commit", "-m", "docs: document reviewed boundary")
	correction := beginAmendmentCLI(t, root, statePath, "--contract", "--trigger", "interface-correction")
	cfg, err := pipeline.LoadFrozen(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := statevalidate.ValidateProviderDependencies(readState(t, statePath), pipeline.NewResolver(cfg)); err != nil {
		t.Fatalf("commissioning correction invalidated existing execution: %v", err)
	}
	for _, args := range [][]string{
		{"claim-task", "reservation-ready", "coder-3", "--json"},
		{"write-checkpoint", "reservation-running", "--intent", "Implement boundary", "--validation-plan", "Run boundary tests", "--files-to-modify", "README.md", "--agent-id", "coder-1", "--json"},
		{"submit-for-review", "reservation-running", "HEAD", "--agent-id", "coder-1", "--json"},
	} {
		before := readStateBytes(t, statePath)
		stdout, err := executeRootCommandCapture(t, root, args...)
		want := "reviewed plan amendment pending for original"
		if args[0] == "claim-task" {
			want = "original (unsatisfied_pending): reviewed plan amendment is pending"
		}
		if err == nil || !strings.Contains(stdout, want) {
			t.Fatalf("reservation-only operation escaped fence: %v err=%v stdout=%s", args, err, stdout)
		}
		if readStateBytes(t, statePath) != before {
			t.Fatalf("refused operation changed state: %v", args)
		}
	}
	// The fenced original is already expanded. Its correction still uses the
	// ordinary independent architecture review and merge lifecycle.
	runAmendmentCLI(t, root, "claim-task", correction, "architect-1", "--json")
	runAmendmentCLI(t, root, "write-checkpoint", correction, "--intent", "Correct interface prose", "--validation-plan", "Review unchanged ownership and allocation", "--files-to-modify", carrier, "--tdd-not-required", "Reviewed planning prose; this test exercises adoption", "--agent-id", "architect-1", "--json")
	task := readState(t, statePath).FindTask(correction)
	wt := *task.Worktree
	if !filepath.IsAbs(wt) {
		wt = filepath.Join(root, wt)
	}
	writeAmendmentFile(t, wt, carrier, "# Architecture\n\n## Scope One\nOwned boundary.\n\n### CONTRACT\nCallers use the clarified reviewed interface.\n")
	testhelpers.MustGit(t, wt, "add", carrier)
	testhelpers.MustGit(t, wt, "commit", "-m", "fix: clarify existing interface contract")
	manifest, err := json.Marshal(readState(t, statePath).FindTask("original").Output)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "output.json")
	if err := os.WriteFile(manifestPath, manifest, 0644); err != nil {
		t.Fatal(err)
	}
	runAmendmentCLI(t, root, "set-task-output", correction, "--output", manifestPath, "--agent-id", "architect-1", "--json")
	runAmendmentCLI(t, root, "submit-for-review", correction, "HEAD", "--agent-id", "architect-1", "--json")
	reviewer := models.AgentAuthority{ID: "architecture-reviewer-1", Generation: testhelpers.TestAgentGeneration}
	if _, err := ops.ClaimReviewerTask(ops.ClaimReviewerTaskInput{ProjectRoot: root, TaskID: correction, AgentID: reviewer.ID, Role: "architecture-reviewer", Authority: &reviewer}); err != nil {
		t.Fatal(err)
	}
	review = *readState(t, statePath).FindTask(correction).ReviewCommit
	runAmendmentCLI(t, root, "submit-verdict", correction, "APPROVED", "--review-commit", review, "--agent-id", reviewer.ID, "--json")
	runAmendmentCLI(t, root, "wt-merge", correction, "--agent-id", reviewer.ID, "--json")
	runAmendmentCLI(t, root, "amend-plan", "original", "--apply", correction, "--agent-id", "orchestrator-1", "--json")
	runAmendmentCLI(t, root, "claim-task", "reservation-ready", "coder-3", "--json")
	runAmendmentCLI(t, root, "write-checkpoint", "reservation-running", "--intent", "Document reviewed boundary", "--validation-plan", "Review boundary documentation", "--files-to-modify", "README.md", "--tdd-not-required", "Documentation-only candidate; this test covers lifecycle admission", "--agent-id", "coder-1", "--json")
	runAmendmentCLI(t, root, "submit-for-review", "reservation-running", "HEAD", "--agent-id", "coder-1", "--json")
}
