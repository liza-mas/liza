package analysis

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

// PatternResult represents the result of pattern detection
type PatternResult struct {
	Triggered      bool
	Pattern        string
	Severity       string
	Evidence       string
	Response       models.CircuitBreakerResponseType
	Classification models.CircuitBreakerEvidenceClass
	Explanation    string
	// Subject binds a task-scoped pattern to the task and blocked episode it
	// reports; resolving the response releases only that episode.
	Subject *models.CircuitBreakerSubject
}

// DetectPatterns analyzes anomalies and detects circuit breaker patterns.
// Returns the first matching pattern (checked in priority order), or a non-triggered result if none match.
func DetectPatterns(anomalies []models.Anomaly) PatternResult {
	return detectPatterns(anomalies, true)
}

func detectPatterns(anomalies []models.Anomaly, includeProviderAudit bool) PatternResult {
	checks := []func([]models.Anomaly) PatternResult{
		checkRetryCluster,
		checkDebtAccumulation,
		checkAssumptionCascade,
		checkSpecGapCluster,
		checkWorkaroundPattern,
		checkExternalServiceOutage,
	}
	if includeProviderAudit {
		checks = append(checks, checkProviderAuditDegradation)
	}
	for _, check := range checks {
		if result := check(anomalies); result.Pattern != "" {
			if result.Response == "" {
				result.Response = models.CircuitBreakerResponseHalt
			}
			return result
		}
	}
	return PatternResult{Triggered: false}
}

// DetectUnacknowledgedPatterns runs circuit-breaker detection on anomalies and
// planning-task review evidence that have not already been acknowledged by a
// cleared circuit-breaker trigger. Provider-audit evidence is classified
// separately across its latest resolved-response boundary.
//
// Legacy cleared triggers remain generic acknowledgement boundaries. A provider
// response boundary applies only to provider-audit classification and only after
// the active response is resolved.
func DetectUnacknowledgedPatterns(state *models.State) (PatternResult, []models.Anomaly, int) {
	if state == nil {
		return DetectPatterns(nil), nil, 0
	}

	considered, suppressedCount := UnacknowledgedAnomalies(state)
	if result := detectPatterns(considered, false); result.Pattern != "" {
		return result, considered, suppressedCount
	}

	watermark, hasWatermark := latestResolvedResponseWatermark(state)
	if result, evidence := checkProviderAuditEvidence(state, watermark, hasWatermark); result.Pattern != "" {
		return result, evidence, suppressedCount
	}

	result := checkPlanningReviewChurn(state)
	if result.Pattern == "" {
		result = checkBlockedReplacementChain(state)
	}
	if result.Pattern != "" {
		result.Response = models.CircuitBreakerResponseHalt
	}
	return result, considered, suppressedCount
}

// UnacknowledgedAnomalies returns the anomaly slice eligible for generic
// circuit-breaker detection plus the number suppressed by the cleared-trigger
// acknowledgement watermark.
func UnacknowledgedAnomalies(state *models.State) ([]models.Anomaly, int) {
	if state == nil {
		return nil, 0
	}

	watermark, ok := latestClearedTriggerWatermark(state)
	if !ok {
		return state.Anomalies, 0
	}

	considered := make([]models.Anomaly, 0, len(state.Anomalies))
	for _, anomaly := range state.Anomalies {
		if anomaly.Timestamp.After(watermark) {
			considered = append(considered, anomaly)
		}
	}
	return considered, len(state.Anomalies) - len(considered)
}

func latestResolvedResponseWatermark(state *models.State) (time.Time, bool) {
	if state.CircuitBreaker.Status != "OK" || state.CircuitBreaker.CurrentTrigger != nil || state.CircuitBreaker.CurrentResponse != nil {
		return time.Time{}, false
	}

	var latest time.Time
	for _, entry := range state.CircuitBreaker.History {
		legacyTrigger := entry.Result == "TRIGGERED"
		resolvedResponse := entry.ResolvedAt != nil && (entry.Response == models.CircuitBreakerResponseCheckpoint || entry.Response == models.CircuitBreakerResponseHalt)
		if !legacyTrigger && !resolvedResponse {
			continue
		}
		if latest.IsZero() || entry.Timestamp.After(latest) {
			latest = entry.Timestamp
		}
	}
	return latest, !latest.IsZero()
}

func latestClearedTriggerWatermark(state *models.State) (time.Time, bool) {
	if state.CircuitBreaker.Status != "OK" || state.CircuitBreaker.CurrentTrigger != nil {
		return time.Time{}, false
	}

	var latest time.Time
	for _, entry := range state.CircuitBreaker.History {
		if entry.Result != "TRIGGERED" {
			continue
		}
		if latest.IsZero() || entry.Timestamp.After(latest) {
			latest = entry.Timestamp
		}
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	return latest, true
}

func checkPlanningReviewChurn(state *models.State) PatternResult {
	watermark, cleared := latestClearedTriggerWatermark(state)
	for _, task := range state.Tasks {
		if task.Type != models.TaskTypePlanning {
			continue
		}

		historyCount, latestRejection := planningRejectionHistory(task.History)
		rejectionCount := task.ReviewCyclesTotal
		if rejectionCount == 0 {
			rejectionCount = historyCount
		}
		if rejectionCount < 4 || cleared && !latestRejection.After(watermark) {
			continue
		}

		return PatternResult{
			Triggered: true,
			Pattern:   "planning_review_churn",
			Severity:  "PLANNING_CONVERGENCE_DEGRADED",
			Evidence:  fmt.Sprintf("planning task %s with status %s has %d durable rejection cycles", task.ID, task.Status, rejectionCount),
		}
	}
	return PatternResult{Triggered: false}
}

// checkBlockedReplacementChain reports the first BLOCKED task whose lineage is
// at the agent blocked-recovery cap and not yet released (ADR-0171). The cap
// keeps such a task BLOCKED, so the evidence stays current until a human
// resolves this response, which releases exactly the reported episode.
func checkBlockedReplacementChain(state *models.State) PatternResult {
	for i := range state.Tasks {
		task := &state.Tasks[i]
		chain, capped := models.BlockedRecoveryCapped(state, task)
		if !capped {
			continue
		}
		evidence := fmt.Sprintf("task %s is BLOCKED after %d blocked-recovery replacements (lineage %s); blocked_reason: %q",
			task.ID, len(chain.Recoveries), strings.Join(chain.Path, " -> "), truncateEvidence(task.BlockedReason))
		for _, id := range chain.Recoveries {
			record := models.BlockedRecovery(state.FindTask(id))
			reason, _ := record["blocked_reason"].(string)
			changed, _ := record["changed"].(string)
			evidence += fmt.Sprintf("; %s blocked on %q, replaced with change %q", id, truncateEvidence(&reason), truncateEvidence(&changed))
		}
		return PatternResult{
			Triggered: true,
			Pattern:   models.BlockedReplacementChainPattern,
			Severity:  "RECOVERY_CONVERGENCE_DEGRADED",
			Evidence:  evidence,
			Subject:   &models.CircuitBreakerSubject{TaskID: task.ID, BlockedAt: models.BlockedEpisodeAt(task)},
		}
	}
	return PatternResult{Triggered: false}
}

func truncateEvidence(text *string) string {
	const limit = 200
	if text == nil {
		return ""
	}
	if runes := []rune(*text); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return *text
}

func planningRejectionHistory(history []models.TaskHistoryEntry) (int, time.Time) {
	count := 0
	var latest time.Time
	for _, entry := range history {
		if entry.Time.IsZero() || entry.Event != models.TaskEventRejected && entry.Event != models.TaskEventReviewVerdictRejected {
			continue
		}
		count++
		if entry.Time.After(latest) {
			latest = entry.Time
		}
	}
	return count, latest
}

func checkRetryCluster(anomalies []models.Anomaly) PatternResult {
	retryLoops := filterByType(anomalies, "retry_loop")
	if len(retryLoops) < 3 {
		return PatternResult{Triggered: false}
	}

	// Count similar error patterns
	groups := groupByField(retryLoops, "error_pattern")
	for _, group := range groups {
		if len(group) >= 2 {
			// At least 2 anomalies share the same error pattern
			return PatternResult{
				Triggered: true,
				Pattern:   "retry_cluster",
				Severity:  "ARCHITECTURE_FLAW",
				Evidence:  fmt.Sprintf("%d retry_loop anomalies with similar error patterns", len(retryLoops)),
			}
		}
	}

	return PatternResult{Triggered: false}
}

func checkDebtAccumulation(anomalies []models.Anomaly) PatternResult {
	tradeOffs := filterByType(anomalies, "trade_off")

	debtCount := 0
	for _, a := range tradeOffs {
		if debtCreated, ok := a.Details["debt_created"].(bool); ok && debtCreated {
			debtCount++
		}
	}

	if debtCount >= 3 {
		return PatternResult{
			Triggered: true,
			Pattern:   "debt_accumulation",
			Severity:  "SCOPE_FLAW",
			Evidence:  fmt.Sprintf("%d trade-offs creating technical debt", debtCount),
		}
	}

	return PatternResult{Triggered: false}
}

func checkAssumptionCascade(anomalies []models.Anomaly) PatternResult {
	return checkGroupedThreshold(anomalies, "assumption_violated", "assumption", 2, PatternResult{
		Pattern:  "assumption_cascade",
		Severity: "SPEC_FLAW",
		Evidence: "Same assumption violated across multiple tasks",
	})
}

func checkSpecGapCluster(anomalies []models.Anomaly) PatternResult {
	return checkGroupedThreshold(anomalies, "spec_ambiguity", "spec_ref", 2, PatternResult{
		Pattern:  "spec_gap_cluster",
		Severity: "SPEC_FLAW",
		Evidence: "Multiple tasks hitting same spec ambiguity",
	})
}

func checkWorkaroundPattern(anomalies []models.Anomaly) PatternResult {
	var workarounds []models.Anomaly
	for _, a := range anomalies {
		if a.Type == "workaround" || a.Type == "trade_off" {
			workarounds = append(workarounds, a)
		}
	}

	if len(workarounds) < 2 {
		return PatternResult{Triggered: false}
	}

	// Group by root_cause (or "what" field as fallback)
	groups := make(map[string][]models.Anomaly)
	for _, a := range workarounds {
		key := ""
		if rootCause, ok := a.Details["root_cause"].(string); ok {
			key = rootCause
		} else if what, ok := a.Details["what"].(string); ok {
			key = what
		}

		if key != "" {
			groups[key] = append(groups[key], a)
		}
	}

	for _, group := range groups {
		if len(group) >= 2 {
			return PatternResult{
				Triggered: true,
				Pattern:   "workaround_pattern",
				Severity:  "ARCHITECTURE_FLAW",
				Evidence:  fmt.Sprintf("%d workarounds/trade-offs with similar root causes", len(workarounds)),
			}
		}
	}

	return PatternResult{Triggered: false}
}

func checkExternalServiceOutage(anomalies []models.Anomaly) PatternResult {
	externals := filterByType(anomalies, "external_blocker")
	groups := groupByField(externals, "blocker_service")
	for service, group := range groups {
		if len(group) >= 2 {
			return PatternResult{
				Triggered: true,
				Pattern:   "external_service_outage",
				Severity:  "EXTERNAL_DEPENDENCY",
				Evidence:  fmt.Sprintf("Multiple tasks blocked by same external service: %s", service),
			}
		}
	}
	return PatternResult{Triggered: false}
}

func checkProviderAuditDegradation(anomalies []models.Anomaly) PatternResult {
	auditDegraded := filterByType(anomalies, "provider_audit_degraded")
	groups := groupByField(auditDegraded, "provider")
	for provider, group := range groups {
		agentCount, qualifies := providerAuditThreshold(group)
		if qualifies {
			return PatternResult{
				Pattern:        "provider_audit_degradation",
				Severity:       "OBSERVABILITY_DEGRADED",
				Evidence:       fmt.Sprintf("%d provider_audit_degraded anomalies for provider %s across %d agents", len(group), provider, agentCount),
				Response:       models.CircuitBreakerResponseCheckpoint,
				Classification: models.CircuitBreakerEvidenceNew,
				Explanation:    fmt.Sprintf("current health for provider %s is unknown without a registered-agent state snapshot", provider),
			}
		}
	}
	return PatternResult{Triggered: false}
}

func checkProviderAuditEvidence(state *models.State, watermark time.Time, hasWatermark bool) (PatternResult, []models.Anomaly) {
	groups := groupByField(filterByType(state.Anomalies, "provider_audit_degraded"), "provider")
	var selected PatternResult
	var selectedEvidence []models.Anomaly
	selectedProvider := ""
	for provider, group := range groups {
		agentCount, qualifies := providerAuditThreshold(group)
		if !qualifies {
			continue
		}

		hasHistorical := false
		hasCurrent := !hasWatermark
		for _, anomaly := range group {
			if anomaly.Timestamp.After(watermark) {
				hasCurrent = true
			} else {
				hasHistorical = true
			}
		}
		result := PatternResult{
			Pattern:  "provider_audit_degradation",
			Severity: "OBSERVABILITY_DEGRADED",
			Evidence: fmt.Sprintf("%d provider_audit_degraded anomalies for provider %s across %d agents", len(group), provider, agentCount),
		}
		switch {
		case hasWatermark && !hasCurrent:
			result.Classification = models.CircuitBreakerEvidenceAcknowledgedHistorical
			result.Response = models.CircuitBreakerResponseWarning
			result.Explanation = fmt.Sprintf("qualifying provider-audit evidence for %s is entirely at or before the resolved response boundary", provider)
		case hasWatermark && hasHistorical:
			result.Classification = models.CircuitBreakerEvidenceContinuing
			selectProviderAuditAction(state, provider, &result)
		default:
			result.Classification = models.CircuitBreakerEvidenceNew
			selectProviderAuditAction(state, provider, &result)
		}

		priority := providerAuditResponsePriority(result.Response)
		selectedPriority := providerAuditResponsePriority(selected.Response)
		if selected.Pattern == "" || priority > selectedPriority || (priority == selectedPriority && provider < selectedProvider) {
			selected = result
			selectedEvidence = group
			selectedProvider = provider
		}
	}
	return selected, selectedEvidence
}

func providerAuditResponsePriority(response models.CircuitBreakerResponseType) int {
	switch response {
	case models.CircuitBreakerResponseHalt:
		return 3
	case models.CircuitBreakerResponseCheckpoint:
		return 2
	case models.CircuitBreakerResponseWarning:
		return 1
	default:
		return 0
	}
}

func selectProviderAuditAction(state *models.State, provider string, result *PatternResult) {
	explanation, halt := allExactProviderEpochsDegraded(state, provider)
	if halt {
		result.Triggered = true
		result.Response = models.CircuitBreakerResponseHalt
		result.Explanation = explanation
		return
	}
	result.Response = models.CircuitBreakerResponseCheckpoint
	result.Explanation = explanation
}

func allExactProviderEpochsDegraded(state *models.State, provider string) (string, bool) {
	exactMatches := 0
	for agentID, agent := range state.Agents {
		if agent.Provider != provider {
			continue
		}
		exactMatches++
		if agent.PID == 0 || agent.RegisteredAt.IsZero() {
			return fmt.Sprintf("current health for provider %s is unknown: registered agent %s lacks exact PID or registration-time identity", provider, agentID), false
		}
		health, ok := state.AgentHealth[agentID]
		if !ok {
			return fmt.Sprintf("current health for provider %s is unknown: registered agent %s has no health record", provider, agentID), false
		}
		if health.Provider != agent.Provider || health.PID != agent.PID || health.RegisteredAt == nil || !health.RegisteredAt.Equal(agent.RegisteredAt) {
			return fmt.Sprintf("current health for provider %s is unknown: agent %s health does not match its provider, PID, and registration-time epoch", provider, agentID), false
		}
		if !health.IsCurrentDegradedFor(agent) {
			return fmt.Sprintf("current health for provider %s is not degraded for registered agent %s", provider, agentID), false
		}
	}
	if exactMatches == 0 {
		return fmt.Sprintf("current health for provider %s is unknown: no currently registered agent has an exact provider match", provider), false
	}
	return fmt.Sprintf("all %d currently registered agents with exact provider %s matches have degraded health for the same agent ID, provider, PID, and registration-time epoch", exactMatches, provider), true
}

func providerAuditThreshold(group []models.Anomaly) (int, bool) {
	agents := make(map[string]struct{})
	for _, anomaly := range group {
		if agentID, ok := anomaly.Details["agent_id"].(string); ok && agentID != "" {
			agents[agentID] = struct{}{}
		}
	}
	return len(agents), len(agents) >= 2 || len(group) >= 3
}

// checkGroupedThreshold detects patterns where anomalies of a given type share a field value
// at or above a threshold count. The result template is returned with Triggered set to true
// when the threshold is met.
func checkGroupedThreshold(anomalies []models.Anomaly, anomalyType, field string, threshold int, result PatternResult) PatternResult {
	filtered := filterByType(anomalies, anomalyType)
	groups := groupByField(filtered, field)
	for _, group := range groups {
		if len(group) >= threshold {
			result.Triggered = true
			return result
		}
	}
	return PatternResult{Triggered: false}
}

func filterByType(anomalies []models.Anomaly, anomalyType string) []models.Anomaly {
	var result []models.Anomaly
	for _, a := range anomalies {
		if a.Type == anomalyType {
			result = append(result, a)
		}
	}
	return result
}

// groupByField groups anomalies by a field value in their Details
func groupByField(anomalies []models.Anomaly, field string) map[string][]models.Anomaly {
	groups := make(map[string][]models.Anomaly)
	for _, a := range anomalies {
		if value, ok := a.Details[field].(string); ok && value != "" {
			groups[value] = append(groups[value], a)
		}
	}
	return groups
}

// GenerateReport creates a markdown report for a circuit-breaker analysis response.
// The anomalies slice must be the exact anomaly set considered for detection.
func GenerateReport(result PatternResult, anomalies []models.Anomaly, timestamp time.Time, suppressedCount int) string {
	var sb strings.Builder

	sb.WriteString("# Circuit Breaker Report\n\n")
	fmt.Fprintf(&sb, "**Analyzed:** %s\n", timestamp.Format(time.RFC3339))
	fmt.Fprintf(&sb, "**Pattern:** %s\n", result.Pattern)
	fmt.Fprintf(&sb, "**Severity:** %s\n", result.Severity)
	fmt.Fprintf(&sb, "**Evidence class:** %s\n", result.Classification)
	fmt.Fprintf(&sb, "**Response:** %s\n", result.Response)
	fmt.Fprintf(&sb, "**Explanation:** %s\n\n", result.Explanation)
	if suppressedCount > 0 {
		fmt.Fprintf(&sb, "**Acknowledged anomalies suppressed:** %d\n\n", suppressedCount)
	}

	sb.WriteString("## Evidence\n\n")
	sb.WriteString(result.Evidence)
	sb.WriteString("\n\n")

	writeResponseAction(&sb, result.Response)
	writeTrimmedAnomalies(&sb, anomalies)

	sb.WriteString("## Anomalies (raw)\n\n")
	sb.WriteString("```yaml\n")
	yamlData, err := yaml.Marshal(anomalies)
	if err != nil {
		fmt.Fprintf(&sb, "Error marshaling anomalies: %v\n", err)
	} else {
		sb.Write(yamlData)
	}

	sb.WriteString("```\n\n")

	if result.Response != models.CircuitBreakerResponseWarning {
		sb.WriteString("## Human Decision Required\n\n")
		sb.WriteString("- [ ] Acknowledge report\n")
		sb.WriteString("- [ ] Confirm severity assessment\n")
		sb.WriteString("- [ ] Determine remediation\n")
		sb.WriteString("- [ ] Release checkpoint with decision logged\n")
	}

	return sb.String()
}

func writeResponseAction(sb *strings.Builder, response models.CircuitBreakerResponseType) {
	sb.WriteString("## Response Action\n\n")
	switch response {
	case models.CircuitBreakerResponseWarning:
		sb.WriteString("No state action — this evidence was already acknowledged.\n\n")
	case models.CircuitBreakerResponseCheckpoint:
		fmt.Fprintf(sb, "Sprint moved to CHECKPOINT. Review the explanation. Run `%s` to continue.\n\n", brand.Command("resume"))
	case models.CircuitBreakerResponseHalt:
		fmt.Fprintf(sb, "Circuit breaker triggered and execution halted. Review the explanation. Run `%s` after remediation.\n\n", brand.Command("resume"))
	default:
		sb.WriteString("No response action recorded.\n\n")
	}
}

func writeTrimmedAnomalies(sb *strings.Builder, anomalies []models.Anomaly) {
	sb.WriteString("## Anomalies (trimmed)\n\n")
	if len(anomalies) == 0 {
		sb.WriteString("_None_\n\n")
		return
	}

	for i, anomaly := range anomalies {
		task := anomaly.Task
		if task == "" {
			task = "-"
		}
		fmt.Fprintf(sb, "%d. `%s` at `%s`\n", i+1, anomaly.Type, anomaly.Timestamp.Format(time.RFC3339Nano))
		fmt.Fprintf(sb, "   - task: `%s`\n", task)
		fmt.Fprintf(sb, "   - reporter: `%s`\n", anomaly.Reporter)
		writeTrimmedDetail(sb, "provider", anomaly.Details)
		writeTrimmedDetail(sb, "agent_id", anomaly.Details)
		writeTrimmedDetail(sb, "impact", anomaly.Details)
		writeTrimmedMessage(sb, anomaly.Details)
		sb.WriteString("\n")
	}
}

func writeTrimmedDetail(sb *strings.Builder, key string, details map[string]any) {
	value, ok := details[key].(string)
	if !ok || value == "" {
		return
	}
	fmt.Fprintf(sb, "   - %s: `%s`\n", key, compactText(value, 240))
}

func writeTrimmedMessage(sb *strings.Builder, details map[string]any) {
	message, ok := details["message"].(string)
	if !ok || message == "" {
		return
	}

	if writeProviderMessageSummary(sb, message) {
		return
	}
	fmt.Fprintf(sb, "   - message_excerpt: `%s`\n", compactText(message, 500))
}

func writeProviderMessageSummary(sb *strings.Builder, message string) bool {
	var event struct {
		Type string `json:"type"`
		Item struct {
			Type             string `json:"type"`
			Command          string `json:"command"`
			AggregatedOutput string `json:"aggregated_output"`
			ExitCode         *int   `json:"exit_code"`
			Status           string `json:"status"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(message), &event); err != nil {
		return false
	}

	wrote := false
	if event.Type != "" || event.Item.Type != "" {
		fmt.Fprintf(sb, "   - provider_event: `%s / %s`\n", compactText(event.Type, 80), compactText(event.Item.Type, 80))
		wrote = true
	}
	if event.Item.Command != "" {
		fmt.Fprintf(sb, "   - command: `%s`\n", compactText(event.Item.Command, 240))
		wrote = true
	}
	if event.Item.Status != "" || event.Item.ExitCode != nil {
		exitCode := "-"
		if event.Item.ExitCode != nil {
			exitCode = fmt.Sprintf("%d", *event.Item.ExitCode)
		}
		fmt.Fprintf(sb, "   - status: `%s`, exit_code: `%s`\n", compactText(event.Item.Status, 80), exitCode)
		wrote = true
	}
	if event.Item.AggregatedOutput != "" {
		fmt.Fprintf(sb, "   - aggregated_output_chars: `%d`\n", len(event.Item.AggregatedOutput))
		wrote = true
	}
	return wrote
}

func compactText(value string, limit int) string {
	compacted := strings.Join(strings.Fields(value), " ")
	runes := []rune(compacted)
	if len(runes) <= limit {
		return strings.ReplaceAll(compacted, "`", "'")
	}
	return strings.ReplaceAll(string(runes[:limit])+"...", "`", "'")
}
