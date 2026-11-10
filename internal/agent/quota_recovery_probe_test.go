package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
)

// #114: once a quota block expires, recovery is one attempt per provider and
// project. The launch gate is the boundary every provider start crosses, so a
// second agent on the same provider must not start while the first attempt is
// still running.
func TestProviderLaunchGate_ExpiredQuotaAdmitsOneConcurrentStart(t *testing.T) {
	// GIVEN two registered agents on the same provider and an expired quota
	// signal for that provider
	fixture := newProviderGenerationFixture(t)
	authorityB, err := registerAgentWithAuthority(fixture.bb, fixture.projectRoot, "coder-2", "coder", "terminal-b", 1800, "codex", "", fixture.resolver)
	if err != nil {
		t.Fatalf("register coder-2: %v", err)
	}
	configA := fixture.config(fixture.authorityA)
	configB := configA
	configB.AgentID, configB.Authority = "coder-2", authorityB
	writeExpiredQuotaSignal(t, fixture.projectRoot, configA.CLIName)

	// WHEN agent A's provider start is admitted and still running
	entered, release := make(chan struct{}), make(chan struct{})
	doneA := make(chan error, 1)
	go func() {
		doneA <- newProviderLaunchGate(configA)(context.Background(), func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-doneA:
		t.Fatalf("agent A was not admitted on an expired quota signal: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("agent A's provider start was not reached")
	}

	// AND agent B reaches its own provider start
	startedB := false
	errB := newProviderLaunchGate(configB)(context.Background(), func() error {
		startedB = true
		return nil
	})
	close(release)
	if err := <-doneA; err != nil {
		t.Fatalf("agent A launch: %v", err)
	}

	// THEN only A's attempt runs: B is refused without starting a provider
	if startedB {
		t.Fatal("agent B started a provider while agent A's recovery attempt on the same provider was still running")
	}
	if errB == nil {
		t.Fatal("agent B's launch gate returned nil, want a refusal while A's recovery attempt runs")
	}
	if !errors.Is(errB, errLaunchQuotaBlocked) || !strings.Contains(errB.Error(), "probe by coder-1") {
		t.Fatalf("agent B refusal = %v, want errLaunchQuotaBlocked naming A's probe", errB)
	}
}

func TestProviderLaunchGate_UnexpiredQuotaRefusesStart(t *testing.T) {
	// GIVEN a peer raised quota exhaustion after this agent passed its loop-top check
	fixture := newProviderGenerationFixture(t)
	config := fixture.config(fixture.authorityA)
	if err := providersignal.RaiseQuotaExhaustion(fixture.projectRoot, &providersignal.QuotaExhaustion{Provider: config.CLIName, Message: "limit"}); err != nil {
		t.Fatal(err)
	}

	// WHEN the agent reaches its provider start
	started := false
	err := newProviderLaunchGate(config)(context.Background(), func() error {
		started = true
		return nil
	})

	// THEN no provider starts
	if started || !errors.Is(err, errLaunchQuotaBlocked) {
		t.Fatalf("started=%v err=%v, want a quota refusal before the start", started, err)
	}
}

// A backend may mask a gate refusal as exit 1 (ACPX); the gate's outcome wins.
func TestSessionLaunchSettle_SurfacesQuotaRefusal(t *testing.T) {
	launch := &sessionLaunch{launchErr: fmt.Errorf("%w: codex is quota-exhausted", errLaunchQuotaBlocked)}
	if err := launch.settle(nil); !errors.Is(err, errLaunchQuotaBlocked) {
		t.Fatalf("settle() = %v, want the quota refusal", err)
	}
}

// The probe's turn settles the episode, and no outcome touches the sprint,
// the checkpoint or the system mode.
func TestExecuteAgent_SettlesQuotaRecoveryProbe(t *testing.T) {
	for _, c := range []struct {
		name         string
		result       LLMAgentRunResult
		wantSignal   bool
		wantLease    bool
		wantAlert    string
		raiseOnCrash bool
	}{
		{name: "provider answers", result: LLMAgentRunResult{ExitCode: 0, Output: "review done"}, wantAlert: "PROVIDER QUOTA RECOVERED — codex: provider answered code-reviewer-1's recovery probe"},
		{name: "graceful abort is an answer", result: LLMAgentRunResult{ExitCode: exitCodeGracefulAbort, Output: "aborting"}, wantAlert: "PROVIDER QUOTA RECOVERED"},
		{name: "unrelated crash is inconclusive", result: LLMAgentRunResult{ExitCode: 1, Output: "segmentation fault"}, wantSignal: true, wantAlert: "PROVIDER QUOTA PROBE INCONCLUSIVE"},
		{name: "renewed exhaustion re-blocks through the crash path", result: LLMAgentRunResult{ExitCode: 1, Output: `{"type":"error","message":"You've hit your usage limit. Upgrade to Pro."}`}, wantSignal: true, wantLease: true, raiseOnCrash: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			config, bb, runtime := reviewExecutionFixture(t)
			writeExpiredQuotaSignal(t, config.ProjectRoot, config.CLIName)
			before, err := bb.ReadSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			config.LLMAgent = &reviewExecutionProvider{result: c.result, run: func(context.Context) error { return nil }}

			if _, _, err := executeAgent(context.Background(), config, "review", nil, "review-task", runtime); err != nil {
				t.Fatalf("executeAgent: %v", err)
			}

			signalPath := providersignal.QuotaSignalPath(config.ProjectRoot, config.CLIName)
			leasePath := providersignal.QuotaProbePath(config.ProjectRoot, config.CLIName)
			if got := fileExists(t, signalPath); got != c.wantSignal {
				t.Errorf("signal present = %v, want %v", got, c.wantSignal)
			}
			if got := fileExists(t, leasePath); got != c.wantLease {
				t.Errorf("probe lease present = %v, want %v", got, c.wantLease)
			}
			if c.raiseOnCrash {
				if !handleClassifiedProviderCrash(config, c.result.Output) {
					t.Fatal("the quota output was not classified as a provider crash")
				}
				if !providersignal.CheckQuotaSignal(config.ProjectRoot, config.CLIName) || fileExists(t, leasePath) {
					t.Fatal("renewed exhaustion must re-block the provider and free the probe")
				}
			}
			alerts, _ := os.ReadFile(paths.New(config.ProjectRoot).AlertsLogPath())
			if c.wantAlert != "" && !strings.Contains(string(alerts), c.wantAlert) {
				t.Errorf("alerts lack %q:\n%s", c.wantAlert, alerts)
			}
			after, err := bb.ReadSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.Sprint, after.Sprint) || before.Config.Mode != after.Config.Mode {
				t.Fatalf("probe settlement changed the sprint or mode:\nbefore %+v %s\nafter  %+v %s", before.Sprint, before.Config.Mode, after.Sprint, after.Config.Mode)
			}
		})
	}
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

func writeExpiredQuotaSignal(t *testing.T, projectRoot, provider string) {
	t.Helper()
	past := time.Now().UTC().Add(-time.Hour)
	record := "provider: " + provider + "\n" +
		"detected: " + past.Add(-time.Hour).Format(time.RFC3339) + "\n" +
		"message: You've hit your usage limit\n" +
		"resets_at: " + past.Format(time.RFC3339) + "\n" +
		"expires: " + past.Format(time.RFC3339) + "\n"
	if err := os.WriteFile(providersignal.QuotaSignalPath(projectRoot, provider), []byte(record), 0o644); err != nil {
		t.Fatalf("write expired quota signal: %v", err)
	}
}

// A session the progress watchdog killed is classified 0 to settle its task;
// that policy code is not a provider answer and must not prove recovery.
func TestSettleQuotaProbe_WatchdogEndedProbeIsInconclusive(t *testing.T) {
	config, _, runtime := reviewExecutionFixture(t)
	writeExpiredQuotaSignal(t, config.ProjectRoot, config.CLIName)
	admission, err := providersignal.AdmitQuotaStart(config.ProjectRoot, config.CLIName, config.AgentID)
	if err != nil || admission.Admission != providersignal.QuotaProbe {
		t.Fatalf("admission = %+v, %v; want probe", admission, err)
	}
	run := sessionRun{
		result:   LLMAgentRunResult{ExitCode: 1},
		err:      errors.New("provider was killed without an answer"),
		execCtx:  context.Background(),
		progress: executionProgressWatchdogResult{Blocked: true, Reason: "no progress"},
	}
	session := &agentSession{config: config, taskID: "review-task", runtimeConfig: runtime}
	if code, _, classifyErr := session.classify(run); code != 0 || classifyErr != nil {
		t.Fatalf("classify = %d, %v; this test pins the policy code 0 it must not trust", code, classifyErr)
	}

	settleQuotaProbe(config, run.providerAnswered(), run.result.Output)

	if !fileExists(t, providersignal.QuotaSignalPath(config.ProjectRoot, config.CLIName)) {
		t.Fatal("a watchdog-ended silent probe cleared the quota signal")
	}
	if fileExists(t, providersignal.QuotaProbePath(config.ProjectRoot, config.CLIName)) {
		t.Fatal("the inconclusive probe kept its lease")
	}
	alerts, _ := os.ReadFile(paths.New(config.ProjectRoot).AlertsLogPath())
	if !strings.Contains(string(alerts), "PROVIDER QUOTA PROBE INCONCLUSIVE") || strings.Contains(string(alerts), "PROVIDER QUOTA RECOVERED") {
		t.Fatalf("alerts = %s, want inconclusive and no recovery", alerts)
	}
}

func TestSessionRunProviderAnswered(t *testing.T) {
	expired, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-expired.Done()
	for _, c := range []struct {
		name string
		run  sessionRun
		want bool
	}{
		{"finished turn", sessionRun{result: LLMAgentRunResult{ExitCode: 0}, execCtx: context.Background()}, true},
		{"graceful abort", sessionRun{result: LLMAgentRunResult{ExitCode: exitCodeGracefulAbort}, execCtx: context.Background()}, true},
		{"backend error", sessionRun{result: LLMAgentRunResult{ExitCode: 0}, err: errors.New("pipe closed"), execCtx: context.Background()}, false},
		{"crash", sessionRun{result: LLMAgentRunResult{ExitCode: 1}, execCtx: context.Background()}, false},
		{"watchdog", sessionRun{result: LLMAgentRunResult{ExitCode: 0}, execCtx: context.Background(), progress: executionProgressWatchdogResult{Blocked: true}}, false},
		{"review lost", sessionRun{result: LLMAgentRunResult{ExitCode: 0}, execCtx: context.Background(), reviewLost: true}, false},
		{"deadline", sessionRun{result: LLMAgentRunResult{ExitCode: 0}, execCtx: expired}, false},
	} {
		if got := c.run.providerAnswered(); got != c.want {
			t.Errorf("%s: providerAnswered() = %v, want %v", c.name, got, c.want)
		}
	}
}
