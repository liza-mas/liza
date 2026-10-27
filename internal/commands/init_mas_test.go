package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
)

func TestMASInitDefaultsKeepFormerValues(t *testing.T) {
	if defaultInitBranch != "integration" {
		t.Fatalf("defaultInitBranch changed")
	}
	if initDirMode != 0755 || initFileMode != 0644 {
		t.Fatalf("init file modes changed: dir %o file %o", initDirMode, initFileMode)
	}
	m, err := newMASInit(InitParams{Stdin: strings.NewReader("")})
	if err != nil {
		t.Fatalf("newMASInit: %v", err)
	}
	if m.branch != "integration" {
		t.Fatalf("default branch changed")
	}
}

func TestMASInitRejectsScipSearchPlansBeforeAnyProjectAccess(t *testing.T) {
	_, err := newMASInit(InitParams{ScipSearchPlans: []string{"go=."}})
	if err == nil || !strings.Contains(err.Error(), "--scip-search-plan is only supported for pairing init without a description") {
		t.Fatalf("expected scip-search-plan refusal, got %v", err)
	}
}

// The state values moved to ops (TestC6InitStateKeepsFormerValues); this keeps
// the input mapping and the copy-flag environment read in the command.
func TestMASInitStateInputsKeepFormerSources(t *testing.T) {
	m := &masInit{
		params: InitParams{
			Description:                     "goal text",
			EntryPoint:                      "entry",
			DefaultCLI:                      "codex",
			AutoResume:                      true,
			NoFollowUp:                      true,
			MaxGlobalIntegrationGenerations: 4,
			MaxInstances:                    2,
		},
		branch:           "feature-branch",
		specRepoRel:      "specs/vision.md",
		agentEnvDeny:     []string{"SECRET_ONE"},
		scipSearchConfig: scipsearch.InitResult{},
		postWorktreeCmd:  "npm ci",
		validationTools:  map[string]models.AgentToolConfig{"codex": {ValidationExecution: "local"}},
	}
	timestamp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Setenv(models.EnvEnableCopyWorktreeEnvFiles, "")
	got := m.initialStateInputs(timestamp)
	want := ops.InitialStateInputs{
		Description:                     "goal text",
		SpecRef:                         "specs/vision.md",
		EntryPoint:                      "entry",
		Timestamp:                       timestamp,
		Branch:                          "feature-branch",
		DefaultCLI:                      "codex",
		AutoResume:                      true,
		NoFollowUp:                      true,
		MaxGlobalIntegrationGenerations: 4,
		MaxInstances:                    2,
		ScipSearch:                      m.scipSearchConfig.LanguageNames(),
		PostWorktreeCmd:                 "npm ci",
		AgentEnvDeny:                    []string{"SECRET_ONE"},
		AgentTools:                      m.validationTools,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("init inputs changed:\n got %+v\nwant %+v", got, want)
	}

	t.Setenv(models.EnvEnableCopyWorktreeEnvFiles, "1")
	if !m.initialStateInputs(timestamp).CopyWorktreeEnvFiles {
		t.Fatalf("copy-worktree-env-files environment gate no longer enables the flag")
	}
	t.Setenv(models.EnvEnableCopyWorktreeEnvFiles, "")
	m.params.CopyWorktreeEnvFiles = true
	if !m.initialStateInputs(timestamp).CopyWorktreeEnvFiles {
		t.Fatalf("copy-worktree-env-files parameter no longer enables the flag")
	}
}

func newMASInitForRoot(t *testing.T) *masInit {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return &masInit{
		params:       InitParams{Description: "goal text"},
		branch:       "integration",
		lizaPaths:    paths.New(root),
		pipelineData: []byte("pipeline: {}\n"),
	}
}

// A failure after the runtime directory exists removes it; a failure creating
// the directory itself removes nothing.
func TestMASInitRuntimeFileFailuresCleanUp(t *testing.T) {
	t.Run("freeze failure removes runtime directory", func(t *testing.T) {
		m := newMASInitForRoot(t)
		if err := m.createLayout(); err != nil {
			t.Fatalf("createLayout: %v", err)
		}
		if err := os.Mkdir(filepath.Join(m.lizaPaths.LizaDir(), "pipeline.yaml"), 0755); err != nil {
			t.Fatal(err)
		}
		err := m.writeRuntimeFiles()
		if err == nil || !strings.Contains(err.Error(), "failed to freeze pipeline config") {
			t.Fatalf("expected freeze failure, got %v", err)
		}
		if _, statErr := os.Stat(m.lizaPaths.LizaDir()); !os.IsNotExist(statErr) {
			t.Fatalf("runtime directory kept after freeze failure")
		}
	})
	t.Run("log failure removes runtime directory", func(t *testing.T) {
		m := newMASInitForRoot(t)
		if err := m.createLayout(); err != nil {
			t.Fatalf("createLayout: %v", err)
		}
		if err := os.Mkdir(m.lizaPaths.LogPath(), 0755); err != nil {
			t.Fatal(err)
		}
		err := m.writeLog(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
		if err == nil || !strings.Contains(err.Error(), "failed to write log file") {
			t.Fatalf("expected log failure, got %v", err)
		}
		if _, statErr := os.Stat(m.lizaPaths.LizaDir()); !os.IsNotExist(statErr) {
			t.Fatalf("runtime directory kept after log failure")
		}
	})
	t.Run("layout failure keeps what it did not create", func(t *testing.T) {
		m := newMASInitForRoot(t)
		if err := os.WriteFile(m.lizaPaths.LizaDir(), []byte("not a directory"), 0644); err != nil {
			t.Fatal(err)
		}
		err := m.createLayout()
		if err == nil || !strings.Contains(err.Error(), "directory") {
			t.Fatalf("expected layout failure, got %v", err)
		}
		if _, statErr := os.Stat(m.lizaPaths.LizaDir()); statErr != nil {
			t.Fatalf("pre-existing path removed after layout failure: %v", statErr)
		}
	})
}

func TestMASInitLogRecordsGoalAndTimestamp(t *testing.T) {
	m := newMASInitForRoot(t)
	if err := m.createLayout(); err != nil {
		t.Fatalf("createLayout: %v", err)
	}
	timestamp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := m.writeLog(timestamp); err != nil {
		t.Fatalf("writeLog: %v", err)
	}
	got, err := os.ReadFile(m.lizaPaths.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	want := "- timestamp: 2030-01-02T03:04:05Z\n  agent: system\n  action: initialized\n  detail: goal text\n"
	if string(got) != want {
		t.Fatalf("log content changed:\n got %q\nwant %q", got, want)
	}
	for _, name := range []string{m.lizaPaths.AlertsLogPath(), m.lizaPaths.LockPath()} {
		info, err := os.Stat(name)
		if err != nil || info.Size() != 0 {
			t.Fatalf("supporting file %s missing or not empty: %v", filepath.Base(name), err)
		}
	}
}
