package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSubmitForReview_TDDEnforcement_CSharp(t *testing.T) {
	for _, tc := range []struct {
		name     string
		testFile string
	}{
		{name: "singular", testFile: "tests/authority/SessionTest.cs"},
		{name: "plural", testFile: "tests/authority/SessionTests.cs"},
		{name: "support files alone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			testhelpers.MustGit(t, root, "checkout", "integration")
			g := git.New(root)
			taskID, agentID := "csharp-submit", "coder-1"
			base, err := g.CreateWorktree(taskID, "integration")
			if err != nil {
				t.Fatal(err)
			}
			wt := g.GetWorktreePath(taskID)
			files := map[string]string{
				"Session.cs":                          "public class Session {}\n",
				"tests/authority/AuthorityFixture.cs": "public class AuthorityFixture {}\n",
				"tests/authority/Access.Tests.csproj": "<Project />\n",
			}
			if tc.testFile != "" {
				// Only filename admission is exercised here; canonical execution is separate.
				files[tc.testFile] = "public class SessionTests {}\n"
			}
			for name, content := range files {
				path := filepath.Join(wt, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			testhelpers.MustGit(t, wt, "add", ".")
			testhelpers.MustGit(t, wt, "commit", "-m", "Add C# submission fixture")
			head := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
			now := time.Now().UTC()
			task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
			worktree := g.GetWorktreeRelPath(taskID)
			task.BaseCommit, task.Worktree = &base, &worktree
			task.History = []models.TaskHistoryEntry{{
				Time: now, Event: models.TaskEventPreExecutionCheckpoint, Agent: &agentID,
				Extra: map[string]any{"intent": "exercise C# filename admission"},
			}}
			state := &models.State{
				Config: models.Config{IntegrationBranch: "integration", LeaseDuration: 1800},
				Tasks:  []models.Task{task},
				Agents: map[string]models.Agent{agentID: {Status: models.AgentStatusWorking, CurrentTask: &taskID}},
			}
			testhelpers.WriteInitialState(t, statePath, state)

			_, err = SubmitForReview(root, taskID, head, agentID)
			wantStatus := models.TaskStatusReadyForReview
			if tc.testFile == "" {
				testhelpers.RequireErrorContains(t, err, "code tasks must include test files")
				wantStatus = models.TaskStatusImplementing
			} else if err != nil {
				t.Fatalf("C# test file must satisfy TDD admission without a waiver: %v", err)
			}
			updated, err := db.For(statePath).Read()
			if err != nil {
				t.Fatal(err)
			}
			if updated.Tasks[0].Status != wantStatus {
				t.Fatalf("status = %s, want %s", updated.Tasks[0].Status, wantStatus)
			}
			if tc.testFile == "" && !reflect.DeepEqual(updated.Tasks[0], state.Tasks[0]) {
				t.Fatal("rejected submission changed task state")
			}
			diagnostics, err := AnalyzeTestFiles(g, taskID, base, head)
			if err != nil {
				t.Fatal(err)
			}
			wantMatches := []string{}
			if tc.testFile != "" {
				wantMatches = append(wantMatches, tc.testFile)
			}
			if !reflect.DeepEqual(diagnostics.TestFilesMatched, wantMatches) {
				t.Fatalf("matched = %v, want %v", diagnostics.TestFilesMatched, wantMatches)
			}
			for _, pattern := range []string{"*Test.cs", "*Tests.cs"} {
				if !containsString(diagnostics.MatcherPatterns, pattern) {
					t.Errorf("diagnostics omit %s", pattern)
				}
			}
		})
	}
}
