package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupInitTestDir creates a temp dir with a git repo and a spec file,
// mimicking a valid project root for InitProject.
func setupInitTestDir(t *testing.T) (projectRoot, specFile string) {
	t.Helper()

	testhelpers.SetupGlobalLiza(t)
	projectRoot = t.TempDir()

	// Initialize a git repo so branch operations work
	gitInit(t, projectRoot)

	// Create a spec file
	specDir := filepath.Join(projectRoot, "specs")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("Failed to create specs dir: %v", err)
	}
	specFile = filepath.Join(specDir, "goal.md")
	if err := os.WriteFile(specFile, []byte("# Test Goal\n"), 0644); err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	return projectRoot, specFile
}

// gitInit initializes a bare-minimum git repo with an initial commit.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}
}

func TestInitProject_Success(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	// Verify .liza directory exists
	lizaDir := filepath.Join(projectRoot, ".liza")
	if _, err := os.Stat(lizaDir); os.IsNotExist(err) {
		t.Fatal(".liza directory was not created")
	}

	// Verify state.yaml is readable with expected values
	statePath := filepath.Join(lizaDir, "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	if state.Goal.Status != models.GoalStatusInProgress {
		t.Errorf("GoalStatus = %v, want IN_PROGRESS", state.Goal.Status)
	}
	if state.Config.Mode != models.SystemModeRunning {
		t.Errorf("Mode = %v, want RUNNING", state.Config.Mode)
	}
	if state.Config.IntegrationBranch != "integration" {
		t.Errorf("IntegrationBranch = %q, want %q", state.Config.IntegrationBranch, "integration")
	}
	if state.Goal.Description != "Test project" {
		t.Errorf("Description = %q, want %q", state.Goal.Description, "Test project")
	}
	if state.Goal.SpecRef != specFile {
		t.Errorf("SpecRef = %q, want %q", state.Goal.SpecRef, specFile)
	}

	// Verify log file exists
	logPath := filepath.Join(lizaDir, "log.yaml")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Fatal("log.yaml was not created")
	}

	// Verify alerts.log exists
	alertsPath := filepath.Join(lizaDir, "alerts.log")
	if _, err := os.Stat(alertsPath); os.IsNotExist(err) {
		t.Fatal("alerts.log was not created")
	}

	// Verify lock file exists
	lockPath := filepath.Join(lizaDir, "state.yaml.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("state.yaml.lock was not created")
	}

	// Verify archive directory exists
	archiveDir := filepath.Join(lizaDir, "archive")
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		t.Fatal("archive directory was not created")
	}

	// Verify pipeline.yaml frozen
	pipelinePath := filepath.Join(lizaDir, "pipeline.yaml")
	if _, err := os.Stat(pipelinePath); os.IsNotExist(err) {
		t.Fatal("pipeline.yaml was not created")
	}
}

func TestInitProject_AlreadyExists(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	// Pre-create .liza directory
	lizaDir := filepath.Join(projectRoot, ".liza")
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatalf("Failed to create .liza dir: %v", err)
	}

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err == nil {
		t.Fatal("Expected error when .liza already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Error = %q, want to contain 'already exists'", err.Error())
	}
}

func TestInitProject_MissingSpecFile(t *testing.T) {
	projectRoot, _ := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     filepath.Join(projectRoot, "specs", "nonexistent.md"),
	})
	if err == nil {
		t.Fatal("Expected error when spec file is missing")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Error = %q, want to contain 'does not exist'", err.Error())
	}
}

func TestInitProject_EmptyBranchDefaultsToIntegration(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		Branch:      "", // empty should default to "integration"
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.IntegrationBranch != "integration" {
		t.Errorf("IntegrationBranch = %q, want %q", state.Config.IntegrationBranch, "integration")
	}
}

func TestInitProject_InvalidEntryPoint(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		EntryPoint:  "nonexistent-entry-point",
	})
	if err == nil {
		t.Fatal("Expected error for invalid entry-point")
	}
	if !strings.Contains(err.Error(), "not found in pipeline config") {
		t.Errorf("Error = %q, want to contain 'not found in pipeline config'", err.Error())
	}
}

func TestInitProject_StateReadableAfterSuccess(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	// Use db.For() as specified in done_when
	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("db.For().Read() error: %v", err)
	}
	if state.Goal.Status != models.GoalStatusInProgress {
		t.Errorf("GoalStatus = %v, want IN_PROGRESS", state.Goal.Status)
	}
	if state.Config.Mode != models.SystemModeRunning {
		t.Errorf("Mode = %v, want RUNNING", state.Config.Mode)
	}
}

func TestInitProject_CustomBranch(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		Branch:      "develop",
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.IntegrationBranch != "develop" {
		t.Errorf("IntegrationBranch = %q, want %q", state.Config.IntegrationBranch, "develop")
	}
}

func TestInitProject_AutoResumeFlag(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		AutoResume:  true,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !state.Config.AutoResume {
		t.Error("AutoResume = false, want true")
	}
}

func TestInitProject_DefaultCLI(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		DefaultCLI:  "codex",
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultCLI != "codex" {
		t.Errorf("DefaultCLI = %q, want %q", state.Config.DefaultCLI, "codex")
	}
}

func TestInitProject_DefaultCLIEmpty(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultCLI != "" {
		t.Errorf("DefaultCLI = %q, want empty", state.Config.DefaultCLI)
	}

	// Verify omitempty
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state.yaml: %v", err)
	}
	if strings.Contains(string(data), "default_cli") {
		t.Error("state.yaml contains default_cli, want omitted when empty")
	}
}
