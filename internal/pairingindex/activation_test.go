package pairingindex

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/scipsearch"
)

func TestPlanActivationLanguageFilterKeepsConfiguredAndReportsUnindexable(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "go.mod"), "module example.com/project\n", 0644)
	writeFile(t, filepath.Join(repo, "main.go"), "package main\n", 0644)
	if err := os.MkdirAll(filepath.Join(repo, "api"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "api", "pyproject.toml"), "[project]\nname = \"api\"\n", 0644)
	writeFile(t, filepath.Join(repo, "api", "app.py"), "def app():\n    return 1\n", 0644)
	commitPath(t, repo, ".", "Add Go and Python sources")

	plan, err := PlanActivation(ActivationPlanOptions{
		RepoRoot:           repo,
		EnableScip:         true,
		ScipLanguageFilter: []string{"go", "typescript"},
	})
	if err != nil {
		t.Fatalf("PlanActivation() error = %v", err)
	}

	var languages []string
	for _, scipPlan := range plan.Install.ScipPlans {
		languages = append(languages, scipPlan.Language)
	}
	if !slices.Equal(languages, []string{"go"}) {
		t.Fatalf("planned languages = %v, want only configured and indexable go", languages)
	}
	if !slices.Equal(plan.Unindexable, []string{"typescript"}) {
		t.Fatalf("Unindexable = %v, want typescript", plan.Unindexable)
	}
	if len(plan.Skips) != 0 {
		t.Fatalf("Skips = %v, want none when a language filter is set", plan.Skips)
	}
	if !plan.Active() {
		t.Fatal("Active() = false, want true with a Go plan")
	}
}

func TestPlanActivationInactiveWhenNothingIsIndexable(t *testing.T) {
	repo := initGitRepo(t)

	plan, err := PlanActivation(ActivationPlanOptions{
		RepoRoot:                 repo,
		EnableScip:               true,
		EnableFunctionalClusters: true,
		ScipLanguageFilter:       []string{"go"},
	})
	if err != nil {
		t.Fatalf("PlanActivation() error = %v", err)
	}
	if plan.Active() {
		t.Fatalf("Active() = true, want false for a greenfield repository: %+v", plan.Install)
	}
	if plan.Install.EnableFunctionalClusters {
		t.Fatal("EnableFunctionalClusters = true, want false without Stacklit")
	}
}

func TestCheckActivationReportsMissingCurrentAndDrifted(t *testing.T) {
	repo := initGitRepo(t)
	opts := InstallActivationOptions{RepoRoot: repo, EnableStacklit: true}

	assertActivationStatus(t, opts, ActivationMissing)

	if _, err := InstallActivation(opts); err != nil {
		t.Fatalf("InstallActivation() error = %v", err)
	}
	assertActivationStatus(t, opts, ActivationCurrent)

	scipOpts := opts
	scipOpts.ScipPlans = []scipsearch.LanguageAggregatePlan{goAggregatePlan(repo, filepath.Join(repo, "go.scip"))}
	assertActivationStatus(t, scipOpts, ActivationDrifted)

	if runtime.GOOS != "windows" {
		scriptPath := filepath.Join(repo, ".git", "hooks", scriptName())
		if err := os.Chmod(scriptPath, 0o644); err != nil {
			t.Fatal(err)
		}
		assertActivationStatus(t, opts, ActivationDrifted)
		if _, err := InstallActivation(opts); err != nil {
			t.Fatalf("InstallActivation() error = %v", err)
		}
		assertActivationStatus(t, opts, ActivationCurrent)

		// A wrapper, installed where symlinks are unavailable, restored
		// without its execute bit is skipped by Git.
		wrapperPath := filepath.Join(repo, ".git", "hooks", "post-commit")
		if err := os.Remove(wrapperPath); err != nil {
			t.Fatal(err)
		}
		writeFile(t, wrapperPath, managedHookContent("post-commit"), 0o644)
		assertActivationStatus(t, opts, ActivationDrifted)
	}

	if err := os.Remove(filepath.Join(repo, ".git", "hooks", "post-merge")); err != nil {
		t.Fatal(err)
	}
	assertActivationStatus(t, opts, ActivationMissing)
}

func assertActivationStatus(t *testing.T, opts InstallActivationOptions, want ActivationStatus) {
	t.Helper()

	got, err := CheckActivation(opts)
	if err != nil {
		t.Fatalf("CheckActivation() error = %v", err)
	}
	if got != want {
		t.Fatalf("CheckActivation() = %q, want %q", got, want)
	}
}
