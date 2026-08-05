package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func withOpsBrandProjectDir(t *testing.T, projectDir string) {
	t.Helper()
	oldProjectDir := brand.ProjectDirName
	brand.ProjectDirName = projectDir
	t.Cleanup(func() {
		brand.ProjectDirName = oldProjectDir
	})
}

func TestCreateWorktree_Validation(t *testing.T) {
	_, err := CreateWorktree("/nonexistent", "", false)
	if err == nil {
		t.Fatal("Expected error for empty task ID")
	}
	if !strings.Contains(err.Error(), "task ID is required") {
		t.Errorf("Error = %q, want to contain 'task ID is required'", err.Error())
	}
}

func TestCreateWorktree_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := CreateWorktree(tmpDir, "nonexistent", false)
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestCreateWorktree_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := CreateWorktree(tmpDir, "task-1", false)
	if err == nil {
		t.Fatal("Expected error for non-executing task")
	}
	if !strings.Contains(err.Error(), "not in an executing state") {
		t.Errorf("Error = %q, want to contain 'not in an executing state'", err.Error())
	}
}

func TestCreateWorktree_CodePlanningStatus(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusCodePlanning, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() for CODE_PLANNING task: unexpected error: %v", err)
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
}

func TestCreateWorktree_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Create the worktree directory manually
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	if !result.AlreadyExisted {
		t.Error("AlreadyExisted should be true")
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
}

func TestCreateWorktree_CopyWorktreeEnvFilesBeforePostWorktreeCmd(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	commitEnvIgnoreForWorktreeTest(t, tmpDir)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "ROOT_ENV=1\n")
	writeRootFileForWorktreeTest(t, tmpDir, ".env.local", "LOCAL_ENV=1\n")
	writeRootFileForWorktreeTest(t, tmpDir, "app.env", "APP_ENV=1\n")
	writeRootFileForWorktreeTest(t, tmpDir, ".envrc", "export ROOT_ENV=1\n")
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	postCmd := "test -f .env && test -f .env.local && test -f app.env && test -f .envrc && touch ../post-worktree-saw-env"
	state := testhelpers.CreateValidState()
	state.Config.CopyWorktreeEnvFiles = true
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("CreateWorktree() warnings = %v, want none", result.Warnings)
	}

	for _, rel := range []string{".env", ".env.local", "app.env", ".envrc"} {
		assertWorktreeFileExists(t, result.WorktreeDir, rel)
		assertWorktreePathIgnored(t, result.WorktreeDir, rel)
	}
	assertWorktreeFileExists(t, filepath.Join(tmpDir, paths.WorktreesDirName), "post-worktree-saw-env")
	assertGitStatusClean(t, result.WorktreeDir)
}

func TestCreateWorktree_DoesNotCopyWorktreeEnvFilesByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	commitEnvIgnoreForWorktreeTest(t, tmpDir)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "ROOT_ENV=1\n")
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	assertWorktreeFileMissing(t, result.WorktreeDir, ".env")
	assertGitStatusClean(t, result.WorktreeDir)
}

func TestCreateWorktree_CopyWorktreeEnvFilesExistingWorktreeDoesNotOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	commitEnvIgnoreForWorktreeTest(t, tmpDir)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "SOURCE_ENV=1\n")
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	state.Config.CopyWorktreeEnvFiles = true
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	first, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("first CreateWorktree() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first.WorktreeDir, ".env"), []byte("WORKTREE_ENV=1\n"), 0o600); err != nil {
		t.Fatalf("overwrite worktree .env fixture: %v", err)
	}

	second, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("second CreateWorktree() error: %v", err)
	}
	if !second.AlreadyExisted {
		t.Fatal("second CreateWorktree() AlreadyExisted = false, want true")
	}
	got, err := os.ReadFile(filepath.Join(second.WorktreeDir, ".env"))
	if err != nil {
		t.Fatalf("ReadFile(worktree .env) error: %v", err)
	}
	if string(got) != "WORKTREE_ENV=1\n" {
		t.Fatalf("worktree .env = %q, want preserved existing content", got)
	}
	assertWorktreePathIgnored(t, second.WorktreeDir, ".env")
	assertGitStatusClean(t, second.WorktreeDir)
}

func TestProvisionWorktreeEnvFilesCandidateFilteringAndWarnings(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	commitEnvIgnoreForWorktreeTest(t, tmpDir)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "ROOT_ENV=1\n")
	writeRootFileForWorktreeTest(t, tmpDir, "credentials.local", "CREDENTIALS=1\n")
	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "config", ".env"), []byte("SUBDIR_ENV=1\n"), 0o600); err != nil {
		t.Fatalf("write subdir .env fixture: %v", err)
	}
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}
	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")

	warnings := ProvisionWorktreeEnvFiles(tmpDir, worktreeDir)
	if len(warnings) != 0 {
		t.Fatalf("ProvisionWorktreeEnvFiles() warnings = %v, want none", warnings)
	}

	assertWorktreeFileExists(t, worktreeDir, ".env")
	assertWorktreeFileMissing(t, worktreeDir, "credentials.local")
	assertWorktreeFileMissing(t, worktreeDir, filepath.Join("config", ".env"))
	assertGitStatusClean(t, worktreeDir)
}

func TestProvisionWorktreeEnvFilesRejectsUnsafeSources(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	commitEnvIgnoreForWorktreeTest(t, tmpDir)
	if err := os.Symlink("shared-env", filepath.Join(tmpDir, ".env")); err != nil {
		t.Fatalf("symlink .env fixture: %v", err)
	}
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}
	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")

	warnings := ProvisionWorktreeEnvFiles(tmpDir, worktreeDir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "source is not a regular file") {
		t.Fatalf("ProvisionWorktreeEnvFiles() warnings = %v, want non-regular source warning", warnings)
	}
	assertWorktreeFileMissing(t, worktreeDir, ".env")
	assertWorktreePrivateExcludeMissing(t, worktreeDir, ".env")
	assertGitStatusClean(t, worktreeDir)
}

func TestProvisionWorktreeEnvFilesWarnsForUnignoredSource(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "ROOT_ENV=1\n")
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}
	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")

	warnings := ProvisionWorktreeEnvFiles(tmpDir, worktreeDir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "source is not ignored by git") {
		t.Fatalf("ProvisionWorktreeEnvFiles() warnings = %v, want source ignore warning", warnings)
	}
	assertWorktreeFileMissing(t, worktreeDir, ".env")
	assertWorktreePrivateExcludeMissing(t, worktreeDir, ".env")
	assertGitStatusClean(t, worktreeDir)
}

func TestProvisionWorktreeEnvFilesWarnsForTrackedSource(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "TRACKED_ENV=1\n")
	testhelpers.MustGit(t, tmpDir, "add", ".env")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Track env file")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", "HEAD")
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}
	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")

	warnings := ProvisionWorktreeEnvFiles(tmpDir, worktreeDir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "source is not ignored by git") {
		t.Fatalf("ProvisionWorktreeEnvFiles() warnings = %v, want source ignore warning for tracked file", warnings)
	}
	assertWorktreePrivateExcludeMissing(t, worktreeDir, ".env")
	assertGitStatusClean(t, worktreeDir)
}

func TestProvisionWorktreeEnvFilesPrivateExcludeConflictWarningOnly(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	commitEnvIgnoreForWorktreeTest(t, tmpDir)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "ROOT_ENV=1\n")
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}
	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	conflictingExclude := filepath.Join(tmpDir, "other-exclude")
	testhelpers.MustGit(t, worktreeDir, "config", "core.excludesFile", conflictingExclude)

	warnings := ProvisionWorktreeEnvFiles(tmpDir, worktreeDir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "configure worktree ignore") {
		t.Fatalf("ProvisionWorktreeEnvFiles() warnings = %v, want private exclude conflict warning", warnings)
	}
	assertWorktreeFileMissing(t, worktreeDir, ".env")
	assertGitStatusClean(t, worktreeDir)
}

func TestCreateWorktreePreparesSembleIgnoreForFreshWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForCreateWorktreeScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	assertPrepareSembleIgnorePayload(t, result.WorktreeDir)
	assertPrepareSemblePrivateExcludeCount(t, result.WorktreeDir, ".sembleignore", 1)
	assertGitStatusClean(t, result.WorktreeDir)
}

func TestCreateWorktreePreparesSembleIgnoreForExistingWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForCreateWorktreeScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	if !result.AlreadyExisted {
		t.Fatal("AlreadyExisted = false, want true")
	}

	assertPrepareSembleIgnorePayload(t, result.WorktreeDir)
	assertPrepareSemblePrivateExcludeCount(t, result.WorktreeDir, ".sembleignore", 1)
	assertGitStatusClean(t, result.WorktreeDir)
}

func TestCreateWorktree_ScipIndexesEnabledNewWorktreeAfterSetup(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForCreateWorktreeScipTest(t, tmpDir)
	writeClaudeSettingsForCreateWorktreeScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	markerPath := filepath.Join(tmpDir, "post-worktree-ran")
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.ScipSearch = []string{"go"}
	postCmd := fmt.Sprintf("touch %s", markerPath)
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	var calls []scipsearch.RuntimeCommandPlan
	withCreateWorktreeScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		if _, err := os.Stat(filepath.Join(plan.Dir, ".claude", "settings.json")); err != nil {
			return "", fmt.Errorf("claude config not provisioned before indexing: %w", err)
		}
		if _, err := os.Stat(filepath.Join(plan.Dir, worktreeHooksDirName(), "pre-commit")); err != nil {
			return "", fmt.Errorf("pre-commit hook not installed before indexing: %w", err)
		}
		if _, err := os.Stat(markerPath); err != nil {
			return "", fmt.Errorf("post-worktree marker not present before indexing: %w", err)
		}
		calls = append(calls, plan)
		return writeCreateWorktreeScipIndex(plan, []byte(plan.Dir))
	})

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("CreateWorktree() warnings = %v, want none", result.Warnings)
	}
	if len(calls) != 2 || calls[0].Language != "go" || calls[1].Name != "scip-search" {
		t.Fatalf("indexer calls = %#v, want go indexer and aggregate calls", calls)
	}

	wantIndexPath := filepath.Join(result.WorktreeDir, paths.ProjectDirName(), "scip", "go.scip")
	indexes := availableCreateWorktreeScipIndexes(t, result.WorktreeDir, []string{"go"})
	if len(indexes) != 1 || indexes[0].Language != "go" || indexes[0].Path != wantIndexPath {
		t.Fatalf("AvailableIndexes() = %#v, want go index at %s", indexes, wantIndexPath)
	}
	if !filepath.IsAbs(indexes[0].Path) {
		t.Fatalf("index path %q is not absolute", indexes[0].Path)
	}
	assertGitStatusClean(t, result.WorktreeDir)
}

func TestCreateWorktree_ScipExistingWorktreeRefreshesIdempotently(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForCreateWorktreeScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.ScipSearch = []string{"go"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	call := 0
	var outputPaths []string
	withCreateWorktreeScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		call++
		outputPaths = append(outputPaths, plan.OutputPath)
		content := fmt.Sprintf("refresh-%d:%s", call, plan.Dir)
		return writeCreateWorktreeScipIndex(plan, []byte(content))
	})

	first, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("first CreateWorktree() error: %v", err)
	}
	second, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("second CreateWorktree() error: %v", err)
	}
	if !second.AlreadyExisted {
		t.Fatal("second CreateWorktree() AlreadyExisted = false, want true")
	}
	if call != 4 {
		t.Fatalf("indexer call count = %d, want 4", call)
	}
	wantPath := filepath.Join(first.WorktreeDir, paths.ProjectDirName(), "scip", "go.scip")
	if len(outputPaths) != 4 || outputPaths[1] == wantPath || outputPaths[3] == wantPath ||
		!strings.HasSuffix(outputPaths[1], "go-aggregate.scip") || !strings.HasSuffix(outputPaths[3], "go-aggregate.scip") {
		t.Fatalf("output paths = %#v, want repeated temporary aggregate refresh before atomic rename to %s", outputPaths, wantPath)
	}
	content, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error: %v", wantPath, err)
	}
	wantContent := "refresh-4:" + first.WorktreeDir
	if string(content) != wantContent {
		t.Fatalf("index content = %q, want %q", content, wantContent)
	}
	assertCreateWorktreeScipExcludeCount(t, first.WorktreeDir, 1)
	assertGitStatusClean(t, first.WorktreeDir)
}

func TestCreateWorktree_ScipDisabledActivationNoop(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForCreateWorktreeScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.ScipSearch = []string{"go"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	withCreateWorktreeScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		t.Fatalf("unexpected indexer call when scip-search activation is disabled: %#v", plan)
		return "", nil
	})

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("CreateWorktree() warnings = %v, want none", result.Warnings)
	}
	if _, err := os.Stat(filepath.Join(result.WorktreeDir, paths.ProjectDirName(), "scip")); !os.IsNotExist(err) {
		t.Fatalf("%s/scip stat error = %v, want not exist", paths.ProjectDirName(), err)
	}
	if indexes := availableCreateWorktreeScipIndexes(t, result.WorktreeDir, []string{"go"}); len(indexes) != 0 {
		t.Fatalf("AvailableIndexes() = %#v, want none", indexes)
	}
}

func TestCreateWorktree_StacklitIndexesEnabledNewWorktreeAfterSetup(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForCreateWorktreeScipTest(t, tmpDir)
	commitTrackedStacklitForCreateWorktreeTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "true")

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	var calls []stacklit.RuntimeCommandPlan
	withCreateWorktreeStacklitRuntimeRunner(t, func(plan stacklit.RuntimeCommandPlan) (string, error) {
		calls = append(calls, plan)
		if err := os.WriteFile(plan.OutputPath, []byte(plan.Dir), 0o644); err != nil {
			return "", err
		}
		return "", nil
	})

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("CreateWorktree() warnings = %v, want none", result.Warnings)
	}
	if len(calls) != 1 {
		t.Fatalf("stacklit calls = %#v, want one call", calls)
	}
	wantIndexPath := filepath.Join(result.WorktreeDir, "stacklit.json")
	if calls[0].OutputPath != wantIndexPath {
		t.Fatalf("stacklit output path = %q, want %q", calls[0].OutputPath, wantIndexPath)
	}
	if got := string(readCreateWorktreeFile(t, wantIndexPath)); got != result.WorktreeDir {
		t.Fatalf("stacklit.json content = %q, want worktree dir", got)
	}
	if status := runGitInDir(t, result.WorktreeDir, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
	if ls := runGitInDir(t, result.WorktreeDir, "ls-files", "-v", "stacklit.json"); !strings.HasPrefix(ls, "S ") {
		t.Fatalf("git ls-files -v stacklit.json = %q, want skip-worktree marker", ls)
	}
}

func TestCreateWorktree_ScipFailedIndexerWarningOnly(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForCreateWorktreeScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.ScipSearch = []string{"go"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	withCreateWorktreeScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		return "indexer stderr", fmt.Errorf("boom")
	})

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() should succeed on indexer failure, got: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "scip-search go:") || !strings.Contains(result.Warnings[0], "boom") {
		t.Fatalf("CreateWorktree() warnings = %v, want scip-search go warning with diagnostic", result.Warnings)
	}
	if indexes := availableCreateWorktreeScipIndexes(t, result.WorktreeDir, []string{"go"}); len(indexes) != 0 {
		t.Fatalf("AvailableIndexes() = %#v, want none after failed indexer", indexes)
	}
	assertGitStatusClean(t, result.WorktreeDir)
}

func TestCreateWorktree_ScipConcurrentCreatesUseIsolatedIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForCreateWorktreeScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.ScipSearch = []string{"go"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
		testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	var mu sync.Mutex
	outputs := map[string]string{}
	withCreateWorktreeScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		if plan.Name == "scip-search" {
			finalPath := filepath.Join(plan.Dir, paths.ProjectDirName(), "scip", plan.Language+".scip")
			mu.Lock()
			outputs[finalPath] = plan.Dir
			mu.Unlock()
		}
		return writeCreateWorktreeScipIndex(plan, []byte(plan.Dir))
	})

	type createOutcome struct {
		result *CreateWorktreeResult
		err    error
	}
	results := make(chan createOutcome, 2)
	for _, taskID := range []string{"task-1", "task-2"} {
		taskID := taskID
		go func() {
			result, err := CreateWorktree(tmpDir, taskID, false)
			results <- createOutcome{result: result, err: err}
		}()
	}

	for range 2 {
		outcome := <-results
		if outcome.err != nil {
			t.Fatalf("CreateWorktree() concurrent create error: %v", outcome.err)
		}
		if len(outcome.result.Warnings) != 0 {
			t.Fatalf("CreateWorktree() warnings = %v, want none", outcome.result.Warnings)
		}
	}

	task1Index := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1", paths.ProjectDirName(), "scip", "go.scip")
	task2Index := filepath.Join(tmpDir, paths.WorktreesDirName, "task-2", paths.ProjectDirName(), "scip", "go.scip")
	if task1Index == task2Index {
		t.Fatal("concurrent creates produced identical index paths")
	}
	for _, indexPath := range []string{task1Index, task2Index} {
		if !filepath.IsAbs(indexPath) {
			t.Fatalf("index path %q is not absolute", indexPath)
		}
		content, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error: %v", indexPath, err)
		}
		mu.Lock()
		want := outputs[indexPath]
		mu.Unlock()
		if string(content) != want {
			t.Fatalf("index %s content = %q, want its own worktree dir %q", indexPath, content, want)
		}
	}
	if task1Content, err := os.ReadFile(task1Index); err != nil {
		t.Fatalf("ReadFile(%s) error: %v", task1Index, err)
	} else if task2Content, err := os.ReadFile(task2Index); err != nil {
		t.Fatalf("ReadFile(%s) error: %v", task2Index, err)
	} else if string(task1Content) == string(task2Content) {
		t.Fatalf("concurrent creates shared output content: %q", task1Content)
	}
	assertGitStatusClean(t, filepath.Join(tmpDir, paths.WorktreesDirName, "task-1"))
	assertGitStatusClean(t, filepath.Join(tmpDir, paths.WorktreesDirName, "task-2"))
}

func TestCreateWorktree_ExistingWorktreeWithUnresolvableHEADFails(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")
	worktreeDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	gitLinkPath := filepath.Join(worktreeDir, ".git")
	gitLink, err := os.ReadFile(gitLinkPath)
	if err != nil {
		t.Fatalf("failed to read worktree .git link: %v", err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(gitLink)), "gitdir: ")
	if !ok {
		t.Fatalf("unexpected .git link contents: %q", string(gitLink))
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreeDir, gitDir)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/task/missing-head\n"), 0644); err != nil {
		t.Fatalf("failed to corrupt worktree HEAD: %v", err)
	}

	_, err = CreateWorktree(tmpDir, "task-1", false)
	if err == nil {
		t.Fatal("CreateWorktree() should reject an existing worktree whose HEAD cannot resolve")
	}
	if !strings.Contains(err.Error(), "existing worktree not healthy") || !strings.Contains(err.Error(), "HEAD") {
		t.Errorf("CreateWorktree() error = %v, want existing worktree health and HEAD details", err)
	}
}

// TestCreateWorktree_InstallsPreCommitHook covers the post-submit commit guard:
// after wt-create, the worktree must have the liza pre-commit hook wired up
// via extensions.worktreeConfig + --worktree core.hooksPath. Without all three
// pieces (extension on main, hooks file, per-worktree config) git would silently
// fall back to the main repo's hooks and the guard would never fire.
func TestCreateWorktree_InstallsPreCommitHook(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	// 1. Hook file exists at the expected path and is executable.
	hookPath := filepath.Join(result.WorktreeDir, worktreeHooksDirName(), "pre-commit")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("pre-commit hook not installed at %s: %v", hookPath, err)
	}
	testhelpers.AssertExecutableScript(t, hookPath)

	// 2. Main repo has extensions.worktreeConfig=true.
	ext := runGitInDir(t, tmpDir, "config", "--get", "extensions.worktreeConfig")
	if ext != "true" {
		t.Errorf("extensions.worktreeConfig = %q, want %q (required for per-worktree core.hooksPath)", ext, "true")
	}

	// 3. Worktree has core.hooksPath pointing at the installed dir.
	hooksAbs, err := filepath.Abs(filepath.Join(result.WorktreeDir, worktreeHooksDirName()))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	// EvalSymlinks because tmp dirs on macOS go through /var → /private/var.
	wantHooksAbs, err := filepath.EvalSymlinks(hooksAbs)
	if err != nil {
		wantHooksAbs = hooksAbs
	}
	got := runGitInDir(t, result.WorktreeDir, "config", "--worktree", "--get", "core.hooksPath")
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		gotResolved = got
	}
	if gotResolved != wantHooksAbs {
		t.Errorf("core.hooksPath = %q, want %q", got, hooksAbs)
	}
}

func TestCreateWorktree_InstallsBrandedHookDirectory(t *testing.T) {
	withOpsBrandProjectDir(t, ".acme")
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	hookPath := filepath.Join(result.WorktreeDir, ".acme-hooks", "pre-commit")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("pre-commit hook not installed at branded path %s: %v", hookPath, err)
	}
	if _, err := os.Stat(filepath.Join(result.WorktreeDir, ".liza-hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("default hook path exists or stat failed unexpectedly: %v", err)
	}
}

// TestCreateWorktree_InstallsHookOnExisting verifies the upgrade path: a
// pre-hook-era worktree picks up the hook on the next wt-create without
// requiring fresh=true.
func TestCreateWorktree_InstallsHookOnExisting(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
	testhelpers.WriteInitialState(t, stateFile, state)

	// First call creates the worktree and installs the hook.
	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("first CreateWorktree() error: %v", err)
	}

	// Simulate a pre-hook-era worktree by deleting the hook file.
	hookPath := filepath.Join(result.WorktreeDir, worktreeHooksDirName(), "pre-commit")
	if err := os.RemoveAll(filepath.Join(result.WorktreeDir, worktreeHooksDirName())); err != nil {
		t.Fatalf("setup: remove hooks dir: %v", err)
	}

	// Second call on the existing worktree must re-install.
	result2, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("second CreateWorktree() error: %v", err)
	}
	if !result2.AlreadyExisted {
		t.Fatal("expected AlreadyExisted=true on the upgrade path")
	}

	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("hook not re-installed on AlreadyExisted path: %v", err)
	}
}

// TestCreateWorktree_HookFiresAndRejects is the end-to-end guard:
// it verifies the hook is actually invoked by git (the whole point of the
// extensions.worktreeConfig + core.hooksPath dance) and rejects commits when
// the task is in a non-executing state. This would have caught the earlier
// P0 bug where the hook was installed under .git/worktrees/<id>/hooks/ but
// git never looked there.
//
// The hook is rendered with an inert "liza" binary path; we stub a shell
// script at that path returning exit 1 so the hook rejects unconditionally,
// then confirm git commit rejects. A second pass with the stub returning 0
// confirms the hook path is otherwise permissive.
func TestCreateWorktree_HookFiresAndRejects(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	// Overwrite the installed hook with a deterministic rejector so we're
	// testing the hook-invocation plumbing, not CheckCommitAllowed's logic
	// (which has its own unit tests).
	hookPath := filepath.Join(result.WorktreeDir, worktreeHooksDirName(), "pre-commit")
	rejector := "#!/bin/sh\necho 'liza-test-reject' 1>&2\nexit 1\n"
	if err := os.WriteFile(hookPath, []byte(rejector), 0755); err != nil {
		t.Fatalf("write rejector: %v", err)
	}

	// Configure commit identity inside the worktree.
	runGitInDir(t, result.WorktreeDir, "config", "user.email", "test@example.com")
	runGitInDir(t, result.WorktreeDir, "config", "user.name", "Test User")

	// Attempt an empty commit — hook must fire and reject.
	cmd := exec.Command("git", "-C", result.WorktreeDir, "commit", "--allow-empty", "-m", "should-fail")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("git commit succeeded but hook should have rejected. Output:\n%s", out)
	}
	if !strings.Contains(string(out), "liza-test-reject") {
		t.Errorf("hook output missing — git didn't invoke our hook. Output:\n%s", out)
	}

	// --no-verify must bypass, proving the hook is the thing that blocked.
	cmd = exec.Command("git", "-C", result.WorktreeDir, "commit", "--allow-empty", "--no-verify", "-m", "bypass-ok")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("--no-verify should bypass the hook: %v\n%s", err, out)
	}

	// Swap the hook to an allower and confirm a subsequent commit succeeds,
	// ruling out "hook rejects regardless of content" false positives.
	allower := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(hookPath, []byte(allower), 0755); err != nil {
		t.Fatalf("write allower: %v", err)
	}
	cmd = exec.Command("git", "-C", result.WorktreeDir, "commit", "--allow-empty", "-m", "should-pass")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("allower hook should have allowed commit: %v\n%s", err, out)
	}
}

// TestHookShellFailSafeOnUnknownExitCode proves the shell hook enforces the
// fail-safe-allow contract at the shell boundary, not just inside the Go CLI.
// A stub "liza" that exits with a non-policy code (e.g. 127 "command not
// found", 139 "segfault", 2 "panic") must be interpreted as allow by the
// hook wrapper — otherwise a crashing or upgraded-out-of-sync binary would
// deadlock every commit in a worktree.
func TestHookShellFailSafeOnUnknownExitCode(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	// Overwrite the installed hook with a re-render of the SHIPPED template
	// (not a handwritten copy) pointing at a stub "liza" that exits 127.
	// Using the real embedded template means this test protects the
	// in-repo script: if someone deletes the case statement, this test
	// fails.
	hookPath := filepath.Join(result.WorktreeDir, worktreeHooksDirName(), "pre-commit")
	stubBin := filepath.Join(tmpDir, "stub-liza")
	if err := os.WriteFile(stubBin, []byte("#!/bin/sh\nexit 127\n"), 0755); err != nil {
		t.Fatalf("write stub liza: %v", err)
	}
	renderedHook := embedded.RenderWorktreePreCommitHook(stubBin, "task-1")
	if err := os.WriteFile(hookPath, renderedHook, 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	runGitInDir(t, result.WorktreeDir, "config", "user.email", "test@example.com")
	runGitInDir(t, result.WorktreeDir, "config", "user.name", "Test User")

	// Stub exits 127 → hook translates to exit 0 → git allows the commit.
	cmd := exec.Command("git", "-C", result.WorktreeDir, "commit", "--allow-empty", "-m", "stub-127-should-allow")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("commit should succeed when stub liza exits 127 (fail-safe allow), got:\n%s\n%v", out, err)
	}
}

// runGitInDir is a test helper for asserting git config state. Returns the
// trimmed stdout of `git -C dir <args...>`.
func runGitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(out))
}

// setupChainTestWorktree creates a worktree wired to a stub liza binary that
// exits with lizaExitCode. The hook is the real rendered template, so tests
// exercise the in-repo script. Returns the worktree dir.
func setupChainTestWorktree(t *testing.T, lizaExitCode int) string {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CreateWorktree(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	stubBin := filepath.Join(tmpDir, "stub-liza")
	stubScript := fmt.Sprintf("#!/bin/sh\nexit %d\n", lizaExitCode)
	if err := os.WriteFile(stubBin, []byte(stubScript), 0755); err != nil {
		t.Fatalf("write stub liza: %v", err)
	}
	hookPath := filepath.Join(result.WorktreeDir, worktreeHooksDirName(), "pre-commit")
	if err := os.WriteFile(hookPath, embedded.RenderWorktreePreCommitHook(stubBin, "task-1"), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	runGitInDir(t, result.WorktreeDir, "config", "user.email", "test@example.com")
	runGitInDir(t, result.WorktreeDir, "config", "user.name", "Test User")

	return result.WorktreeDir
}

// makeIsolatedPath creates a directory with `git` symlinked in (so the hook's
// own `git rev-parse` works) and optionally a `pre-commit` stub. Returns the
// dir, intended for use as PATH on the commit invocation. The stub touches
// markerFile when invoked and exits with preCommitExit.
func makeIsolatedPath(t *testing.T, withPreCommit bool, preCommitExit int, markerFile string) string {
	t.Helper()
	dir := t.TempDir()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	if err := os.Symlink(gitBin, filepath.Join(dir, "git")); err != nil {
		t.Fatalf("symlink git: %v", err)
	}
	if withPreCommit {
		// Shell-builtin-only: `:` is a no-op builtin, `> file` is redirection.
		// Avoids external `touch` which wouldn't be on the restricted PATH.
		// Echo to stderr surfaces invocation in CombinedOutput when debugging.
		script := fmt.Sprintf("#!/bin/sh\necho 'pre-commit-stub-invoked' >&2\n: > %q\nexit %d\n", markerFile, preCommitExit)
		if err := os.WriteFile(filepath.Join(dir, "pre-commit"), []byte(script), 0755); err != nil {
			t.Fatalf("write pre-commit stub: %v", err)
		}
	}
	return dir
}

// commitInIsolatedPath runs `git commit --allow-empty` in worktreeDir with
// PATH restricted to isolatedPath, so the hook sees a controlled binary set.
func commitInIsolatedPath(t *testing.T, worktreeDir, isolatedPath, message string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("git", "-C", worktreeDir, "commit", "--allow-empty", "-m", message)
	cmd.Env = append(os.Environ(), "PATH="+isolatedPath)
	return cmd.CombinedOutput()
}

// writePreCommitConfig drops a minimal .pre-commit-config.yaml into the
// worktree. Content is irrelevant to the chain — only file presence matters.
func writePreCommitConfig(t *testing.T, worktreeDir string) {
	t.Helper()
	path := filepath.Join(worktreeDir, ".pre-commit-config.yaml")
	if err := os.WriteFile(path, []byte("repos: []\n"), 0644); err != nil {
		t.Fatalf("write pre-commit config: %v", err)
	}
}

// TestHook_ChainsToProjectPreCommit_WhenConfigPresent covers spec acceptance
// criteria 1 and 4: when both the Liza guard and a project pre-commit config
// are in play, the hook chains through and project pre-commit fires.
func TestHook_ChainsToProjectPreCommit_WhenConfigPresent(t *testing.T) {
	worktreeDir := setupChainTestWorktree(t, 0) // guard allows
	writePreCommitConfig(t, worktreeDir)

	marker := filepath.Join(t.TempDir(), "invoked")
	isolatedPath := makeIsolatedPath(t, true, 0, marker)

	out, err := commitInIsolatedPath(t, worktreeDir, isolatedPath, "chain-runs")
	if err != nil {
		t.Fatalf("commit failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("pre-commit stub was not invoked (marker %q missing): %v\noutput:\n%s", marker, err, out)
	}
}

// TestHook_NoChainWhenConfigAbsent covers acceptance criterion 2: with no
// .pre-commit-config.yaml in the worktree, the hook does not invoke
// pre-commit at all.
func TestHook_NoChainWhenConfigAbsent(t *testing.T) {
	worktreeDir := setupChainTestWorktree(t, 0) // guard allows

	marker := filepath.Join(t.TempDir(), "invoked")
	isolatedPath := makeIsolatedPath(t, true, 0, marker)

	out, err := commitInIsolatedPath(t, worktreeDir, isolatedPath, "no-chain")
	if err != nil {
		t.Fatalf("commit failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("pre-commit stub was invoked despite missing config\noutput:\n%s", out)
	}
}

// TestHook_FailLoudOnMissingPreCommitBinary covers acceptance criterion 3:
// when a config is present but pre-commit is not installed, the hook fails
// loudly rather than silently skipping.
func TestHook_FailLoudOnMissingPreCommitBinary(t *testing.T) {
	worktreeDir := setupChainTestWorktree(t, 0) // guard allows
	writePreCommitConfig(t, worktreeDir)

	isolatedPath := makeIsolatedPath(t, false, 0, "") // no pre-commit stub

	out, err := commitInIsolatedPath(t, worktreeDir, isolatedPath, "missing-binary")
	if err == nil {
		t.Fatalf("commit should have failed, got success:\n%s", out)
	}
	if !strings.Contains(string(out), "pre-commit binary not installed") {
		t.Errorf("expected loud error in stderr, got:\n%s", out)
	}
}

// TestHook_GuardRejectShortCircuitsChain proves the Liza guard's reject
// short-circuits before project pre-commit runs — the guard is authoritative
// for task-state policy regardless of config presence.
func TestHook_GuardRejectShortCircuitsChain(t *testing.T) {
	worktreeDir := setupChainTestWorktree(t, 1) // guard rejects
	writePreCommitConfig(t, worktreeDir)

	marker := filepath.Join(t.TempDir(), "invoked")
	isolatedPath := makeIsolatedPath(t, true, 0, marker)

	out, err := commitInIsolatedPath(t, worktreeDir, isolatedPath, "guard-wins")
	if err == nil {
		t.Fatalf("commit should have been rejected by guard, got success:\n%s", out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("pre-commit stub ran despite guard rejection\noutput:\n%s", out)
	}
}

// TestHook_ProjectPreCommitFailureBlocksCommit proves project pre-commit's
// real exit code propagates: a non-zero pre-commit exit blocks the commit.
func TestHook_ProjectPreCommitFailureBlocksCommit(t *testing.T) {
	worktreeDir := setupChainTestWorktree(t, 0) // guard allows
	writePreCommitConfig(t, worktreeDir)

	marker := filepath.Join(t.TempDir(), "invoked")
	isolatedPath := makeIsolatedPath(t, true, 1, marker) // pre-commit fails

	out, err := commitInIsolatedPath(t, worktreeDir, isolatedPath, "pre-commit-fails")
	if err == nil {
		t.Fatalf("commit should have failed when pre-commit exits 1, got success:\n%s", out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("pre-commit stub was not invoked: %v\noutput:\n%s", err, out)
	}
}

// TestHook_FailSafeOnUnknownGuardExitFallsThroughToChain covers acceptance
// criterion 5: an unknown guard exit code (e.g. 127, stale binary) is treated
// as allow, but the project pre-commit chain still runs — preserving the
// fail-safe asymmetry. This complements TestHookShellFailSafeOnUnknownExitCode
// which covers the no-config flavor of the same property.
func TestHook_FailSafeOnUnknownGuardExitFallsThroughToChain(t *testing.T) {
	worktreeDir := setupChainTestWorktree(t, 127) // guard exits 127
	writePreCommitConfig(t, worktreeDir)

	marker := filepath.Join(t.TempDir(), "invoked")
	isolatedPath := makeIsolatedPath(t, true, 0, marker)

	out, err := commitInIsolatedPath(t, worktreeDir, isolatedPath, "fail-safe-chains")
	if err != nil {
		t.Fatalf("commit should succeed (guard fail-safe + pre-commit allows): %v\n%s", err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("pre-commit stub was not invoked despite fail-safe fall-through: %v\noutput:\n%s", err, out)
	}
}

func withCreateWorktreeScipRuntimeRunner(t *testing.T, runner scipsearch.RuntimeRunner) {
	t.Helper()
	scipRuntimeRunnerMu.Lock()
	previous := scipRuntimeRunner
	scipRuntimeRunner = runner
	scipRuntimeRunnerMu.Unlock()

	t.Cleanup(func() {
		scipRuntimeRunnerMu.Lock()
		scipRuntimeRunner = previous
		scipRuntimeRunnerMu.Unlock()
	})
}

func withCreateWorktreeStacklitRuntimeRunner(t *testing.T, runner stacklit.RuntimeRunner) {
	t.Helper()
	stacklitRuntimeRunnerMu.Lock()
	previous := stacklitRuntimeRunner
	stacklitRuntimeRunner = runner
	stacklitRuntimeRunnerMu.Unlock()

	t.Cleanup(func() {
		stacklitRuntimeRunnerMu.Lock()
		stacklitRuntimeRunner = previous
		stacklitRuntimeRunnerMu.Unlock()
	})
}

func addTrackedGoSourceForCreateWorktreeScipTest(t *testing.T, projectRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectRoot, ".gitignore"), []byte(".claude/\n"+worktreeHooksDirName()+"/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/scipcreate\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(go.mod) error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go) error: %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", ".gitignore", "go.mod", "main.go")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add Go source")
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
}

func commitTrackedStacklitForCreateWorktreeTest(t *testing.T, projectRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectRoot, "stacklit.json"), []byte(`{"project":{"name":"test"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stacklit.json) error: %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", "stacklit.json")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add stacklit index")
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
}

func writeClaudeSettingsForCreateWorktreeScipTest(t *testing.T, projectRoot string) {
	t.Helper()
	settingsDir := filepath.Join(projectRoot, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.claude/settings.json) error: %v", err)
	}
}

func readCreateWorktreeFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error: %v", path, err)
	}
	return content
}

func writeCreateWorktreeScipIndex(plan scipsearch.RuntimeCommandPlan, content []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(plan.OutputPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(plan.OutputPath, content, 0o644); err != nil {
		return "", err
	}
	return "", nil
}

func availableCreateWorktreeScipIndexes(t *testing.T, worktreeDir string, languages []string) []scipsearch.IndexRef {
	t.Helper()
	indexes, err := scipsearch.AvailableIndexes(scipsearch.RuntimePlanOptions{
		TargetRoot:          worktreeDir,
		ConfiguredLanguages: languages,
	})
	if err != nil {
		t.Fatalf("AvailableIndexes() error: %v", err)
	}
	return indexes
}

func assertCreateWorktreeScipExcludeCount(t *testing.T, worktreeDir string, want int) {
	t.Helper()
	gitDir := runGitInDir(t, worktreeDir, "rev-parse", "--git-dir")
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreeDir, gitDir)
	}
	content, err := os.ReadFile(filepath.Join(gitDir, "info", "exclude"))
	if err != nil {
		t.Fatalf("ReadFile(worktree exclude) error: %v", err)
	}
	if got := strings.Count(string(content), paths.ProjectDirName()+"/scip/"); got != want {
		t.Fatalf("worktree exclude contains %s/scip/ %d times, want %d; content: %q", paths.ProjectDirName(), got, want, content)
	}
}

func commitEnvIgnoreForWorktreeTest(t *testing.T, projectRoot string) {
	t.Helper()
	ignore := ".env\n.env.*\n*.env\n.envrc\n" + worktreeHooksDirName() + "/\n"
	if err := os.WriteFile(filepath.Join(projectRoot, ".gitignore"), []byte(ignore), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error: %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", ".gitignore")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Ignore env files")
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
}

func writeRootFileForWorktreeTest(t *testing.T, projectRoot, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectRoot, rel), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error: %v", rel, err)
	}
}

func assertWorktreePathIgnored(t *testing.T, worktreeDir, rel string) {
	t.Helper()
	cmd := exec.Command("git", "-C", worktreeDir, "check-ignore", "--quiet", "--", rel)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git check-ignore %s in %s failed: %v: %s", rel, worktreeDir, err, output)
	}
}

func assertWorktreePrivateExcludeMissing(t *testing.T, worktreeDir, rel string) {
	t.Helper()
	gitDir := runGitInDir(t, worktreeDir, "rev-parse", "--git-dir")
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreeDir, gitDir)
	}
	content, err := os.ReadFile(filepath.Join(gitDir, "info", "exclude"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("ReadFile(worktree private exclude) error: %v", err)
	}
	if strings.Contains(string(content), rel) {
		t.Fatalf("worktree private exclude contains %q, want no mutation; content: %q", rel, content)
	}
}

func assertWorktreeFileExists(t *testing.T, worktreeDir, rel string) {
	t.Helper()
	path := filepath.Join(worktreeDir, rel)
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("Lstat(%s) error: %v", path, err)
	}
}

func assertWorktreeFileMissing(t *testing.T, worktreeDir, rel string) {
	t.Helper()
	path := filepath.Join(worktreeDir, rel)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("Lstat(%s) error = %v, want not exist", path, err)
	}
}

// CreateWorktree must not report a worktree as ready when its configured setup
// command failed — on both the fresh and the already-existing path.
func TestCreateWorktree_PostWorktreeCmdFailureFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		preCreate  bool
		wantExists bool
	}{
		{name: "fresh worktree", preCreate: false},
		{name: "existing worktree", preCreate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			t.Setenv(stacklit.EnvEnableStacklit, "false")

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			postCmd := "echo setup-broke >&2; exit 3"
			state.Config.PostWorktreeCmd = &postCmd
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			if tc.preCreate {
				if _, err := git.New(tmpDir).CreateWorktree("task-1", "integration"); err != nil {
					t.Fatalf("Failed to pre-create worktree: %v", err)
				}
			}

			result, err := CreateWorktree(tmpDir, "task-1", false)
			if err == nil {
				t.Fatalf("CreateWorktree() error = nil, want setup failure; result = %+v", result)
			}
			var setupErr *PostWorktreeSetupError
			if !stderrors.As(err, &setupErr) {
				t.Fatalf("CreateWorktree() error = %v, want *PostWorktreeSetupError", err)
			}
			if setupErr.Cmd != postCmd {
				t.Errorf("setupErr.Cmd = %q, want %q", setupErr.Cmd, postCmd)
			}
		})
	}
}
