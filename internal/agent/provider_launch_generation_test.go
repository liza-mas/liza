package agent

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestProviderLaunchGenerationLinearization(t *testing.T) {
	t.Run("replacement wins before every start boundary", func(t *testing.T) {
		cases := []struct {
			name    string
			prepare func(*testing.T, string)
			run     func(context.Context, LLMAgentLaunchGate, string, string) error
		}{
			{name: "cli run", prepare: func(t *testing.T, path string) { writeCLIProviderStubForGenerationTest(t, path, false) }, run: runBlockedCLIForGenerationTest},
			{name: "cli interactive", prepare: func(t *testing.T, path string) { writeCLIProviderStubForGenerationTest(t, path, false) }, run: runBlockedCLIInteractiveForGenerationTest},
			{name: "acpx session and prompt", prepare: func(t *testing.T, path string) { writeACPXProviderStubsForGenerationTest(t, path, false) }, run: runBlockedACPXForGenerationTest},
			{name: "acpx interactive", prepare: func(t *testing.T, path string) { writeACPXProviderStubsForGenerationTest(t, path, false) }, run: runBlockedACPXInteractiveForGenerationTest},
			{name: "legacy execute", run: runBlockedLegacyForGenerationTest(false)},
			{name: "legacy execute interactive", run: runBlockedLegacyForGenerationTest(true)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newProviderGenerationFixture(t)
				sideEffectPath := filepath.Join(t.TempDir(), "provider-started")
				if tc.prepare != nil {
					tc.prepare(t, sideEffectPath)
				}
				atBoundary := make(chan struct{})
				allowGate := make(chan struct{})
				realGate := newProviderLaunchGate(fixture.config(fixture.authorityA))
				pausedGate := func(ctx context.Context, start func() error) error {
					close(atBoundary)
					<-allowGate
					return realGate(ctx, start)
				}

				runDone := make(chan error, 1)
				go func() {
					runDone <- tc.run(context.Background(), pausedGate, fixture.authorityA.Generation, sideEffectPath)
				}()
				<-atBoundary

				fixture.expireCurrent(t)
				authorityB := fixture.registerReplacement(t)
				close(allowGate)
				err := <-runDone
				if err == nil || !ops.IsAgentAuthorityError(err) {
					t.Fatalf("stale launch error = %v, want AgentAuthorityError", err)
				}
				if _, statErr := os.Stat(sideEffectPath); !stderrors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("stale generation performed provider side effect: %v", statErr)
				}
				assertCurrentGeneration(t, fixture.bb, authorityB)
			})
		}
	})

	t.Run("built-ins release registration after start before wait", func(t *testing.T) {
		cases := []struct {
			name    string
			prepare func(*testing.T, string)
			run     func(context.Context, LLMAgentLaunchGate, string, string) error
		}{
			{name: "cli", prepare: func(t *testing.T, path string) { writeCLIProviderStubForGenerationTest(t, path, true) }, run: runSleepingCLIForGenerationTest},
			{name: "cli interactive", prepare: func(t *testing.T, path string) { writeCLIProviderStubForGenerationTest(t, path, true) }, run: runSleepingCLIInteractiveForGenerationTest},
			{name: "acpx", prepare: func(t *testing.T, path string) { writeACPXProviderStubsForGenerationTest(t, path, true) }, run: runSleepingACPXForGenerationTest},
			{name: "acpx interactive", prepare: func(t *testing.T, path string) { writeACPXProviderStubsForGenerationTest(t, path, true) }, run: runSleepingACPXInteractiveForGenerationTest},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newProviderGenerationFixture(t)
				fixture.expireCurrent(t)
				sideEffectPath := filepath.Join(t.TempDir(), "provider-started")
				tc.prepare(t, sideEffectPath)
				atStart := make(chan struct{})
				allowStart := make(chan struct{})
				realGate := newProviderLaunchGate(fixture.config(fixture.authorityA))
				pausedInsideLock := func(ctx context.Context, start func() error) error {
					return realGate(ctx, func() error {
						close(atStart)
						<-allowStart
						return start()
					})
				}

				runDone := make(chan error, 1)
				go func() {
					runDone <- tc.run(context.Background(), pausedInsideLock, fixture.authorityA.Generation, sideEffectPath)
				}()
				<-atStart

				registrationDone := make(chan models.AgentAuthority, 1)
				registrationErr := make(chan error, 1)
				go func() {
					authority, err := registerAgentWithAuthority(fixture.bb, fixture.projectRoot, fixture.agentID, "coder", "terminal-b", 1800, "claude", fixture.resolver)
					if err != nil {
						registrationErr <- err
						return
					}
					registrationDone <- authority
				}()
				select {
				case <-registrationDone:
					t.Fatal("replacement registered before authorized provider start")
				case err := <-registrationErr:
					t.Fatalf("replacement registration failed before start: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				close(allowStart)

				var authorityB models.AgentAuthority
				select {
				case authorityB = <-registrationDone:
				case err := <-registrationErr:
					t.Fatalf("replacement registration failed: %v", err)
				case <-time.After(700 * time.Millisecond):
					t.Fatal("replacement waited for built-in provider completion instead of start")
				}
				if _, err := os.Stat(sideEffectPath); err != nil {
					t.Fatalf("provider did not reach start boundary: %v", err)
				}
				if err := <-runDone; err != nil {
					t.Fatalf("provider run: %v", err)
				}
				assertCurrentGeneration(t, fixture.bb, authorityB)
			})
		}
	})

	t.Run("legacy adapter serializes the complete blocking call", func(t *testing.T) {
		for _, interactive := range []bool{false, true} {
			name := "execute"
			if interactive {
				name = "execute interactive"
			}
			t.Run(name, func(t *testing.T) {
				fixture := newProviderGenerationFixture(t)
				fixture.expireCurrent(t)
				started := make(chan struct{})
				release := make(chan struct{})
				executor := &blockingLegacyGenerationExecutor{started: started, release: release}
				adapter := legacyCLIExecutorAdapter{executor: executor}
				gate := newProviderLaunchGate(fixture.config(fixture.authorityA))
				runDone := make(chan error, 1)
				go func() {
					if interactive {
						_, err := adapter.RunInteractive(context.Background(), LLMAgentInteractiveRequest{BackendName: "codex", AgentID: fixture.agentID, Generation: fixture.authorityA.Generation, ProjectRoot: fixture.projectRoot, LaunchGate: gate})
						runDone <- err
						return
					}
					_, err := adapter.Run(context.Background(), LLMAgentRunRequest{BackendName: "codex", AgentID: fixture.agentID, Generation: fixture.authorityA.Generation, ProjectRoot: fixture.projectRoot, LaunchGate: gate})
					runDone <- err
				}()
				<-started

				registrationDone := make(chan error, 1)
				go func() {
					_, err := registerAgentWithAuthority(fixture.bb, fixture.projectRoot, fixture.agentID, "coder", "terminal-b", 1800, "claude", fixture.resolver)
					registrationDone <- err
				}()
				select {
				case err := <-registrationDone:
					t.Fatalf("replacement escaped legacy blocking-call serialization: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				close(release)
				if err := <-runDone; err != nil {
					t.Fatalf("legacy run: %v", err)
				}
				if err := <-registrationDone; err != nil {
					t.Fatalf("replacement after legacy completion: %v", err)
				}
			})
		}
	})
}

func TestProviderLaunchFenceFailureSemantics(t *testing.T) {
	t.Run("lock timeout", func(t *testing.T) {
		fixture := newProviderGenerationFixture(t)
		restore := ops.SetAgentLifecycleLockTimeoutForTest(40 * time.Millisecond)
		t.Cleanup(restore)
		holderAcquired := make(chan struct{})
		releaseHolder := make(chan struct{})
		holderDone := make(chan error, 1)
		go func() {
			holderDone <- ops.WithAgentLifecycleLock(context.Background(), fixture.projectRoot, fixture.agentID, "holder", func() error {
				close(holderAcquired)
				<-releaseHolder
				return nil
			})
		}()
		<-holderAcquired
		started := false
		err := newProviderLaunchGate(fixture.config(fixture.authorityA))(context.Background(), func() error {
			started = true
			return nil
		})
		if err == nil || started {
			t.Fatalf("timeout err=%v started=%v, want failure before start", err, started)
		}
		close(releaseHolder)
		if err := <-holderDone; err != nil {
			t.Fatal(err)
		}
		assertAgentLifecycleLockAvailable(t, fixture.projectRoot, fixture.agentID)
	})

	t.Run("state read failure and generation mismatch", func(t *testing.T) {
		cases := []struct {
			name string
			gate func(t *testing.T, fixture providerGenerationFixture) LLMAgentLaunchGate
		}{
			{
				name: "state read",
				gate: func(t *testing.T, fixture providerGenerationFixture) LLMAgentLaunchGate {
					config := fixture.config(fixture.authorityA)
					config.StatePath = filepath.Join(fixture.projectRoot, paths.ProjectDirName(), "missing-state.yaml")
					return newProviderLaunchGate(config)
				},
			},
			{
				name: "mismatch",
				gate: func(t *testing.T, fixture providerGenerationFixture) LLMAgentLaunchGate {
					fixture.expireCurrent(t)
					fixture.registerReplacement(t)
					return newProviderLaunchGate(fixture.config(fixture.authorityA))
				},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newProviderGenerationFixture(t)
				gate := tc.gate(t, fixture)
				before, err := fixture.bb.ReadRaw()
				if err != nil {
					t.Fatal(err)
				}
				started := false
				err = gate(context.Background(), func() error { started = true; return nil })
				if err == nil || started {
					t.Fatalf("gate err=%v started=%v, want failure before start", err, started)
				}
				after, err := fixture.bb.ReadRaw()
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("failed launch rewrote state")
				}
				assertAgentLifecycleLockAvailable(t, fixture.projectRoot, fixture.agentID)
			})
		}
	})

	t.Run("acpx setup and process start failure", func(t *testing.T) {
		fixture := newProviderGenerationFixture(t)
		before, err := fixture.bb.ReadRaw()
		if err != nil {
			t.Fatal(err)
		}
		gate := newProviderLaunchGate(fixture.config(fixture.authorityA))
		sink := &recordingLLMAgentEventSink{}
		binDir := t.TempDir()
		acpxPath := filepath.Join(binDir, "acpx")
		testhelpers.WriteShellStub(t, acpxPath, "#!/bin/sh\nexit 7\n")
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		_, setupErr := NewACPXAgent("").Run(context.Background(), LLMAgentRunRequest{BackendName: "codex-acp", AgentID: fixture.agentID, Generation: fixture.authorityA.Generation, ProjectRoot: fixture.projectRoot, Prompt: "prompt", EventSink: sink, LaunchGate: gate})
		if setupErr == nil || hasLLMAgentEvent(sink.Events(), LLMAgentEventStarted) {
			t.Fatalf("ACPX setup err=%v events=%#v, want no successful start", setupErr, sink.Events())
		}
		assertAgentLifecycleLockAvailable(t, fixture.projectRoot, fixture.agentID)

		startSink := &recordingLLMAgentEventSink{}
		missingTool := "missing-generation-test-cli"
		runtimeConfig := models.Config{AgentTools: map[string]models.AgentToolConfig{
			missingTool: {
				Backend:         "cli",
				Executable:      filepath.Join(binDir, "missing-executable"),
				PromptTransport: PromptTransportStdin,
			},
		}}
		_, startErr := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{BackendName: missingTool, AgentID: fixture.agentID, Generation: fixture.authorityA.Generation, ProjectRoot: fixture.projectRoot, Prompt: "prompt", RuntimeConfig: runtimeConfig, EventSink: startSink, LaunchGate: gate})
		if startErr == nil || hasLLMAgentEvent(startSink.Events(), LLMAgentEventStarted) {
			t.Fatalf("CLI start err=%v events=%#v, want no successful start", startErr, startSink.Events())
		}
		assertAgentLifecycleLockAvailable(t, fixture.projectRoot, fixture.agentID)
		after, err := fixture.bb.ReadRaw()
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("provider failure rewrote blackboard state")
		}
		fixture.expireCurrent(t)
		fixture.registerReplacement(t)
	})
}

func TestAgentProcessGenerationEnv(t *testing.T) {
	withAgentBrandValues(t, func() { brand.EnvPrefix = "ACME" })
	const generation = "generation-current"
	base := []string{"ACME_AGENT_GENERATION=stale-brand", "LIZA_AGENT_GENERATION=stale-legacy", "KEEP=value"}
	env := agentProcessEnv(base, "coder-1", generation)
	for _, want := range []string{"ACME_AGENT_GENERATION=" + generation, "LIZA_AGENT_GENERATION=" + generation, "KEEP=value"} {
		if !containsExactString(env, want) {
			t.Fatalf("agent process env = %#v, missing %q", env, want)
		}
	}
	if containsExactString(env, "ACME_AGENT_GENERATION=stale-brand") || containsExactString(env, "LIZA_AGENT_GENERATION=stale-legacy") {
		t.Fatalf("agent process env retained stale generation: %#v", env)
	}

	t.Run("normal and interactive built-ins pass generation", func(t *testing.T) {
		for _, interactive := range []bool{false, true} {
			for _, backend := range []string{"cli", "acpx"} {
				name := backend
				if interactive {
					name += " interactive"
				}
				t.Run(name, func(t *testing.T) {
					logPath := filepath.Join(t.TempDir(), "env.log")
					if err := runGenerationEnvProcessForTest(t, context.Background(), backend, interactive, generation, logPath); err != nil {
						t.Fatal(err)
					}
					data, err := os.ReadFile(logPath)
					if err != nil {
						t.Fatal(err)
					}
					got := string(data)
					if !strings.Contains(got, "brand:"+generation) || !strings.Contains(got, "legacy:"+generation) {
						t.Fatalf("provider env log = %q, want branded and legacy generation", got)
					}
				})
			}
		}
	})
}

func TestSupervisorGenerationCancellation(t *testing.T) {
	fixture := newProviderGenerationFixture(t)
	heartbeatA := NewHeartbeat(HeartbeatConfig{Authority: fixture.authorityA, StatePath: fixture.statePath, Interval: 10 * time.Millisecond, LeaseDuration: time.Minute})
	fixture.expireCurrent(t)
	authorityB := fixture.registerReplacement(t)
	heartbeatB := NewHeartbeat(HeartbeatConfig{Authority: authorityB, StatePath: fixture.statePath, Interval: 10 * time.Millisecond, LeaseDuration: time.Minute})
	if err := heartbeatB.beat(); err != nil {
		t.Fatalf("generation B heartbeat: %v", err)
	}

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	heartbeatErr := make(chan error, 1)
	stopA := startSupervisorHeartbeat(ctxA, heartbeatA.Start, func(err error) {
		heartbeatErr <- err
		cancelA()
	})
	defer stopA()

	select {
	case err := <-heartbeatErr:
		if !ops.IsAgentAuthorityError(err) {
			t.Fatalf("generation A heartbeat error = %v, want authority mismatch", err)
		}
	case <-time.After(time.Second):
		t.Fatal("generation mismatch did not stop A supervisor context")
	}
	select {
	case <-ctxA.Done():
	case <-time.After(time.Second):
		t.Fatal("generation mismatch did not cancel A provider context")
	}
	if err := heartbeatB.beat(); err != nil {
		t.Fatalf("generation B did not continue after A cancellation: %v", err)
	}
}

type providerGenerationFixture struct {
	projectRoot string
	statePath   string
	agentID     string
	bb          *db.Blackboard
	resolver    *pipeline.Resolver
	authorityA  models.AgentAuthority
}

func newProviderGenerationFixture(t *testing.T) providerGenerationFixture {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	bb := testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	resolver := testResolver(t)
	authorityA, err := registerAgentWithAuthority(bb, projectRoot, "coder-1", "coder", "terminal-a", 1800, "codex", resolver)
	if err != nil {
		t.Fatalf("register generation A: %v", err)
	}
	return providerGenerationFixture{projectRoot: projectRoot, statePath: statePath, agentID: "coder-1", bb: bb, resolver: resolver, authorityA: authorityA}
}

func (f providerGenerationFixture) config(authority models.AgentAuthority) SupervisorConfig {
	return SupervisorConfig{AgentID: f.agentID, Authority: authority, ProjectRoot: f.projectRoot, StatePath: f.statePath, CLIName: "gemini", ExecutionTimeout: 5 * time.Second}
}

func (f providerGenerationFixture) expireCurrent(t *testing.T) {
	t.Helper()
	if err := f.bb.Modify(func(state *models.State) error {
		agent := state.Agents[f.agentID]
		expired := time.Now().UTC().Add(-time.Minute)
		agent.LeaseExpires = &expired
		state.Agents[f.agentID] = agent
		return nil
	}); err != nil {
		t.Fatalf("expire generation: %v", err)
	}
}

func (f providerGenerationFixture) registerReplacement(t *testing.T) models.AgentAuthority {
	t.Helper()
	authority, err := registerAgentWithAuthority(f.bb, f.projectRoot, f.agentID, "coder", "terminal-b", 1800, "claude", f.resolver)
	if err != nil {
		t.Fatalf("register generation B: %v", err)
	}
	return authority
}

func assertCurrentGeneration(t *testing.T, bb *db.Blackboard, authority models.AgentAuthority) {
	t.Helper()
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Agents[authority.ID].Generation; got != authority.Generation {
		t.Fatalf("current generation = %q, want %q", got, authority.Generation)
	}
}

func assertAgentLifecycleLockAvailable(t *testing.T, projectRoot, agentID string) {
	t.Helper()
	if err := ops.WithAgentLifecycleLock(context.Background(), projectRoot, agentID, "verify failure release", func() error {
		return nil
	}); err != nil {
		t.Fatalf("reacquire agent lifecycle lock: %v", err)
	}
}

func runBlockedCLIForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
	return runCLIProviderForGenerationTest(ctx, gate, generation, sideEffectPath, false)
}

func runBlockedCLIInteractiveForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
	return runCLIProviderForGenerationTest(ctx, gate, generation, sideEffectPath, true)
}

func runSleepingCLIForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
	return runCLIProviderForGenerationTest(ctx, gate, generation, sideEffectPath, false)
}

func runSleepingCLIInteractiveForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
	return runCLIProviderForGenerationTest(ctx, gate, generation, sideEffectPath, true)
}

func runCLIProviderForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string, interactive bool) error {
	binDir := filepath.Dir(sideEffectPath)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath); err != nil {
		return err
	}
	defer os.Setenv("PATH", oldPath)
	agent := NewCLIAgent("")
	if interactive {
		_, err := agent.RunInteractive(ctx, LLMAgentInteractiveRequest{BackendName: "gemini", AgentID: "coder-1", Generation: generation, ProjectRoot: binDir, LaunchGate: gate})
		return err
	}
	_, err := agent.Run(ctx, LLMAgentRunRequest{BackendName: "gemini", AgentID: "coder-1", Generation: generation, ProjectRoot: binDir, Prompt: "prompt", LaunchGate: gate})
	return err
}

func runBlockedACPXForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
	return runACPXProviderForGenerationTest(ctx, gate, generation, sideEffectPath, false)
}

func runBlockedACPXInteractiveForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
	return runACPXProviderForGenerationTest(ctx, gate, generation, sideEffectPath, true)
}

func runSleepingACPXForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
	return runACPXProviderForGenerationTest(ctx, gate, generation, sideEffectPath, false)
}

func runSleepingACPXInteractiveForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
	return runACPXProviderForGenerationTest(ctx, gate, generation, sideEffectPath, true)
}

func runACPXProviderForGenerationTest(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string, interactive bool) error {
	binDir := filepath.Dir(sideEffectPath)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath); err != nil {
		return err
	}
	defer os.Setenv("PATH", oldPath)
	agent := NewACPXAgent("")
	if interactive {
		_, err := agent.RunInteractive(ctx, LLMAgentInteractiveRequest{BackendName: "codex-acp", AgentID: "coder-1", Generation: generation, ProjectRoot: binDir, LaunchGate: gate})
		return err
	}
	_, err := agent.Run(ctx, LLMAgentRunRequest{BackendName: "codex-acp", AgentID: "coder-1", Generation: generation, ProjectRoot: binDir, Prompt: "prompt", LaunchGate: gate})
	return err
}

func runBlockedLegacyForGenerationTest(interactive bool) func(context.Context, LLMAgentLaunchGate, string, string) error {
	return func(ctx context.Context, gate LLMAgentLaunchGate, generation, sideEffectPath string) error {
		executor := &sideEffectLegacyGenerationExecutor{sideEffectPath: sideEffectPath}
		adapter := legacyCLIExecutorAdapter{executor: executor}
		if interactive {
			_, err := adapter.RunInteractive(ctx, LLMAgentInteractiveRequest{BackendName: "codex", AgentID: "coder-1", Generation: generation, ProjectRoot: filepath.Dir(sideEffectPath), LaunchGate: gate})
			return err
		}
		_, err := adapter.Run(ctx, LLMAgentRunRequest{BackendName: "codex", AgentID: "coder-1", Generation: generation, ProjectRoot: filepath.Dir(sideEffectPath), LaunchGate: gate})
		return err
	}
}

type sideEffectLegacyGenerationExecutor struct{ sideEffectPath string }

func (e *sideEffectLegacyGenerationExecutor) Execute(context.Context, string, string, string, string, []string, models.Config) (CLIExecutionResult, error) {
	return CLIExecutionResult{}, os.WriteFile(e.sideEffectPath, []byte("started"), 0o600)
}

func (e *sideEffectLegacyGenerationExecutor) ExecuteInteractive(context.Context, string, string, string, []string) (int, error) {
	return 0, os.WriteFile(e.sideEffectPath, []byte("started"), 0o600)
}

type blockingLegacyGenerationExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *blockingLegacyGenerationExecutor) wait() {
	e.once.Do(func() { close(e.started) })
	<-e.release
}

func (e *blockingLegacyGenerationExecutor) Execute(context.Context, string, string, string, string, []string, models.Config) (CLIExecutionResult, error) {
	e.wait()
	return CLIExecutionResult{}, nil
}

func (e *blockingLegacyGenerationExecutor) ExecuteInteractive(context.Context, string, string, string, []string) (int, error) {
	e.wait()
	return 0, nil
}

func containsExactString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeCLIProviderStubForGenerationTest(t *testing.T, sideEffectPath string, sleeping bool) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\nprintf started > %q\n", filepath.ToSlash(sideEffectPath))
	if sleeping {
		script += "sleep 1\n"
	}
	testhelpers.WriteShellStub(t, filepath.Join(filepath.Dir(sideEffectPath), "gemini"), script)
}

func writeACPXProviderStubsForGenerationTest(t *testing.T, sideEffectPath string, sleeping bool) {
	t.Helper()
	shellSideEffectPath := filepath.ToSlash(sideEffectPath)
	script := "#!/bin/sh\n"
	if sleeping {
		script += fmt.Sprintf("case \"$*\" in *\" prompt \"*) printf started > %q; sleep 1; printf '%%s\\n' '{\"result\":{}}';; esac\n", shellSideEffectPath)
	} else {
		script += fmt.Sprintf("case \"$*\" in *\" prompt \"*) printf started > %q; printf '%%s\\n' '{\"result\":{}}';; esac\n", shellSideEffectPath)
	}
	binDir := filepath.Dir(sideEffectPath)
	testhelpers.WriteShellStub(t, filepath.Join(binDir, "acpx"), script)
	interactiveScript := fmt.Sprintf("#!/bin/sh\nprintf started > %q\n", shellSideEffectPath)
	if sleeping {
		interactiveScript += "sleep 1\n"
	}
	testhelpers.WriteShellStub(t, filepath.Join(binDir, "codex"), interactiveScript)
}

func runGenerationEnvProcessForTest(t *testing.T, ctx context.Context, backend string, interactive bool, generation, logPath string) error {
	t.Helper()
	binDir := filepath.Dir(logPath)
	script := fmt.Sprintf("#!/bin/sh\nprintf 'brand:%%s\\nlegacy:%%s\\n' \"$ACME_AGENT_GENERATION\" \"$LIZA_AGENT_GENERATION\" >> %q\n", filepath.ToSlash(logPath))
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath); err != nil {
		return err
	}
	defer os.Setenv("PATH", oldPath)
	gate := LLMAgentLaunchGate(func(_ context.Context, start func() error) error { return start() })
	if backend == "cli" {
		testhelpers.WriteShellStub(t, filepath.Join(binDir, "gemini"), script)
		agent := NewCLIAgent("")
		if interactive {
			_, err := agent.RunInteractive(ctx, LLMAgentInteractiveRequest{BackendName: "gemini", AgentID: "coder-1", Generation: generation, ProjectRoot: binDir, LaunchGate: gate})
			return err
		}
		_, err := agent.Run(ctx, LLMAgentRunRequest{BackendName: "gemini", AgentID: "coder-1", Generation: generation, ProjectRoot: binDir, Prompt: "prompt", LaunchGate: gate})
		return err
	}
	if interactive {
		testhelpers.WriteShellStub(t, filepath.Join(binDir, "codex"), script)
		_, err := NewACPXAgent("").RunInteractive(ctx, LLMAgentInteractiveRequest{BackendName: "codex-acp", AgentID: "coder-1", Generation: generation, ProjectRoot: binDir, LaunchGate: gate})
		return err
	}
	acpxScript := script + "case \"$*\" in *\" prompt \"*) printf '%s\\n' '{\"result\":{}}';; esac\n"
	testhelpers.WriteShellStub(t, filepath.Join(binDir, "acpx"), acpxScript)
	_, err := NewACPXAgent("").Run(ctx, LLMAgentRunRequest{BackendName: "codex-acp", AgentID: "coder-1", Generation: generation, ProjectRoot: binDir, Prompt: "prompt", LaunchGate: gate})
	return err
}
