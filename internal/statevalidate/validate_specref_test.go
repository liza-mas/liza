package statevalidate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidate_RejectsWorktreePrefixInTaskSpecRef(t *testing.T) {
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		{
			ID:          "task-1",
			Description: "Test task",
			Status:      models.TaskStatusReady,
			Priority:    1,
			Created:     now,
			SpecRef:     ".worktrees/code-planning-1/specs/plans/auth.md",
			DoneWhen:    "Done",
			Scope:       "test",
			Iteration:   1,
		},
	}

	err := validateTaskInvariants(state, t.TempDir(), true, nil, nil)
	if err == nil {
		t.Fatal("Expected error for worktree-prefixed spec_ref")
	}
	if !strings.Contains(err.Error(), "worktree prefix") {
		t.Errorf("Error = %q, want to contain 'worktree prefix'", err.Error())
	}
}

func TestValidate_RejectsWorktreePrefixInOutputSpecRef(t *testing.T) {
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Description:  "Plan task",
			Status:       models.TaskStatusCodingPlanApproved,
			Priority:     1,
			Created:      now,
			SpecRef:      "specs/plans/auth.md",
			DoneWhen:     "Plan approved",
			Scope:        "auth",
			Iteration:    1,
			ReviewCommit: testhelpers.StringPtr("abc123"),
			Output: []models.OutputEntry{
				{
					Desc:     "Implement login",
					DoneWhen: "POST /login works",
					Scope:    "auth",
					SpecRef:  ".worktrees/code-planning-1/specs/plans/auth.md#login",
				},
			},
		},
	}

	err := validateTaskInvariants(state, t.TempDir(), true, nil, nil)
	if err == nil {
		t.Fatal("Expected error for worktree-prefixed output spec_ref")
	}
	if !strings.Contains(err.Error(), "worktree prefix") {
		t.Errorf("Error = %q, want to contain 'worktree prefix'", err.Error())
	}
}

func TestValidate_RejectsWorktreePrefixInTaskPlanRef(t *testing.T) {
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		{
			ID:          "task-1",
			Description: "Test task",
			Status:      models.TaskStatusReady,
			Priority:    1,
			Created:     now,
			SpecRef:     "specs/auth.md",
			PlanRef:     ".worktrees/code-planning-1/specs/plans/plan.md",
			DoneWhen:    "Done",
			Scope:       "test",
			Iteration:   1,
		},
	}

	err := validateTaskInvariants(state, t.TempDir(), true, nil, nil)
	if err == nil {
		t.Fatal("Expected error for worktree-prefixed plan_ref")
	}
	if !strings.Contains(err.Error(), "plan_ref") && !strings.Contains(err.Error(), "worktree prefix") {
		t.Errorf("Error = %q, want to contain 'plan_ref' and 'worktree prefix'", err.Error())
	}
}

func TestValidate_RejectsWorktreePrefixInOutputPlanRef(t *testing.T) {
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Description:  "Plan task",
			Status:       models.TaskStatusCodingPlanApproved,
			Priority:     1,
			Created:      now,
			SpecRef:      "specs/plans/auth.md",
			DoneWhen:     "Plan approved",
			Scope:        "auth",
			Iteration:    1,
			ReviewCommit: testhelpers.StringPtr("abc123"),
			Output: []models.OutputEntry{
				{
					Desc:     "Implement login",
					DoneWhen: "POST /login works",
					Scope:    "auth",
					SpecRef:  "specs/plans/auth.md#login",
					PlanRef:  ".worktrees/code-planning-1/specs/plans/plan.md",
				},
			},
		},
	}

	err := validateTaskInvariants(state, t.TempDir(), true, nil, nil)
	if err == nil {
		t.Fatal("Expected error for worktree-prefixed output plan_ref")
	}
	if !strings.Contains(err.Error(), "plan_ref") && !strings.Contains(err.Error(), "worktree prefix") {
		t.Errorf("Error = %q, want to contain 'plan_ref' and 'worktree prefix'", err.Error())
	}
}

func TestValidate_AcceptsRepoRelativeSpecRef(t *testing.T) {
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Description:  "Plan task",
			Status:       models.TaskStatusCodingPlanApproved,
			Priority:     1,
			Created:      now,
			SpecRef:      "specs/plans/auth.md",
			DoneWhen:     "Plan approved",
			Scope:        "auth",
			Iteration:    1,
			ReviewCommit: testhelpers.StringPtr("abc123"),
			Output: []models.OutputEntry{
				{
					Desc:     "Implement login",
					DoneWhen: "POST /login works",
					Scope:    "auth",
					SpecRef:  "specs/plans/auth.md#login",
				},
			},
		},
	}

	err := validateTaskInvariants(state, t.TempDir(), true, nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error for repo-relative spec_ref: %v", err)
	}
}

// initGitRepo creates a temporary git repo with a single commit containing
// the given file. Returns the repo path. The file is committed on "main" and
// a branch named branchName is created pointing at that commit.
func initGitRepo(t *testing.T, branchName, filePath, content string) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	// Create file and commit
	fullPath := filepath.Join(dir, filePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	addCmd := exec.Command("git", "add", filePath)
	addCmd.Dir = dir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
	commitCmd := exec.Command("git", "commit", "-m", "initial")
	commitCmd.Dir = dir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}

	// Create the integration branch at this commit
	branchCmd := exec.Command("git", "branch", branchName)
	branchCmd.Dir = dir
	if out, err := branchCmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %v\n%s", err, out)
	}

	// Remove the file from the working tree (simulate different branch checkout)
	if err := os.Remove(fullPath); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestCheckSpecFileExists_GitFallback_FileOnBranch(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")

	err := checkSpecFileExists(repoDir, "specs/auth.md", "integration")
	if err != nil {
		t.Fatalf("Expected no error (file exists on integration branch), got: %v", err)
	}
}

func TestCheckSpecFileExists_GitFallback_FileOnBranchWithFragment(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")

	err := checkSpecFileExists(repoDir, "specs/auth.md#login", "integration")
	if err != nil {
		t.Fatalf("Expected no error (file with fragment exists on integration branch), got: %v", err)
	}
}

func TestCheckSpecFileExists_GitFallback_FileNotOnAnyBranch(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")

	err := checkSpecFileExists(repoDir, "specs/nonexistent.md", "integration")
	if err == nil {
		t.Fatal("Expected error for file not on any branch or filesystem")
	}
	if !strings.Contains(err.Error(), "spec_ref file not found") {
		t.Errorf("Error = %q, want to contain 'spec_ref file not found'", err.Error())
	}
}

func TestCheckArtifactRefFileExists_FieldSpecificDetails(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")

	err := checkArtifactRefFileExists(repoDir, "plan_ref", "specs/plans/missing.md", "integration", "task-1")
	if err == nil {
		t.Fatal("Expected error for missing plan_ref")
	}

	refErr, ok := err.(*ArtifactRefError)
	if !ok {
		t.Fatalf("error type = %T, want *ArtifactRefError", err)
	}
	if refErr.Field != "plan_ref" {
		t.Errorf("Field = %q, want plan_ref", refErr.Field)
	}
	if refErr.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want task-1", refErr.TaskID)
	}
	if refErr.Cause != artifactRefNotFoundCause {
		t.Errorf("Cause = %q, want %q", refErr.Cause, artifactRefNotFoundCause)
	}
	if strings.Contains(err.Error(), "spec_ref") {
		t.Errorf("Error = %q, should not mention spec_ref for plan_ref failure", err.Error())
	}
}

func TestValidateArtifactRefs_IgnoresRetiredTaskRefs(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")

	state := &models.State{
		Config: models.Config{IntegrationBranch: "integration"},
		Tasks: []models.Task{
			{
				ID:      "superseded-plan",
				Status:  models.TaskStatusSuperseded,
				PlanRef: "specs/plans/missing-superseded.md",
			},
			{
				ID:     "abandoned-output",
				Status: models.TaskStatusAbandoned,
				Output: []models.OutputEntry{
					{
						ArchRef: "specs/arch-plan/missing-abandoned.md",
					},
				},
			},
		},
	}

	if err := ValidateArtifactRefs(state, repoDir); err != nil {
		t.Fatalf("ValidateArtifactRefs() retired refs error = %v", err)
	}
}

func TestValidateArtifactRefs_MergedTaskMissingRefStillFails(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")

	state := &models.State{
		Config: models.Config{IntegrationBranch: "integration"},
		Tasks: []models.Task{
			{
				ID:      "merged-plan",
				Status:  models.TaskStatusMerged,
				PlanRef: "specs/plans/missing-merged.md",
			},
		},
	}

	err := ValidateArtifactRefs(state, repoDir)
	if err == nil {
		t.Fatal("Expected merged task missing plan_ref to fail")
	}
	if !strings.Contains(err.Error(), "plan_ref file not found") || !strings.Contains(err.Error(), "merged-plan") {
		t.Fatalf("error = %q, want merged task plan_ref failure", err.Error())
	}
}

func TestValidateTaskInvariants_IgnoresRetiredTaskArtifactRefs(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")
	now := time.Now().UTC()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusSuperseded, now)
	task.RescopeReason = testhelpers.StringPtr("replaced by self-contained successor")
	task.PlanRef = "specs/plans/missing-superseded.md"
	task.Output = []models.OutputEntry{
		{
			Desc:     "Retired output",
			DoneWhen: "No longer consumed",
			Scope:    "docs",
			SpecRef:  "specs/auth.md",
			ArchRef:  "specs/arch-plan/missing.md",
		},
	}

	if err := validateTaskInvariants(stateWithTasks(task), repoDir, false, nil, nil); err != nil {
		t.Fatalf("validateTaskInvariants() retired refs error = %v", err)
	}
}

func TestValidateArtifactRefScalarRejectsSemicolonJoinedRefs(t *testing.T) {
	err := ValidateArtifactRefScalar("spec_ref", "specs/a.md; specs/b.md#section", "task-1")
	if err == nil {
		t.Fatal("Expected error for semicolon-joined refs")
	}

	refErr, ok := err.(*ArtifactRefError)
	if !ok {
		t.Fatalf("error type = %T, want *ArtifactRefError", err)
	}
	if refErr.Cause != artifactRefMultipleRefsCause {
		t.Errorf("Cause = %q, want %q", refErr.Cause, artifactRefMultipleRefsCause)
	}
	if !strings.Contains(err.Error(), "multiple refs") {
		t.Errorf("Error = %q, want multiple refs message", err.Error())
	}
}

func TestValidateArtifactRefs_NormalizesCollectedRefsBeforeGitFallback(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")
	state := testhelpers.CreateValidState()
	state.Config.IntegrationBranch = "integration"
	state.Goal.SpecRef = ""
	state.Tasks = []models.Task{
		artifactRefTask("task-absolute", filepath.Join(repoDir, "specs", "auth.md")+"#login"),
	}

	if err := ValidateArtifactRefs(state, repoDir); err != nil {
		t.Fatalf("ValidateArtifactRefs returned error for absolute ref available on integration branch: %v", err)
	}
}

func TestValidateArtifactRefs_MissingArtifactsUseCollectorOwnerDiagnostics(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*models.State)
		wantField  string
		wantPath   string
		wantTaskID string
		wantOutput *int
	}{
		{
			name: "goal spec ref",
			configure: func(state *models.State) {
				state.Goal.SpecRef = "specs/missing-goal.md#intro"
			},
			wantField: "goal.spec_ref",
			wantPath:  "specs/missing-goal.md",
		},
		{
			name: "task artifact ref",
			configure: func(state *models.State) {
				state.Tasks = []models.Task{
					{
						ID:       "task-1",
						ArchRef:  "specs/missing-task-arch.md#design",
						Priority: 1,
					},
				}
			},
			wantField:  "arch_ref",
			wantPath:   "specs/missing-task-arch.md",
			wantTaskID: "task-1",
		},
		{
			name: "output artifact ref",
			configure: func(state *models.State) {
				state.Tasks = []models.Task{
					{
						ID:       "task-2",
						Priority: 1,
						Output: []models.OutputEntry{
							{
								PlanRef: "specs/missing-output-plan.md#plan",
							},
						},
					},
				}
			},
			wantField:  "output[0].plan_ref",
			wantPath:   "specs/missing-output-plan.md",
			wantTaskID: "task-2",
			wantOutput: intPtr(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Goal.SpecRef = ""
			state.Tasks = nil
			tt.configure(state)

			err := ValidateArtifactRefs(state, t.TempDir())
			if err == nil {
				t.Fatal("ValidateArtifactRefs returned nil error")
			}

			var refErr *ArtifactRefError
			if !errors.As(err, &refErr) {
				t.Fatalf("error type = %T, want *ArtifactRefError", err)
			}
			if refErr.Cause != artifactRefNotFoundCause {
				t.Errorf("Cause = %q, want %q", refErr.Cause, artifactRefNotFoundCause)
			}
			if refErr.Field != tt.wantField {
				t.Errorf("Field = %q, want %q", refErr.Field, tt.wantField)
			}
			if refErr.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", refErr.Path, tt.wantPath)
			}
			if refErr.TaskID != tt.wantTaskID {
				t.Errorf("TaskID = %q, want %q", refErr.TaskID, tt.wantTaskID)
			}
			if !sameOutputIndex(refErr.OutputIndex, tt.wantOutput) {
				t.Errorf("OutputIndex = %v, want %v", refErr.OutputIndex, tt.wantOutput)
			}
			for _, want := range []string{tt.wantField, tt.wantPath} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Error = %q, want to contain %q", err.Error(), want)
				}
			}
		})
	}
}

func TestValidateArtifactRefs_ReportsMissingArtifactInCollectorOrder(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "specs/z-missing-goal.md"
	state.Tasks = []models.Task{
		artifactRefTask("task-a", "specs/a-missing-task.md"),
	}

	err := ValidateArtifactRefs(state, t.TempDir())
	if err == nil {
		t.Fatal("ValidateArtifactRefs returned nil error")
	}

	var refErr *ArtifactRefError
	if !errors.As(err, &refErr) {
		t.Fatalf("error type = %T, want *ArtifactRefError", err)
	}
	if refErr.Path != "specs/a-missing-task.md" {
		t.Errorf("Path = %q, want first missing ref in sorted collector order", refErr.Path)
	}
	if refErr.TaskID != "task-a" {
		t.Errorf("TaskID = %q, want task-a", refErr.TaskID)
	}
}

func TestCheckSpecFileExists_GitFallback_EmptyBranch(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")

	// Empty integration branch → no git fallback, file not on disk → error
	err := checkSpecFileExists(repoDir, "specs/auth.md", "")
	if err == nil {
		t.Fatal("Expected error when integration branch is empty and file not on disk")
	}
}

func TestCheckSpecFileExists_GitFallback_AbsolutePathSkipsFallback(t *testing.T) {
	repoDir := initGitRepo(t, "integration", "specs/auth.md", "# Auth spec")

	// Absolute path that doesn't exist on disk — git fallback should be skipped
	absPath := filepath.Join(repoDir, "specs/auth.md") // file was removed by initGitRepo
	err := checkSpecFileExists(repoDir, absPath, "integration")
	if err == nil {
		t.Fatal("Expected error for absolute path not on disk (git fallback should be skipped)")
	}
}
