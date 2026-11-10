package providersignal

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/paths"
)

// #114: an expired quota signal admits one recovery attempt per provider and
// project, and its outcome settles the episode.

const quotaProbeTestProvider = "claude"

func writeExpiredQuotaSignalFixture(t *testing.T, projectRoot, detected string) {
	t.Helper()
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	writeRawQuotaSignal(t, projectRoot, quotaProbeTestProvider, "provider: claude\ndetected: "+detected+"\nmessage: limit\nresets_at: unknown\nexpires: "+past+"\n")
}

func newExpiredQuotaProject(t *testing.T) string {
	t.Helper()
	projectRoot := t.TempDir()
	writeExpiredQuotaSignalFixture(t, projectRoot, time.Now().UTC().Add(-2*time.Hour).Format(time.RFC3339))
	return projectRoot
}

func admit(t *testing.T, projectRoot, agentID string) QuotaAdmissionResult {
	t.Helper()
	result, err := AdmitQuotaStart(projectRoot, quotaProbeTestProvider, agentID)
	if err != nil {
		t.Fatalf("AdmitQuotaStart(%s): %v", agentID, err)
	}
	return result
}

func resolve(t *testing.T, projectRoot, agentID string, outcome QuotaProbeOutcome) {
	t.Helper()
	if err := ResolveQuotaProbe(projectRoot, quotaProbeTestProvider, agentID, outcome); err != nil {
		t.Fatalf("ResolveQuotaProbe(%s): %v", agentID, err)
	}
}

func quotaFileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

func alertsText(t *testing.T, projectRoot string) string {
	t.Helper()
	data, err := os.ReadFile(paths.New(projectRoot).AlertsLogPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func writeLease(t *testing.T, projectRoot string, lease quotaLease) {
	t.Helper()
	if err := writeQuotaLease(projectRoot, quotaProbeTestProvider, lease); err != nil {
		t.Fatal(err)
	}
}

func stubProcessAlive(t *testing.T, alive func(pid int) bool) {
	t.Helper()
	previous := processAlive
	processAlive = alive
	t.Cleanup(func() { processAlive = previous })
}

func TestAdmitQuotaStart_LifecycleStates(t *testing.T) {
	t.Run("no signal is clear and takes no lease", func(t *testing.T) {
		projectRoot := t.TempDir()
		if got := admit(t, projectRoot, "coder-1"); got.Admission != QuotaClear {
			t.Fatalf("admission = %+v, want clear", got)
		}
		if quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
			t.Fatal("a clear admission wrote a recovery lease")
		}
	})
	t.Run("unexpired signal refuses with its expiry", func(t *testing.T) {
		projectRoot := t.TempDir()
		makeQuotaProjectDir(t, projectRoot)
		resetsAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		if err := RaiseQuotaExhaustion(projectRoot, &QuotaExhaustion{Provider: quotaProbeTestProvider, Message: "limit", ResetsAt: resetsAt}); err != nil {
			t.Fatal(err)
		}
		got := admit(t, projectRoot, "coder-1")
		if got.Admission != QuotaRefused || !got.Until.Equal(resetsAt.Add(quotaResetGrace)) || got.AttemptHolder != "" {
			t.Fatalf("admission = %+v, want refusal until %v", got, resetsAt.Add(quotaResetGrace))
		}
	})
	t.Run("expired signal admits one probe that names its holder", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		if got := admit(t, projectRoot, "coder-1"); got.Admission != QuotaProbe {
			t.Fatalf("first admission = %+v, want probe", got)
		}
		if got := admit(t, projectRoot, "coder-1"); got.Admission != QuotaProbe {
			t.Fatalf("holder re-admission = %+v, want its own probe", got)
		}
		got := admit(t, projectRoot, "coder-2")
		if got.Admission != QuotaRefused || !strings.Contains(got.AttemptHolder, "probe by coder-1") {
			t.Fatalf("second agent admission = %+v, want refusal naming coder-1's probe", got)
		}
		if alerts := alertsText(t, projectRoot); alerts != "" {
			t.Fatalf("a routine probe start raised an alert:\n%s", alerts)
		}
	})
	t.Run("expired signal converts a live reservation into the probe", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		if got, err := ReserveQuotaProbe(projectRoot, quotaProbeTestProvider, "coder"); err != nil || got.Admission != QuotaReserved {
			t.Fatalf("ReserveQuotaProbe = %+v, %v; want reserved", got, err)
		}
		if got := admit(t, projectRoot, "coder-7"); got.Admission != QuotaProbe {
			t.Fatalf("admission over the reservation = %+v, want probe", got)
		}
		if got := admit(t, projectRoot, "coder-8"); got.Admission != QuotaRefused {
			t.Fatalf("admission after the conversion = %+v, want refusal", got)
		}
	})
}

func TestAdmitQuotaStart_ConcurrentAdmissionsElectOneProbe(t *testing.T) {
	projectRoot := newExpiredQuotaProject(t)
	const callers = 8
	results := make([]QuotaAdmissionResult, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = admit(t, projectRoot, "coder-"+string(rune('a'+i)))
		}(i)
	}
	wg.Wait()
	probes := 0
	for _, result := range results {
		switch result.Admission {
		case QuotaProbe:
			probes++
		case QuotaRefused:
		default:
			t.Fatalf("admission = %+v, want probe or refusal", result)
		}
	}
	if probes != 1 {
		t.Fatalf("probes = %d of %d concurrent admissions, want exactly 1", probes, callers)
	}
}

func TestAdmitQuotaStart_StaleLeases(t *testing.T) {
	t.Run("a probe whose holder died is taken over", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		writeLease(t, projectRoot, quotaLease{Kind: quotaLeaseProbe, Holder: "coder-1", PID: 424242, Acquired: time.Now().UTC()})
		stubProcessAlive(t, func(pid int) bool { return pid != 424242 })
		if got := admit(t, projectRoot, "coder-2"); got.Admission != QuotaProbe {
			t.Fatalf("admission over a dead holder = %+v, want probe", got)
		}
	})
	t.Run("a live probe is never taken over, however old", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		writeLease(t, projectRoot, quotaLease{Kind: quotaLeaseProbe, Holder: "coder-1", PID: 424242, Acquired: time.Now().UTC().Add(-72 * time.Hour)})
		stubProcessAlive(t, func(int) bool { return true })
		if got := admit(t, projectRoot, "coder-2"); got.Admission != QuotaRefused {
			t.Fatalf("admission over a live 72h probe = %+v, want refusal", got)
		}
	})
	t.Run("a reservation past its hold is taken over", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		now := time.Now().UTC()
		writeLease(t, projectRoot, quotaLease{Kind: quotaLeaseReservation, Holder: "spawn:coder", PID: os.Getpid(), Acquired: now.Add(-time.Hour), Expires: now.Add(-time.Minute)})
		got, err := ReserveQuotaProbe(projectRoot, quotaProbeTestProvider, "reviewer")
		if err != nil || got.Admission != QuotaReserved {
			t.Fatalf("ReserveQuotaProbe over an expired reservation = %+v, %v; want reserved", got, err)
		}
	})
	t.Run("a freshly unreadable lease fails closed", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		if err := os.WriteFile(QuotaProbePath(projectRoot, quotaProbeTestProvider), []byte("garbage"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := admit(t, projectRoot, "coder-2"); got.Admission != QuotaRefused {
			t.Fatalf("admission over an unreadable lease = %+v, want refusal", got)
		}
	})
}

func TestResolveQuotaProbe_Outcomes(t *testing.T) {
	t.Run("recovered clears the signal it was admitted on and announces it", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		admit(t, projectRoot, "coder-1")
		resolve(t, projectRoot, "coder-1", QuotaProbeRecovered)
		if quotaFileExists(t, QuotaSignalPath(projectRoot, quotaProbeTestProvider)) || quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
			t.Fatal("recovery left the signal or the lease")
		}
		if alerts := alertsText(t, projectRoot); !strings.Contains(alerts, "PROVIDER QUOTA RECOVERED — claude: provider answered coder-1's recovery probe") {
			t.Fatalf("alerts lack the recovery:\n%s", alerts)
		}
		if got := admit(t, projectRoot, "coder-2"); got.Admission != QuotaClear {
			t.Fatalf("admission after recovery = %+v, want clear", got)
		}
	})
	t.Run("inconclusive frees the lease and keeps the signal", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		admit(t, projectRoot, "coder-1")
		resolve(t, projectRoot, "coder-1", QuotaProbeInconclusive)
		if !quotaFileExists(t, QuotaSignalPath(projectRoot, quotaProbeTestProvider)) || quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
			t.Fatal("inconclusive probe must keep the signal and free the lease")
		}
		if alerts := alertsText(t, projectRoot); !strings.Contains(alerts, "PROVIDER QUOTA PROBE INCONCLUSIVE") || strings.Contains(alerts, "PROVIDER QUOTA RECOVERED") {
			t.Fatalf("alerts = %s, want inconclusive and no recovery", alerts)
		}
		if got := admit(t, projectRoot, "coder-2"); got.Admission != QuotaProbe {
			t.Fatalf("next admission = %+v, want the next probe", got)
		}
	})
	t.Run("a non-holder cannot settle the probe", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		admit(t, projectRoot, "coder-1")
		resolve(t, projectRoot, "coder-2", QuotaProbeRecovered)
		resolve(t, projectRoot, "coder-2", QuotaProbeInconclusive)
		if !quotaFileExists(t, QuotaSignalPath(projectRoot, quotaProbeTestProvider)) || !quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
			t.Fatal("a non-holder's resolve changed the lifecycle")
		}
	})
	t.Run("resolve without a probe is a no-op", func(t *testing.T) {
		projectRoot := t.TempDir()
		resolve(t, projectRoot, "coder-1", QuotaProbeRecovered)
	})
}

func TestRaiseQuotaExhaustionBy_SettlesOnlyItsOwnAttempt(t *testing.T) {
	exhaustion := &QuotaExhaustion{Provider: quotaProbeTestProvider, Message: "You've hit your usage limit"}
	t.Run("the probe's own renewed exhaustion re-blocks and frees its lease", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		admit(t, projectRoot, "coder-1")
		if err := RaiseQuotaExhaustionBy(projectRoot, exhaustion, "coder-1"); err != nil {
			t.Fatal(err)
		}
		if !CheckQuotaSignal(projectRoot, quotaProbeTestProvider) || quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
			t.Fatal("the probe's exhaustion must re-block the provider and free the lease")
		}
		if got := admit(t, projectRoot, "coder-2"); got.Admission != QuotaRefused || got.AttemptHolder != "" {
			t.Fatalf("admission after the failed probe = %+v, want the renewed block", got)
		}
	})
	t.Run("a raise drops a spawn reservation", func(t *testing.T) {
		projectRoot := newExpiredQuotaProject(t)
		if _, err := ReserveQuotaProbe(projectRoot, quotaProbeTestProvider, "coder"); err != nil {
			t.Fatal(err)
		}
		if err := RaiseQuotaExhaustion(projectRoot, exhaustion); err != nil {
			t.Fatal(err)
		}
		if quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
			t.Fatal("a raise kept a reservation no provider ran under")
		}
	})
}

// P1 sequence (plan r2): A probes; B, an in-flight pre-expiry turn, raises a
// newer exhaustion that then expires while A still runs; C is refused until A
// ends; A's success is superseded, not announced as a recovery.
func TestQuotaRecovery_ThirdPartyRaiseKeepsTheRunningProbe(t *testing.T) {
	projectRoot := newExpiredQuotaProject(t)
	if got := admit(t, projectRoot, "coder-a"); got.Admission != QuotaProbe {
		t.Fatalf("A admission = %+v, want probe", got)
	}
	if err := RaiseQuotaExhaustionBy(projectRoot, &QuotaExhaustion{Provider: quotaProbeTestProvider, Message: "limit"}, "coder-b"); err != nil {
		t.Fatal(err)
	}
	if !quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
		t.Fatal("B's raise dropped A's running probe")
	}
	newer := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	writeExpiredQuotaSignalFixture(t, projectRoot, newer) // B's block expires while A runs

	if got := admit(t, projectRoot, "coder-c"); got.Admission != QuotaRefused || !strings.Contains(got.AttemptHolder, "coder-a") {
		t.Fatalf("C admission while A runs = %+v, want refusal naming A", got)
	}
	if !CheckQuotaSignal(projectRoot, quotaProbeTestProvider) {
		t.Fatal("running agents must stop while A's probe runs")
	}

	resolve(t, projectRoot, "coder-a", QuotaProbeRecovered)
	if !quotaFileExists(t, QuotaSignalPath(projectRoot, quotaProbeTestProvider)) {
		t.Fatal("A's stale success erased the newer exhaustion")
	}
	alerts := alertsText(t, projectRoot)
	if !strings.Contains(alerts, "PROVIDER QUOTA PROBE SUPERSEDED — claude: coder-a's recovery probe got a provider answer, but a newer quota block detected at "+newer) {
		t.Fatalf("alerts lack the superseded outcome:\n%s", alerts)
	}
	if strings.Contains(alerts, "PROVIDER QUOTA RECOVERED") {
		t.Fatalf("a superseded probe announced recovery:\n%s", alerts)
	}
	if got := admit(t, projectRoot, "coder-c"); got.Admission != QuotaProbe {
		t.Fatalf("C admission after A ended = %+v, want the next probe", got)
	}
}

// P1 (plan r2/r3): the automatic resume sweep never turns an unproven expired
// episode into a clear state, with or without a lease.
func TestQuotaRecovery_ReportKeepsTheEpisode(t *testing.T) {
	for _, c := range []struct {
		name    string
		prepare func(t *testing.T, projectRoot string)
	}{
		{"no lease yet", func(*testing.T, string) {}},
		{"spawn reservation", func(t *testing.T, projectRoot string) {
			if _, err := ReserveQuotaProbe(projectRoot, quotaProbeTestProvider, "coder"); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			projectRoot := newExpiredQuotaProject(t)
			c.prepare(t, projectRoot)

			report := ReportQuotaSignals(projectRoot)

			if len(report.Recovering) != 1 || report.Recovering[0].Provider != quotaProbeTestProvider {
				t.Fatalf("report = %+v, want claude recovering", report)
			}
			if !quotaFileExists(t, QuotaSignalPath(projectRoot, quotaProbeTestProvider)) {
				t.Fatal("the report removed an unproven expired signal")
			}
			var wg sync.WaitGroup
			results := make([]QuotaAdmissionResult, 2)
			for i := range results {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					results[i] = admit(t, projectRoot, []string{"coder-1", "coder-2"}[i])
				}(i)
			}
			wg.Wait()
			if probes := countAdmissions(results, QuotaProbe); probes != 1 || countAdmissions(results, QuotaClear) != 0 {
				t.Fatalf("admissions after the report = %+v, want one probe and no clear start", results)
			}
		})
	}
}

func countAdmissions(results []QuotaAdmissionResult, admission QuotaAdmission) int {
	n := 0
	for _, result := range results {
		if result.Admission == admission {
			n++
		}
	}
	return n
}

func TestQuotaRecovery_CallerSemantics(t *testing.T) {
	projectRoot := newExpiredQuotaProject(t)
	if CheckQuotaSignal(projectRoot, quotaProbeTestProvider) {
		t.Fatal("an expired signal without an attempt must not stop running agents")
	}
	if _, blocked := QuotaBlockedUntil(projectRoot, quotaProbeTestProvider); blocked {
		t.Fatal("an expired signal without an attempt must not block the first spawn")
	}
	if _, err := ReserveQuotaProbe(projectRoot, quotaProbeTestProvider, "coder"); err != nil {
		t.Fatal(err)
	}
	if CheckQuotaSignal(projectRoot, quotaProbeTestProvider) {
		t.Fatal("a reservation must not stop the agent it reserves for")
	}
	if until, blocked := QuotaBlockedUntil(projectRoot, quotaProbeTestProvider); !blocked || until.IsZero() {
		t.Fatalf("QuotaBlockedUntil under a reservation = (%v, %t), want blocked until its hold", until, blocked)
	}
	if got, err := ReserveQuotaProbe(projectRoot, quotaProbeTestProvider, "reviewer"); err != nil || got.Admission != QuotaRefused || !strings.Contains(got.AttemptHolder, "reservation by spawn:coder") {
		t.Fatalf("second reservation = %+v, %v; want refusal naming the first", got, err)
	}
	admit(t, projectRoot, "coder-1")
	if !CheckQuotaSignal(projectRoot, quotaProbeTestProvider) {
		t.Fatal("a running probe must stop other running agents")
	}
	if err := ClearQuotaSignal(projectRoot, quotaProbeTestProvider); err != nil {
		t.Fatal(err)
	}
	if quotaFileExists(t, QuotaSignalPath(projectRoot, quotaProbeTestProvider)) || quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
		t.Fatal("the manual unblock must remove the signal and the lease")
	}
}

func TestQuotaReservation_HandOverAndRelease(t *testing.T) {
	projectRoot := newExpiredQuotaProject(t)
	if _, err := ReserveQuotaProbe(projectRoot, quotaProbeTestProvider, "coder"); err != nil {
		t.Fatal(err)
	}
	const agentPID = 515151
	if err := HandOverQuotaReservation(projectRoot, quotaProbeTestProvider, "coder", agentPID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(QuotaProbePath(projectRoot, quotaProbeTestProvider))
	if err != nil {
		t.Fatal(err)
	}
	if lease, ok := parseQuotaLease(data); !ok || lease.PID != agentPID {
		t.Fatalf("lease after hand-over = %+v, want pid %d", lease, agentPID)
	}
	if err := ReleaseQuotaReservation(projectRoot, quotaProbeTestProvider, "reviewer", agentPID); err != nil {
		t.Fatal(err)
	}
	if !quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
		t.Fatal("another role's release removed the reservation")
	}
	if err := ReleaseQuotaReservation(projectRoot, quotaProbeTestProvider, "coder", agentPID); err != nil {
		t.Fatal(err)
	}
	if quotaFileExists(t, QuotaProbePath(projectRoot, quotaProbeTestProvider)) {
		t.Fatal("the failed spawn's release kept its reservation")
	}
}

const quotaAdmitHelperEnv = "LIZA_QUOTA_ADMIT_HELPER_ROOT"

// TestQuotaAdmitHelperProcess is the child of the cross-process test: it
// admits once, prints the outcome and holds its lease alive until stdin closes.
func TestQuotaAdmitHelperProcess(t *testing.T) {
	projectRoot := os.Getenv(quotaAdmitHelperEnv)
	if projectRoot == "" {
		t.Skip("helper process for TestAdmitQuotaStart_CrossProcessElectsOneProbe")
	}
	result, err := AdmitQuotaStart(projectRoot, quotaProbeTestProvider, "coder-"+strings.TrimSpace(os.Getenv("LIZA_QUOTA_ADMIT_HELPER_ID")))
	if err != nil {
		os.Stdout.WriteString("error " + err.Error() + "\n")
		return
	}
	os.Stdout.WriteString("admission " + []string{"clear", "probe", "reserved", "refused"}[result.Admission] + "\n")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

// The defect spans independent CLI/watcher processes, so the election is also
// proven across processes, each child staying alive while the outcome counts.
func TestAdmitQuotaStart_CrossProcessElectsOneProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns test-binary children")
	}
	projectRoot := newExpiredQuotaProject(t)
	const children = 6
	outcomes := make(chan string, children)
	var holds []*os.File
	var cmds []*exec.Cmd
	for i := range children {
		cmd := exec.Command(os.Args[0], "-test.run=^TestQuotaAdmitHelperProcess$")
		cmd.Env = append(os.Environ(), quotaAdmitHelperEnv+"="+projectRoot, "LIZA_QUOTA_ADMIT_HELPER_ID="+string(rune('a'+i)))
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
		holds = append(holds, stdin.(*os.File))
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				if line := scanner.Text(); strings.HasPrefix(line, "admission ") || strings.HasPrefix(line, "error ") {
					outcomes <- line
				}
			}
		}()
	}
	t.Cleanup(func() {
		for _, hold := range holds {
			_ = hold.Close()
		}
		for _, cmd := range cmds {
			_ = cmd.Wait()
		}
	})

	counts := map[string]int{}
	for range children {
		select {
		case line := <-outcomes:
			counts[line]++
		case <-time.After(30 * time.Second):
			t.Fatalf("children reported %v before the timeout, want %d outcomes", counts, children)
		}
	}
	if counts["admission probe"] != 1 || counts["admission refused"] != children-1 {
		t.Fatalf("cross-process outcomes = %v, want one probe and %d refusals", counts, children-1)
	}
}

// A raise that cannot take the lifecycle lock must not write the signal
// outside it, where a settling success could delete it; it fails observably.
func TestRaiseQuotaExhaustion_LockUnavailableWritesNothingOutsideIt(t *testing.T) {
	projectRoot := newExpiredQuotaProject(t)
	previousTimeout, previousBudget := quotaLifecycleLockTimeout, quotaRaiseLockBudget
	quotaLifecycleLockTimeout, quotaRaiseLockBudget = 50*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { quotaLifecycleLockTimeout, quotaRaiseLockBudget = previousTimeout, previousBudget })
	timeouts := 0
	var timeoutsMu sync.Mutex
	restoreHook := filelock.SetAcquisitionTimeoutHookForTest(filepath.Join(paths.New(projectRoot).LizaDir(), quotaLifecycleLockPrefix+quotaProbeTestProvider), func() {
		timeoutsMu.Lock()
		timeouts++
		timeoutsMu.Unlock()
	})
	t.Cleanup(restoreHook)
	before, err := os.ReadFile(QuotaSignalPath(projectRoot, quotaProbeTestProvider))
	if err != nil {
		t.Fatal(err)
	}

	// GIVEN another lifecycle step holds the lock for the whole raise
	locked, release := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- withQuotaLifecycleLock(projectRoot, quotaProbeTestProvider, func() error {
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	// WHEN a raise exhausts its lock budget
	raiseErr := RaiseQuotaExhaustionBy(projectRoot, &QuotaExhaustion{Provider: quotaProbeTestProvider, Message: "You've hit your usage limit"}, "coder-b")
	close(release)
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}

	// THEN it tried the lock, wrote no signal outside it, and said so
	timeoutsMu.Lock()
	observed := timeouts
	timeoutsMu.Unlock()
	if observed == 0 {
		t.Fatal("the raise never waited on the lifecycle lock")
	}
	if raiseErr == nil || !strings.Contains(raiseErr.Error(), "quota signal for claude not written") {
		t.Fatalf("raise error = %v, want the unwritten-signal failure", raiseErr)
	}
	after, err := os.ReadFile(QuotaSignalPath(projectRoot, quotaProbeTestProvider))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("the raise rewrote the signal outside the lifecycle lock")
	}
	if alerts := alertsText(t, projectRoot); !strings.Contains(alerts, "PROVIDER QUOTA EXHAUSTED — claude: You've hit your usage limit (quota signal NOT written") {
		t.Fatalf("alerts lack the unwritten-signal exhaustion:\n%s", alerts)
	}
}
