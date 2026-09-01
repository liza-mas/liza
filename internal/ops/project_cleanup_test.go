package ops

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gitops "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestProjectCleanupPlanAndExecuteRemovesOwnedTargets(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	lp := paths.New(projectRoot)
	if err := os.MkdirAll(lp.LizaDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lp.LizaDir(), "old-state"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	gitClient := gitops.New(projectRoot)
	if _, err := gitClient.CreateWorktree("old-task", "HEAD"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	plan, err := PlanProjectCleanup(projectRoot)
	if err != nil {
		t.Fatalf("PlanProjectCleanup() error = %v", err)
	}
	if len(plan.Directories) != 2 || len(plan.Worktrees) != 1 {
		t.Fatalf("cleanup plan = %+v, want two directories and one worktree", plan)
	}
	if plan.Worktrees[0].Branch != paths.TaskBranchPrefix+"old-task" {
		t.Fatalf("cleanup branch = %q", plan.Worktrees[0].Branch)
	}

	if err := ExecuteProjectCleanup(plan); err != nil {
		t.Fatalf("ExecuteProjectCleanup() error = %v", err)
	}
	for _, dir := range []string{lp.LizaDir(), filepath.Join(projectRoot, paths.WorktreesDirName)} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("cleanup directory %s still exists or cannot be checked: %v", dir, err)
		}
	}
	exists, err := gitClient.BranchExists(paths.TaskBranchPrefix + "old-task")
	if err != nil {
		t.Fatalf("BranchExists() error = %v", err)
	}
	if exists {
		t.Fatal("associated task branch still exists after cleanup")
	}
}

func TestPlanProjectCleanupRejectsUnownedRegisteredWorktree(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	worktreePath := filepath.Join(projectRoot, paths.WorktreesDirName, "not owned")
	testhelpers.MustGit(t, projectRoot, "worktree", "add", "-b", "foreign-cleanup", worktreePath, "HEAD")
	marker := filepath.Join(worktreePath, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := PlanProjectCleanup(projectRoot)
	if err == nil || !strings.Contains(err.Error(), "invalid task directory") {
		t.Fatalf("PlanProjectCleanup() error = %v, want invalid task directory", err)
	}
	if !strings.Contains(err.Error(), "relocate or unregister") {
		t.Fatalf("PlanProjectCleanup() error = %v, want actionable remedy", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("planning changed unowned worktree: %v", statErr)
	}
}

func TestPlanProjectCleanupResolvesRegisteredWorktreeSymlinks(t *testing.T) {
	baseDir := t.TempDir()
	projectRoot := filepath.Join(baseDir, "project")
	if err := os.Mkdir(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}
	testhelpers.SetupTestGitRepo(t, projectRoot)

	aliasRoot := filepath.Join(baseDir, "project-alias")
	if err := os.Symlink(projectRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	gitClient := gitops.New(projectRoot)
	if _, err := gitClient.CreateWorktree("symlink-task", "HEAD"); err != nil {
		t.Fatal(err)
	}

	aliasGitDir := filepath.Join(aliasRoot, paths.WorktreesDirName, "symlink-task", paths.GitDirName)
	metadataGitDir := filepath.Join(projectRoot, paths.GitDirName, "worktrees", "symlink-task", "gitdir")
	if err := os.WriteFile(metadataGitDir, []byte(filepath.ToSlash(aliasGitDir)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	registered, err := gitClient.ListWorktrees()
	if err != nil {
		t.Fatal(err)
	}
	var recordedAlias bool
	for _, worktree := range registered {
		if worktree.Branch == paths.TaskBranchPrefix+"symlink-task" {
			recordedAlias = filepath.Clean(worktree.Path) == filepath.Join(aliasRoot, paths.WorktreesDirName, "symlink-task")
		}
	}
	if !recordedAlias {
		t.Fatalf("test setup did not preserve symlinked worktree path: %+v", registered)
	}

	plan, err := PlanProjectCleanup(projectRoot)
	if err != nil {
		t.Fatalf("PlanProjectCleanup() error = %v", err)
	}
	if len(plan.Worktrees) != 1 {
		t.Fatalf("cleanup worktrees = %+v, want symlink-resolved task worktree", plan.Worktrees)
	}
	wantPath, err := filepath.EvalSymlinks(filepath.Join(projectRoot, paths.WorktreesDirName, "symlink-task"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Worktrees[0].Path != wantPath {
		t.Fatalf("cleanup worktree path = %q, want %q", plan.Worktrees[0].Path, wantPath)
	}
}

func TestPlanProjectCleanupKeepsMissingRegisteredWorktree(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	gitClient := gitops.New(projectRoot)
	if _, err := gitClient.CreateWorktree("stale-task", "HEAD"); err != nil {
		t.Fatal(err)
	}

	worktreePath := filepath.Join(projectRoot, paths.WorktreesDirName, "stale-task")
	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatal(err)
	}
	resolvedProjectRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(resolvedProjectRoot, paths.WorktreesDirName, "stale-task")

	plan, err := PlanProjectCleanup(projectRoot)
	if err != nil {
		t.Fatalf("PlanProjectCleanup() error = %v", err)
	}
	if len(plan.Worktrees) != 1 || plan.Worktrees[0].Path != wantPath {
		t.Fatalf("cleanup worktrees = %+v, want missing registered worktree %s", plan.Worktrees, wantPath)
	}
}

func TestPlanProjectCleanupRejectsNonDirectoryTarget(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	runtimePath := paths.New(projectRoot).LizaDir()
	if err := os.WriteFile(runtimePath, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := PlanProjectCleanup(projectRoot)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("PlanProjectCleanup() error = %v, want non-directory refusal", err)
	}
	contents, readErr := os.ReadFile(runtimePath)
	if readErr != nil || string(contents) != "keep" {
		t.Fatalf("planning changed non-directory target: contents=%q error=%v", contents, readErr)
	}
}

func TestExecuteProjectCleanupRefusesLiveAgent(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	lp := paths.New(projectRoot)
	if err := os.MkdirAll(lp.LizaDir(), 0755); err != nil {
		t.Fatal(err)
	}
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{
		"coder-1": {Role: "coder", PID: 1234},
	}
	testhelpers.WriteInitialState(t, lp.StatePath(), state)

	procRoot := t.TempDir()
	procDir := filepath.Join(procRoot, strconv.Itoa(1234))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	cmdline := strings.Join([]string{"liza", "agent", "coder", "--agent-id", "coder-1"}, "\x00") + "\x00"
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), []byte(cmdline), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(SetAgentProcessProcRootForTest(procRoot))

	plan, err := PlanProjectCleanup(projectRoot)
	if err != nil {
		t.Fatalf("PlanProjectCleanup() error = %v", err)
	}
	if len(plan.LiveAgents) != 1 || !strings.Contains(plan.LiveAgents[0], "coder-1") {
		t.Fatalf("live agents = %v, want coder-1", plan.LiveAgents)
	}
	if err := ExecuteProjectCleanup(plan); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("ExecuteProjectCleanup() error = %v, want live-agent refusal", err)
	}
	if _, err := os.Stat(lp.StatePath()); err != nil {
		t.Fatalf("live-agent refusal removed state: %v", err)
	}
}

func TestExecuteProjectCleanupRejectsTargetDrift(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	lp := paths.New(projectRoot)
	if err := os.MkdirAll(lp.LizaDir(), 0755); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanProjectCleanup(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	worktreesDir := filepath.Join(projectRoot, paths.WorktreesDirName)
	if err := os.Mkdir(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ExecuteProjectCleanup(plan); err == nil || !strings.Contains(err.Error(), "targets changed") {
		t.Fatalf("ExecuteProjectCleanup() error = %v, want target drift refusal", err)
	}
	for _, dir := range []string{lp.LizaDir(), worktreesDir} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("target drift removed %s: %v", dir, err)
		}
	}
}

func TestExecuteProjectCleanupExcludesConcurrentWorktreeCreation(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	lp := paths.New(projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("new-task", models.TaskStatusImplementing, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, statePath, state)

	plan, err := PlanProjectCleanup(projectRoot)
	if err != nil {
		t.Fatal(err)
	}

	revalidated := make(chan struct{})
	allowDeletion := make(chan struct{})
	var releaseOnce sync.Once
	releaseDeletion := func() { releaseOnce.Do(func() { close(allowDeletion) }) }
	t.Cleanup(releaseDeletion)
	cleanupDone := make(chan error, 1)
	go func() {
		cleanupDone <- executeProjectCleanup(plan, func() {
			close(revalidated)
			<-allowDeletion
		})
	}()
	select {
	case <-revalidated:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not reach final revalidation")
	}

	createStarted := make(chan struct{})
	createDone := make(chan error, 1)
	go func() {
		close(createStarted)
		_, createErr := CreateWorktree(projectRoot, "new-task", false)
		createDone <- createErr
	}()
	<-createStarted
	select {
	case createErr := <-createDone:
		t.Fatalf("CreateWorktree() completed during cleanup exclusion: %v", createErr)
	case <-time.After(200 * time.Millisecond):
	}
	worktreePath := filepath.Join(projectRoot, paths.WorktreesDirName, "new-task")
	if _, statErr := os.Stat(worktreePath); !os.IsNotExist(statErr) {
		t.Fatalf("concurrent worktree appeared before cleanup deletion: %v", statErr)
	}

	releaseDeletion()
	if cleanupErr := <-cleanupDone; cleanupErr != nil {
		t.Fatalf("ExecuteProjectCleanup() error = %v", cleanupErr)
	}
	if createErr := <-createDone; createErr == nil {
		t.Fatal("CreateWorktree() succeeded after cleanup removed project state")
	}
	if _, statErr := os.Stat(worktreePath); !os.IsNotExist(statErr) {
		t.Fatalf("worktree creation left data after cleanup: %v", statErr)
	}
	branchExists, branchErr := gitops.New(projectRoot).BranchExists(paths.TaskBranchPrefix + "new-task")
	if branchErr != nil {
		t.Fatal(branchErr)
	}
	if branchExists {
		t.Fatal("worktree creation left a task branch after cleanup")
	}
	if _, statErr := os.Stat(lp.LizaDir()); !os.IsNotExist(statErr) {
		t.Fatalf("cleanup state directory still exists: %v", statErr)
	}
}
