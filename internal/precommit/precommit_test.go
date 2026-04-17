package precommit_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/precommit"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// initRepoOnBranch initializes a fresh git repo in dir on branch `main`
// with a single README commit. Additional files may be committed by the
// caller before checking out a different branch.
func initRepoOnBranch(t *testing.T, dir string) {
	t.Helper()
	testhelpers.MustGit(t, dir, "init", "-b", "main")
	testhelpers.MustGit(t, dir, "config", "user.email", "test@example.com")
	testhelpers.MustGit(t, dir, "config", "user.name", "Test User")
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	testhelpers.MustGit(t, dir, "add", "README.md")
	testhelpers.MustGit(t, dir, "commit", "-m", "init")
}

func TestConfigExistsOnIntegration_Exists(t *testing.T) {
	dir := t.TempDir()
	initRepoOnBranch(t, dir)
	cfg := filepath.Join(dir, ".pre-commit-config.yaml")
	if err := os.WriteFile(cfg, []byte("repos: []\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	testhelpers.MustGit(t, dir, "add", ".pre-commit-config.yaml")
	testhelpers.MustGit(t, dir, "commit", "-m", "add precommit config")

	got, err := precommit.ConfigExistsOnIntegration(dir, "main")
	if err != nil {
		t.Fatalf("ConfigExistsOnIntegration: %v", err)
	}
	if !got {
		t.Errorf("got exists=false, want true")
	}
}

func TestConfigExistsOnIntegration_Absent(t *testing.T) {
	t.Run("tracked-but-absent", func(t *testing.T) {
		dir := t.TempDir()
		initRepoOnBranch(t, dir)

		got, err := precommit.ConfigExistsOnIntegration(dir, "main")
		if err != nil {
			t.Fatalf("ConfigExistsOnIntegration: %v", err)
		}
		if got {
			t.Errorf("got exists=true, want false")
		}
	})

	t.Run("uncommitted-working-tree", func(t *testing.T) {
		dir := t.TempDir()
		initRepoOnBranch(t, dir)
		// File is in the working tree but never added/committed.
		cfg := filepath.Join(dir, ".pre-commit-config.yaml")
		if err := os.WriteFile(cfg, []byte("repos: []\n"), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		got, err := precommit.ConfigExistsOnIntegration(dir, "main")
		if err != nil {
			t.Fatalf("ConfigExistsOnIntegration: %v", err)
		}
		if got {
			t.Errorf("got exists=true for uncommitted file, want false (helper must ignore working-tree drift)")
		}
	})
}

func TestConfigExistsOnIntegration_Error(t *testing.T) {
	// "Absent path on existing branch returns (false, nil)" contrast case.
	t.Run("absent-path-on-existing-branch-no-error", func(t *testing.T) {
		dir := t.TempDir()
		initRepoOnBranch(t, dir)
		got, err := precommit.ConfigExistsOnIntegration(dir, "main")
		if err != nil {
			t.Fatalf("expected (false, nil), got err=%v", err)
		}
		if got {
			t.Fatalf("expected (false, nil), got exists=true")
		}
	})

	cases := []struct {
		name           string
		projectRoot    func(t *testing.T) string
		branch         string
		wantMsgContain []string
	}{
		{
			name: "invalid-branch",
			projectRoot: func(t *testing.T) string {
				dir := t.TempDir()
				initRepoOnBranch(t, dir)
				return dir
			},
			branch:         "nonexistent",
			wantMsgContain: []string{"integration branch", "not found"},
		},
		{
			name: "empty-projectRoot",
			projectRoot: func(t *testing.T) string {
				return ""
			},
			branch:         "main",
			wantMsgContain: []string{"projectRoot is empty"},
		},
		{
			name: "empty-integrationBranch",
			projectRoot: func(t *testing.T) string {
				return t.TempDir()
			},
			branch:         "",
			wantMsgContain: []string{"integrationBranch is empty"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.projectRoot(t)
			got, err := precommit.ConfigExistsOnIntegration(root, tc.branch)
			if err == nil {
				t.Fatalf("expected error, got nil (exists=%v)", got)
			}
			if got {
				t.Errorf("expected exists=false on error, got true")
			}
			if !errors.Is(err, precommit.ErrContextBuild) {
				t.Errorf("errors.Is(err, ErrContextBuild) = false; err=%v", err)
			}
			msg := err.Error()
			for _, want := range tc.wantMsgContain {
				if !strings.Contains(msg, want) {
					t.Errorf("error message %q missing %q", msg, want)
				}
			}
		})
	}
}

func TestBootstrapInFlight_Hit(t *testing.T) {
	state := &models.State{
		Tasks: []models.Task{
			{
				ID:     "t1",
				Kind:   precommit.Kind,
				Status: models.TaskStatusReady,
			},
			{
				ID:     "t2",
				Kind:   "",
				Status: models.TaskStatusImplementing,
			},
		},
	}
	if !precommit.BootstrapInFlight(state) {
		t.Errorf("BootstrapInFlight = false, want true")
	}
}

func TestBootstrapInFlight_BlockedCountsAsInFlight(t *testing.T) {
	t.Run("single-blocked", func(t *testing.T) {
		reason := "waiting on setup-dep-manager"
		state := &models.State{
			Tasks: []models.Task{
				{
					ID:            "bootstrap-1",
					Kind:          precommit.Kind,
					Status:        models.TaskStatusBlocked,
					BlockedReason: &reason,
				},
			},
		}
		if !precommit.BootstrapInFlight(state) {
			t.Errorf("BootstrapInFlight (BLOCKED) = false, want true")
		}
	})

	t.Run("mixed-superseded-merged-blocked", func(t *testing.T) {
		reason := "waiting on setup-dep-manager"
		state := &models.State{
			Tasks: []models.Task{
				{ID: "sup", Kind: precommit.Kind, Status: models.TaskStatusSuperseded},
				{ID: "mer", Kind: precommit.Kind, Status: models.TaskStatusMerged},
				{ID: "blk", Kind: precommit.Kind, Status: models.TaskStatusBlocked, BlockedReason: &reason},
			},
		}
		if !precommit.BootstrapInFlight(state) {
			t.Errorf("BootstrapInFlight (mixed) = false, want true (BLOCKED is non-terminal)")
		}
	})
}

func TestBootstrapInFlight_Miss(t *testing.T) {
	t.Run("nil-state", func(t *testing.T) {
		if precommit.BootstrapInFlight(nil) {
			t.Errorf("BootstrapInFlight(nil) = true, want false")
		}
	})

	t.Run("empty-tasks", func(t *testing.T) {
		state := &models.State{}
		if precommit.BootstrapInFlight(state) {
			t.Errorf("BootstrapInFlight(empty) = true, want false")
		}
	})

	t.Run("non-matching-kind", func(t *testing.T) {
		state := &models.State{
			Tasks: []models.Task{
				{ID: "t1", Kind: "something-else", Status: models.TaskStatusImplementing},
				{ID: "t2", Kind: "", Status: models.TaskStatusReady},
			},
		}
		if precommit.BootstrapInFlight(state) {
			t.Errorf("BootstrapInFlight(non-matching) = true, want false")
		}
	})

	t.Run("matching-kind-all-terminal", func(t *testing.T) {
		state := &models.State{
			Tasks: []models.Task{
				{ID: "t1", Kind: precommit.Kind, Status: models.TaskStatusMerged},
				{ID: "t2", Kind: precommit.Kind, Status: models.TaskStatusAbandoned},
				{ID: "t3", Kind: precommit.Kind, Status: models.TaskStatusSuperseded},
			},
		}
		if precommit.BootstrapInFlight(state) {
			t.Errorf("BootstrapInFlight(all-terminal) = true, want false")
		}
	})
}
