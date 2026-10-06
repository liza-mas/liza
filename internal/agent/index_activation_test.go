package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// newIndexActivationTestProject returns a git project with MAS state whose
// index gates are all off unless the test turns one on.
func newIndexActivationTestProject(t *testing.T) (string, func()) {
	t.Helper()

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, nil)
	t.Setenv(stacklit.EnvEnableStacklit, "false")
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(functionalclusters.EnvEnableFunctionalClusters, "false")
	t.Setenv(semble.EnvEnableSemble, "false")
	return projectRoot, func() { ensureProjectRootIndexActivation(bb, projectRoot) }
}

func TestSembleStartupRequestsPreparationOnMissingAndCurrentInstallations(t *testing.T) {
	for _, current := range []bool{false, true} {
		t.Run(fmt.Sprint(current), func(t *testing.T) {
			root, ensure := newIndexActivationTestProject(t)
			t.Setenv(semble.EnvEnableSemble, "true")
			plan, err := pairingindex.PlanActivation(pairingindex.MASActivationOptions(root, nil))
			if err != nil {
				t.Fatal(err)
			}
			if current {
				if _, err := pairingindex.InstallActivation(plan.Install); err != nil {
					t.Fatal(err)
				}
			}
			original := startProjectRootIndexRefresh
			t.Cleanup(func() { startProjectRootIndexRefresh = original })
			calls := 0
			startProjectRootIndexRefresh = func(target, trigger string) error {
				calls++
				if target != root || trigger != "startup" {
					t.Fatalf("refresh target/trigger = %q/%q", target, trigger)
				}
				return nil
			}
			ensure()
			if calls != 1 || !semble.ValidateTargetSafety(semble.TargetSafetyOptions{Kind: semble.TargetKindProjectRoot, TargetRoot: root}).Safe {
				t.Fatalf("startup requests = %d or root unsafe", calls)
			}
			if status, err := pairingindex.CheckActivation(plan.Install); err != nil || status != pairingindex.ActivationCurrent {
				t.Fatalf("startup activation = %s/%v", status, err)
			}
		})
	}
}

func TestSembleStartupPreparationFailureIsOptionalAndAlerted(t *testing.T) {
	root, ensure := newIndexActivationTestProject(t)
	t.Setenv(semble.EnvEnableSemble, "true")
	original := startProjectRootIndexRefresh
	t.Cleanup(func() { startProjectRootIndexRefresh = original })
	startProjectRootIndexRefresh = func(string, string) error { return errors.New("cannot start preparation") }
	ensure()
	log, err := os.ReadFile(paths.New(root).AlertsLogPath())
	if err != nil || !strings.Contains(string(log), "cannot start preparation") {
		t.Fatalf("optional preparation failure not alerted: %s / %v", log, err)
	}
}

func TestEnsureProjectRootIndexActivationInstallsMissingHooks(t *testing.T) {
	projectRoot, ensure := newIndexActivationTestProject(t)
	t.Setenv(stacklit.EnvEnableStacklit, "true")

	ensure()

	status, err := pairingindex.CheckActivation(pairingindex.InstallActivationOptions{
		RepoRoot:       projectRoot,
		EnableStacklit: true,
	})
	if err != nil {
		t.Fatalf("CheckActivation() error = %v", err)
	}
	if status != pairingindex.ActivationCurrent {
		t.Fatalf("activation status = %q, want %q after orchestrator repair", status, pairingindex.ActivationCurrent)
	}
}

func TestEnsureProjectRootIndexActivationAlertsOnCollisionAndKeepsUserHook(t *testing.T) {
	projectRoot, ensure := newIndexActivationTestProject(t)
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	hooksDir := filepath.Join(projectRoot, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userHook := filepath.Join(hooksDir, "post-commit")
	if err := os.WriteFile(userHook, []byte("#!/bin/sh\necho user hook\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ensure()

	if got, err := os.ReadFile(userHook); err != nil || string(got) != "#!/bin/sh\necho user hook\n" {
		t.Fatalf("user hook = %q (err %v), want it untouched", got, err)
	}
	alertLog, err := os.ReadFile(paths.New(projectRoot).AlertsLogPath())
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	if !strings.Contains(string(alertLog), "INDEX HOOKS UNAVAILABLE") {
		t.Fatalf("alerts log = %q, want index hook alert", alertLog)
	}
}

func TestRejectedDuplicateOrchestratorLeavesIndexHooksUntouched(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Agents["orchestrator-1"] = models.Agent{
		Role:         "orchestrator",
		Status:       models.AgentStatusPlanning,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
		Heartbeat:    time.Now().UTC(),
		PID:          os.Getpid(),
	}
	testhelpers.WriteInitialState(t, statePath, state)
	t.Setenv(stacklit.EnvEnableStacklit, "true")

	err := RunSupervisor(context.Background(), SupervisorConfig{
		AgentID: "orchestrator-2", Role: "orchestrator", ProjectRoot: root,
		StatePath: statePath, LogPath: filepath.Join(root, paths.ProjectDirName(), "log.yaml"),
		SpecsDir: filepath.Join(root, "specs"), CLIName: "codex", LLMAgent: &MockLLMAgent{},
	})
	if err == nil || !strings.Contains(err.Error(), "already has") {
		t.Fatalf("RunSupervisor() error = %v, want duplicate orchestrator rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".git", "hooks", brand.BinaryName+"-index.sh")); !os.IsNotExist(statErr) {
		t.Fatalf("index script stat error = %v, want a rejected duplicate to leave hooks alone", statErr)
	}
}

func TestEnsureProjectRootIndexActivationLeavesHooksAloneWhenGatesAreOff(t *testing.T) {
	projectRoot, ensure := newIndexActivationTestProject(t)

	ensure()

	if _, err := os.Stat(filepath.Join(projectRoot, ".git", "hooks", brand.BinaryName+"-index.sh")); !os.IsNotExist(err) {
		t.Fatalf("index script stat error = %v, want nothing installed with every gate off", err)
	}
}
