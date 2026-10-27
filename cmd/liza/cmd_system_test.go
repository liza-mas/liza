package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func TestTuiCmd_HeadlessFlag(t *testing.T) {
	flag := tuiCmd.Flags().Lookup("headless")
	if flag == nil {
		t.Fatal("tuiCmd missing --headless flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--headless default = %q, want %q", flag.DefValue, "false")
	}
	if flag.Usage == "" {
		t.Error("--headless flag has no usage text")
	}
}

func TestTuiCmd_IntervalFlag(t *testing.T) {
	flag := tuiCmd.Flags().Lookup("interval")
	if flag == nil {
		t.Fatal("tuiCmd missing --interval flag (needed for headless backward compatibility)")
	}
}

func TestTuiCmd_ShortDescription(t *testing.T) {
	want := "Interactive TUI dashboard for monitoring " + brand.NameTitle
	if tuiCmd.Short != want {
		t.Errorf("tuiCmd.Short = %q, want %q", tuiCmd.Short, want)
	}
}

func TestTuiCmd_FallsBackToHeadlessWithoutTTY(t *testing.T) {
	// Replace stdin with a pipe to simulate non-interactive (CI/cron).
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		r.Close()
		w.Close()
	})

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldDir) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resetRootCmdForTest(t)

	var stderr bytes.Buffer
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"tui"})

	err = rootCmd.Execute()

	// The command will error downstream (no git repo for project root),
	// but it must NOT fail with a TTY-related error.
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "tty") {
		t.Fatalf("got TTY error despite auto-fallback: %v", err)
	}

	// Verify the fallback notice was emitted to stderr.
	if !strings.Contains(stderr.String(), "falling back to headless mode") {
		t.Errorf("stderr = %q, want fallback notice", stderr.String())
	}
}

func TestTuiCmd_ExplicitHeadlessSkipsFallback(t *testing.T) {
	// Replace stdin with a pipe to simulate non-interactive,
	// but --headless is already set so no fallback message expected.
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		r.Close()
		w.Close()
	})

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldDir) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resetRootCmdForTest(t)

	var stderr bytes.Buffer
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"tui", "--headless"})

	_ = rootCmd.Execute()

	if strings.Contains(stderr.String(), "falling back to headless mode") {
		t.Errorf("fallback message should not appear when --headless is explicit, stderr = %q", stderr.String())
	}
}

func TestAnalyzeHelpListsProviderAuditDegradation(t *testing.T) {
	helpText := analyzeCmd.Long

	expected := []string{
		"provider_audit_degradation",
		"2+ agents or 3+ hits for same provider",
		"OBSERVABILITY_DEGRADED",
		"WARNING",
		"CHECKPOINT",
		"HALT",
		brand.Command("resume"),
		"If no patterns are detected and no active HALT exists:",
	}
	for _, want := range expected {
		if !strings.Contains(helpText, want) {
			t.Fatalf("analyze help missing %q\nHelp:\n%s", want, helpText)
		}
	}

	for _, line := range strings.Split(helpText, "\n") {
		if strings.Contains(strings.ToLower(line), "trigger") && !strings.Contains(line, "HALT") {
			t.Errorf("analyze help uses trigger terminology outside HALT: %q", line)
		}
	}
	if strings.Contains(helpText, "If no patterns are detected:\n  - Updates circuit_breaker.status to OK\n  - Continues normal operation") {
		t.Error("analyze help promises unconditional OK continuation despite an active HALT")
	}
}

func TestStartResumeHelpExplainsStoppedActiveHaltFlow(t *testing.T) {
	for name, helpText := range map[string]string{
		"start":  startCmd.Long,
		"resume": resumeCmd.Long,
	} {
		t.Run(name, func(t *testing.T) {
			for _, want := range []string{"HALT", brand.Command("resume"), "STOPPED"} {
				if !strings.Contains(helpText, want) {
					t.Errorf("%s help missing %q:\n%s", name, want, helpText)
				}
			}
		})
	}
	if !strings.Contains(resumeCmd.Long, brand.Command("start")) {
		t.Errorf("resume help does not direct the operator to start after HALT acknowledgement:\n%s", resumeCmd.Long)
	}
}

func TestProviderAuditDocumentationContract(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "specs", "protocols", "circuit-breaker.md"))
	if err != nil {
		t.Fatalf("read circuit-breaker protocol: %v", err)
	}
	protocol := string(content)
	section := func(t *testing.T, start, end string) string {
		t.Helper()
		startAt := strings.Index(protocol, start)
		if startAt < 0 {
			t.Fatalf("missing section start %q", start)
		}
		endAt := strings.Index(protocol[startAt+len(start):], end)
		if endAt < 0 {
			t.Fatalf("missing section end %q after %q", end, start)
		}
		return protocol[startAt : startAt+len(start)+endAt]
	}

	report := strings.Join(strings.Fields(section(t, "## Circuit Breaker Report Format", "## Implementation: v1 vs v2")), " ")
	for _, want := range []string{
		"**Evidence Class:** `CONTINUING`",
		"**Response:** `CHECKPOINT`",
		"**Explanation:**",
		"## Response Action",
		"`WARNING` is observation-only",
		"`CHECKPOINT` is a non-trigger hard checkpoint",
		"`HALT` is the only circuit-breaker trigger",
		"## Anomalies (trimmed)",
		"## Anomalies (raw)",
		"type: provider_audit_degraded",
		"message: provider transcript persistence failed",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report contract missing %q", want)
		}
	}
	for _, forbidden := range []string{"**Triggered:**", "## Trigger Evidence", "## Recommended Actions"} {
		if strings.Contains(report, forbidden) {
			t.Errorf("report contract retains trigger-only wording %q", forbidden)
		}
	}

	version := strings.Join(strings.Fields(section(t, "## Implementation: v1 vs v2", "## Blackboard Circuit Breaker Section")), " ")
	for _, want := range []string{
		"Only `HALT` trips mode",
		"`CHECKPOINT` is a non-trigger checkpoint",
		"`WARNING` is observation-only",
	} {
		if !strings.Contains(version, want) {
			t.Errorf("v2 contract missing %q", want)
		}
	}
	if strings.Contains(version, "Auto-trips mode and auto-creates sprint CHECKPOINT if pattern matches") {
		t.Error("v2 contract still claims every matched pattern auto-trips mode")
	}

	resolution := strings.Join(strings.Fields(section(t, "### Resolution Workflow", "## Related Documents")), " ")
	for _, want := range []string{
		"| Active response | Committed candidate | Result |",
		"An active `HALT` of any pattern is latched",
		"wins over no match and every matched candidate",
		"An active provider-audit `HALT` is latched",
		"no match or any non-`HALT` candidate",
		"committed candidate is `HALT`",
		"exact current degraded-epoch proof",
		"generic `HALT` candidate",
		"existing pattern priority",
		"`superseded_by_response: HALT`",
		"`resolution` and `resolved_at` absent",
		"exactly one matching unresolved `HALT`",
		"not an acknowledgement",
		"provider-evidence watermark",
		"`AnalyzeResult`",
		"report path",
		"`§BRAND_BINARY_NAME§ resume` is the sole release and acknowledgement action",
	} {
		if !strings.Contains(resolution, want) {
			t.Errorf("resolution contract missing %q", want)
		}
	}
	if strings.Contains(resolution, "does not add a generic `CurrentResponse` guard") {
		t.Error("resolution contract still excludes a generic active-HALT guard")
	}

	for _, want := range []string{
		"`§BRAND_BINARY_NAME§ analyze`",
		"`§BRAND_BINARY_NAME§ tui`",
		"`§BRAND_BINARY_NAME§ resume`",
	} {
		if !strings.Contains(protocol, want) {
			t.Errorf("protocol missing white-label command %q", want)
		}
	}
}

func TestAnalyzeJSONProjectsTypedResponse(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		state.Anomalies = []models.Anomaly{
			{
				Timestamp: now,
				Reporter:  "coder-1",
				Type:      "provider_audit_degraded",
				Details: map[string]any{
					"provider": "codex",
					"agent_id": "coder-1",
					"message":  "rollout persistence failed",
				},
			},
			{
				Timestamp: now.Add(time.Minute),
				Reporter:  "coder-2",
				Type:      "provider_audit_degraded",
				Details: map[string]any{
					"provider": "codex",
					"agent_id": "coder-2",
					"message":  "transcript persistence failed",
				},
			},
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot, "analyze", "--json")
	if err != nil {
		t.Fatalf("analyze --json failed: %v\n%s", err, stdout)
	}
	envelope := parseEnvelope(t, stdout)
	result, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %T, want object", envelope["result"])
	}
	if result["response"] != string(models.CircuitBreakerResponseCheckpoint) {
		t.Errorf("response = %v, want CHECKPOINT", result["response"])
	}
	if result["classification"] != string(models.CircuitBreakerEvidenceNew) {
		t.Errorf("classification = %v, want NEW", result["classification"])
	}
	explanation, _ := result["explanation"].(string)
	if !strings.Contains(explanation, "unknown") || !strings.Contains(explanation, "exact provider match") {
		t.Errorf("explanation = %q, want unknown exact-provider health proof", explanation)
	}
	if result["triggered"] != false {
		t.Errorf("triggered = %v, want false for CHECKPOINT", result["triggered"])
	}
}

func TestAnalyzeProviderAuditLifecycle(t *testing.T) {
	providerAuditAnomaly := func(timestamp time.Time, agentID string) models.Anomaly {
		return models.Anomaly{
			Timestamp: timestamp,
			Reporter:  agentID,
			Type:      "provider_audit_degraded",
			Details: map[string]any{
				"provider": "codex",
				"agent_id": agentID,
				"message":  "provider audit persistence failed",
			},
		}
	}
	runAnalyze := func(t *testing.T, projectRoot string, classification models.CircuitBreakerEvidenceClass, response models.CircuitBreakerResponseType, triggered bool, explanationParts ...string) string {
		t.Helper()

		stdout, err := executeRootCommandCapture(t, projectRoot, "analyze", "--json")
		if err != nil {
			t.Fatalf("analyze --json failed: %v\n%s", err, stdout)
		}
		envelope := parseEnvelope(t, stdout)
		if envelope["ok"] != true {
			t.Fatalf("analyze envelope ok = %v, want true", envelope["ok"])
		}
		result, ok := envelope["result"].(map[string]any)
		if !ok {
			t.Fatalf("result = %T, want object", envelope["result"])
		}
		for field, want := range map[string]any{
			"pattern":        "provider_audit_degradation",
			"severity":       "OBSERVABILITY_DEGRADED",
			"classification": string(classification),
			"response":       string(response),
			"triggered":      triggered,
		} {
			if result[field] != want {
				t.Errorf("%s = %v, want %v", field, result[field], want)
			}
		}
		explanation, ok := result["explanation"].(string)
		if !ok || explanation == "" {
			t.Fatalf("explanation = %v, want non-empty string", result["explanation"])
		}
		for _, want := range explanationParts {
			if !strings.Contains(explanation, want) {
				t.Errorf("explanation = %q, want %q", explanation, want)
			}
		}

		reportPath, ok := result["report_path"].(string)
		if !ok || reportPath == "" {
			t.Fatalf("report_path = %v, want non-empty string", result["report_path"])
		}
		reportBytes, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatalf("read analyze report: %v", err)
		}
		report := string(reportBytes)
		for _, want := range []string{
			"**Pattern:** provider_audit_degradation",
			"**Severity:** OBSERVABILITY_DEGRADED",
			"**Evidence class:** " + string(classification),
			"**Response:** " + string(response),
			"**Explanation:** " + explanation,
		} {
			if !strings.Contains(report, want) {
				t.Errorf("report missing %q\n%s", want, report)
			}
		}
		return report
	}
	setProviderEpochs := func(state *models.State, provider string, registeredAt time.Time) {
		state.Agents = map[string]models.Agent{
			"coder-1": {Provider: provider, PID: 101, RegisteredAt: registeredAt},
			"coder-2": {Provider: provider, PID: 102, RegisteredAt: registeredAt},
		}
		state.AgentHealth = map[string]models.AgentHealth{
			"coder-1": {State: models.AgentHealthDegraded, Provider: provider, PID: 101, RegisteredAt: &registeredAt},
			"coder-2": {State: models.AgentHealthDegraded, Provider: provider, PID: 102, RegisteredAt: &registeredAt},
		}
	}
	assertRunningWithoutTrigger := func(t *testing.T, state *models.State, sprintStatus models.SprintStatus) {
		t.Helper()
		if state.Config.Mode != models.SystemModeRunning {
			t.Errorf("mode = %s, want RUNNING", state.Config.Mode)
		}
		if state.Sprint.Status != sprintStatus {
			t.Errorf("sprint status = %s, want %s", state.Sprint.Status, sprintStatus)
		}
		if state.CircuitBreaker.Status != "OK" {
			t.Errorf("circuit-breaker status = %s, want OK", state.CircuitBreaker.Status)
		}
		if state.CircuitBreaker.CurrentTrigger != nil {
			t.Errorf("current trigger = %#v, want nil", state.CircuitBreaker.CurrentTrigger)
		}
	}

	t.Run("acknowledged continuing and resumed evidence", func(t *testing.T) {
		base := time.Now().UTC().Add(-time.Hour)
		boundary := base.Add(10 * time.Minute)
		resolvedAt := boundary.Add(time.Second)
		pattern := "provider_audit_degradation"
		severity := "OBSERVABILITY_DEGRADED"
		resolution := "resumed by operator-0"
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			state.Anomalies = []models.Anomaly{
				providerAuditAnomaly(base, "coder-1"),
				providerAuditAnomaly(base.Add(time.Minute), "coder-2"),
			}
			state.CircuitBreaker.History = []models.CircuitBreakerHistory{
				{
					Timestamp:      boundary,
					Pattern:        &pattern,
					Severity:       &severity,
					Result:         string(models.CircuitBreakerResponseCheckpoint),
					Response:       models.CircuitBreakerResponseCheckpoint,
					Classification: models.CircuitBreakerEvidenceNew,
					Explanation:    "initial provider-audit response",
					Resolution:     &resolution,
					ResolvedAt:     &resolvedAt,
				},
			}
		})

		warningReport := runAnalyze(t, projectRoot,
			models.CircuitBreakerEvidenceAcknowledgedHistorical,
			models.CircuitBreakerResponseWarning,
			false,
			"entirely at or before the resolved response boundary",
		)
		if !strings.Contains(warningReport, "No state action — this evidence was already acknowledged.") {
			t.Errorf("warning report missing no-state-action text\n%s", warningReport)
		}
		state := readState(t, statePath)
		assertRunningWithoutTrigger(t, state, models.SprintStatusInProgress)
		if state.CircuitBreaker.CurrentResponse != nil {
			t.Errorf("current response = %#v, want nil for WARNING", state.CircuitBreaker.CurrentResponse)
		}
		if len(state.CircuitBreaker.History) != 2 {
			t.Fatalf("history length = %d, want 2", len(state.CircuitBreaker.History))
		}
		if state.CircuitBreaker.History[0].ResolvedAt == nil || !state.CircuitBreaker.History[0].Timestamp.Equal(boundary) {
			t.Errorf("resolved history boundary = %#v, want timestamp %s with resolution", state.CircuitBreaker.History[0], boundary)
		}
		warningHistory := state.CircuitBreaker.History[1]
		if warningHistory.Response != models.CircuitBreakerResponseWarning || warningHistory.Classification != models.CircuitBreakerEvidenceAcknowledgedHistorical {
			t.Errorf("warning history = {%s %s}, want {WARNING ACKNOWLEDGED_HISTORICAL}", warningHistory.Response, warningHistory.Classification)
		}

		if err := db.For(statePath).Modify(func(state *models.State) error {
			state.Anomalies = append(state.Anomalies, providerAuditAnomaly(boundary.Add(time.Minute), "coder-3"))
			return nil
		}); err != nil {
			t.Fatalf("append continuing provider-audit evidence: %v", err)
		}
		checkpointReport := runAnalyze(t, projectRoot,
			models.CircuitBreakerEvidenceContinuing,
			models.CircuitBreakerResponseCheckpoint,
			false,
			"unknown",
			"exact provider match",
		)
		if !strings.Contains(checkpointReport, "Sprint moved to CHECKPOINT") || !strings.Contains(checkpointReport, "resume") {
			t.Errorf("checkpoint report missing action or recovery\n%s", checkpointReport)
		}
		state = readState(t, statePath)
		assertRunningWithoutTrigger(t, state, models.SprintStatusCheckpoint)
		response := state.CircuitBreaker.CurrentResponse
		if response == nil {
			t.Fatal("current response = nil, want active CHECKPOINT")
		}
		if response.Response != models.CircuitBreakerResponseCheckpoint || response.Classification != models.CircuitBreakerEvidenceContinuing {
			t.Errorf("current response = {%s %s}, want {CHECKPOINT CONTINUING}", response.Response, response.Classification)
		}
		if response.Explanation == "" || response.ReportFile == "" {
			t.Errorf("current response missing explanation or report path: %#v", response)
		}
		checkpointHistory := state.CircuitBreaker.History[len(state.CircuitBreaker.History)-1]
		if checkpointHistory.ResolvedAt != nil || !checkpointHistory.Timestamp.Equal(response.Timestamp) {
			t.Errorf("active response/history boundary mismatch: response=%#v history=%#v", response, checkpointHistory)
		}

		resumeOutput, err := executeRootCommandCapture(t, projectRoot, "resume", "--changed-by", "operator-1")
		if err != nil {
			t.Fatalf("resume failed: %v\n%s", err, resumeOutput)
		}
		if !strings.Contains(resumeOutput, "System resumed") || !strings.Contains(resumeOutput, "operator-1") {
			t.Errorf("resume output missing lifecycle evidence: %q", resumeOutput)
		}
		state = readState(t, statePath)
		assertRunningWithoutTrigger(t, state, models.SprintStatusInProgress)
		if state.CircuitBreaker.CurrentResponse != nil {
			t.Errorf("current response = %#v, want nil after resume", state.CircuitBreaker.CurrentResponse)
		}
		resolvedCheckpoint := state.CircuitBreaker.History[len(state.CircuitBreaker.History)-1]
		if resolvedCheckpoint.ResolvedAt == nil || resolvedCheckpoint.Resolution == nil || !strings.Contains(*resolvedCheckpoint.Resolution, "operator-1") {
			t.Errorf("checkpoint history was not resolved by resume: %#v", resolvedCheckpoint)
		}
		checkpointBoundary := resolvedCheckpoint.Timestamp

		unchangedReport := runAnalyze(t, projectRoot,
			models.CircuitBreakerEvidenceAcknowledgedHistorical,
			models.CircuitBreakerResponseWarning,
			false,
			"entirely at or before the resolved response boundary",
		)
		if !strings.Contains(unchangedReport, "No state action — this evidence was already acknowledged.") {
			t.Errorf("unchanged-evidence report missing warning action\n%s", unchangedReport)
		}
		state = readState(t, statePath)
		assertRunningWithoutTrigger(t, state, models.SprintStatusInProgress)
		if state.CircuitBreaker.CurrentResponse != nil {
			t.Errorf("current response = %#v, want nil for unchanged WARNING", state.CircuitBreaker.CurrentResponse)
		}
		if len(state.CircuitBreaker.History) < 2 {
			t.Fatalf("history length = %d, want resolved checkpoint plus warning", len(state.CircuitBreaker.History))
		}
		preservedBoundary := state.CircuitBreaker.History[len(state.CircuitBreaker.History)-2]
		if preservedBoundary.ResolvedAt == nil || !preservedBoundary.Timestamp.Equal(checkpointBoundary) {
			t.Errorf("latest resolved evidence boundary = %#v, want timestamp %s", preservedBoundary, checkpointBoundary)
		}
	})

	// C2: a codex-acp registration belongs to the codex provider group, so its own
	// degraded epoch halts (see "exact degraded provider epochs halt"). Health
	// recorded under another tool name never validates its epoch and stays unknown.
	t.Run("canonical evidence with alias registration and another tool's health checkpoints", func(t *testing.T) {
		base := time.Now().UTC().Add(-time.Hour)
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			state.Anomalies = []models.Anomaly{
				providerAuditAnomaly(base, "coder-1"),
				providerAuditAnomaly(base.Add(time.Minute), "coder-2"),
			}
			setProviderEpochs(state, "codex-acp", base.Add(-time.Minute))
			for id, health := range state.AgentHealth {
				health.Provider = "codex"
				state.AgentHealth[id] = health
			}
		})

		checkpointReport := runAnalyze(t, projectRoot,
			models.CircuitBreakerEvidenceNew,
			models.CircuitBreakerResponseCheckpoint,
			false,
			"unknown",
			"does not match its provider, PID, and registration-time epoch",
		)
		if !strings.Contains(checkpointReport, "Sprint moved to CHECKPOINT") {
			t.Errorf("alias report missing checkpoint action\n%s", checkpointReport)
		}
		state := readState(t, statePath)
		assertRunningWithoutTrigger(t, state, models.SprintStatusCheckpoint)
		response := state.CircuitBreaker.CurrentResponse
		if response == nil || response.Response != models.CircuitBreakerResponseCheckpoint {
			t.Errorf("current response = %#v, want CHECKPOINT", response)
		} else if response.Classification != models.CircuitBreakerEvidenceNew || response.Explanation == "" || response.ReportFile == "" {
			t.Errorf("alias current response missing persisted lifecycle fields: %#v", response)
		}
		if len(state.CircuitBreaker.History) == 0 {
			t.Fatal("alias response history is empty")
		}
		history := state.CircuitBreaker.History[len(state.CircuitBreaker.History)-1]
		if history.Response != models.CircuitBreakerResponseCheckpoint || history.Classification != models.CircuitBreakerEvidenceNew || history.ResolvedAt != nil {
			t.Errorf("alias response history = %#v, want active NEW/CHECKPOINT boundary", history)
		}
		if response != nil && !history.Timestamp.Equal(response.Timestamp) {
			t.Errorf("alias response/history boundary mismatch: response=%#v history=%#v", response, history)
		}
	})

	t.Run("exact degraded provider epochs halt", func(t *testing.T) {
		base := time.Now().UTC().Add(-time.Hour)
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			state.Anomalies = []models.Anomaly{
				providerAuditAnomaly(base, "coder-1"),
				providerAuditAnomaly(base.Add(time.Minute), "coder-2"),
			}
			setProviderEpochs(state, "codex", base.Add(-time.Minute))
		})

		haltReport := runAnalyze(t, projectRoot,
			models.CircuitBreakerEvidenceNew,
			models.CircuitBreakerResponseHalt,
			true,
			"all 2 currently registered agents",
			"agent ID, provider, PID, and registration-time epoch",
		)
		if !strings.Contains(haltReport, "Circuit breaker triggered and execution halted") || !strings.Contains(haltReport, "resume") {
			t.Errorf("halt report missing action or recovery\n%s", haltReport)
		}
		state := readState(t, statePath)
		if state.Config.Mode != models.SystemModeCircuitBreakerTripped {
			t.Errorf("mode = %s, want CIRCUIT_BREAKER_TRIPPED", state.Config.Mode)
		}
		if state.Sprint.Status != models.SprintStatusInProgress {
			t.Errorf("sprint status = %s, want IN_PROGRESS", state.Sprint.Status)
		}
		if state.CircuitBreaker.Status != "TRIGGERED" {
			t.Errorf("circuit-breaker status = %s, want TRIGGERED", state.CircuitBreaker.Status)
		}
		trigger := state.CircuitBreaker.CurrentTrigger
		if trigger == nil || trigger.Pattern != "provider_audit_degradation" {
			t.Errorf("current trigger = %#v, want provider_audit_degradation", trigger)
		}
		response := state.CircuitBreaker.CurrentResponse
		if response == nil || response.Response != models.CircuitBreakerResponseHalt {
			t.Errorf("current response = %#v, want HALT", response)
		} else if response.Classification != models.CircuitBreakerEvidenceNew || response.Explanation == "" || response.ReportFile == "" {
			t.Errorf("halt current response missing persisted lifecycle fields: %#v", response)
		}
		if len(state.CircuitBreaker.History) == 0 {
			t.Fatal("halt response history is empty")
		}
		history := state.CircuitBreaker.History[len(state.CircuitBreaker.History)-1]
		if history.Result != "TRIGGERED" || history.Response != models.CircuitBreakerResponseHalt || history.Classification != models.CircuitBreakerEvidenceNew || history.ResolvedAt != nil {
			t.Errorf("halt response history = %#v, want active NEW/HALT boundary", history)
		}
		if response != nil && !history.Timestamp.Equal(response.Timestamp) {
			t.Errorf("halt response/history boundary mismatch: response=%#v history=%#v", response, history)
		}
		if trigger != nil && !history.Timestamp.Equal(trigger.Timestamp) {
			t.Errorf("halt trigger/history boundary mismatch: trigger=%#v history=%#v", trigger, history)
		}
	})
}

func TestPlanningReviewChurnDocumentationContract(t *testing.T) {
	readRepoFile := func(t *testing.T, path string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}
	section := func(t *testing.T, content, start, end string) string {
		t.Helper()
		startAt := strings.Index(content, start)
		if startAt < 0 {
			t.Fatalf("missing section start %q", start)
		}
		endAt := strings.Index(content[startAt+len(start):], end)
		if endAt < 0 {
			t.Fatalf("missing section end %q after %q", end, start)
		}
		return content[startAt : startAt+len(start)+endAt]
	}

	protocol := readRepoFile(t, "specs/protocols/circuit-breaker.md")
	buildSpec := readRepoFile(t, "specs/build/1.5 - Circuit Breaker.md")
	hardeningInventory := readRepoFile(t, "docs/liza-hardened-mas.md")
	architecture := readRepoFile(t, "specs/architecture/architectural-issues.md")
	architectureEntry := section(t, architecture,
		"### Circuit Breaker Depends on Participant Reporting",
		"### No Source Type for Pre-Implementation Spec Findings")

	surfaces := []struct {
		name    string
		content string
	}{
		{name: "analyze help", content: analyzeCmd.Long},
		{name: "protocol", content: protocol},
		{name: "functional spec", content: readRepoFile(t, "specs/functional/1.5 - Circuit Breaker.md")},
		{name: "build spec", content: buildSpec},
		{name: "operator recipes", content: readRepoFile(t, "docs/RECIPES.md")},
		{name: "hardening inventory", content: hardeningInventory},
		{name: "architecture blind spot", content: architectureEntry},
	}
	sharedRequirements := []string{
		"anomalies",
		"planning task review evidence",
		"four or more",
		"`MERGED` tasks remain eligible",
		"planning_review_churn",
		"PLANNING_CONVERGENCE_DEGRADED",
	}
	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			for _, want := range sharedRequirements {
				if !strings.Contains(surface.content, want) {
					t.Errorf("missing %q", want)
				}
			}
		})
	}

	identity := section(t, protocol,
		"## Identity and Constraints",
		"## Input: Anomalies and Planning Task Review Evidence")
	for _, want := range []string{"permissions:", "read:", "anomalies", "planning task review evidence"} {
		if !strings.Contains(identity, want) {
			t.Errorf("Identity and Constraints missing %q", want)
		}
	}
	input := section(t, protocol,
		"## Input: Anomalies and Planning Task Review Evidence",
		"## Anomaly Types")
	for _, want := range []string{"anomalies", "planning task review evidence"} {
		if !strings.Contains(input, want) {
			t.Errorf("input section missing %q", want)
		}
	}
	patterns := section(t, protocol, "## Pattern Detection Rules", "## Severity Classification")
	for _, want := range []string{
		"positive `review_cycles_total`",
		"when `review_cycles_total` is zero",
		"`rejected` and `review_verdict_rejected`",
	} {
		if !strings.Contains(patterns, want) {
			t.Errorf("Pattern Detection Rules missing %q", want)
		}
	}
	watermark := section(t, protocol, "### Acknowledgement Watermark", "## Severity Classification")
	for _, want := range []string{"planning task review evidence", "strictly after the watermark"} {
		if !strings.Contains(watermark, want) {
			t.Errorf("Acknowledgement Watermark missing %q", want)
		}
	}
	severity := section(t, protocol, "## Severity Classification", "## Circuit Breaker Activation")
	for _, want := range []string{
		"`PLANNING_CONVERGENCE_DEGRADED`",
		"code-planning convergence",
		"pause downstream fan-out and inspect rejection evidence before choosing remediation",
	} {
		if !strings.Contains(severity, want) {
			t.Errorf("Severity Classification missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"Input: Anomalies Section",
		"reads the anomalies section",
		"detection normally evaluates durable `state.anomalies`",
	} {
		if strings.Contains(protocol, forbidden) {
			t.Errorf("protocol retains anomaly-only text %q", forbidden)
		}
	}

	for _, forbidden := range []string{"from anomaly signals", "evaluates anomalies", "anomaly patterns"} {
		if strings.Contains(buildSpec, forbidden) {
			t.Errorf("build spec retains anomaly-only text %q", forbidden)
		}
	}
	if strings.Contains(hardeningInventory, "Pattern detection on anomalies") {
		t.Error("hardening inventory retains anomaly-only pattern-detection claim")
	}
	if !strings.Contains(hardeningInventory,
		"| planning_review_churn | four or more planning rejection cycles; `MERGED` tasks remain eligible | PLANNING_CONVERGENCE_DEGRADED |") {
		t.Error("hardening inventory missing the planning_review_churn pattern row")
	}
	for _, want := range []string{"partial independent mitigation", "broader participant-reporting blind spot remains open"} {
		if !strings.Contains(architectureEntry, want) {
			t.Errorf("architecture entry missing %q", want)
		}
	}
}
