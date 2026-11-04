package agent

import (
	"encoding/json"
	"sort"
	"strings"
)

// usageSourceStreamResult marks counts read from a CLI's own JSON output stream.
const usageSourceStreamResult = "stream_result"

// The JSON events that carry a CLI run's usage, each read only for the backend
// whose shape was verified. Other stream-json backends (cursor, gemini, kimi,
// qwen) print look-alike events with unverified input semantics, so they keep
// recording unknown provenance rather than counts that may include the cache.
const (
	claudeBackend      = "claude"
	codexBackend       = "codex"
	openCodeBackend    = "opencode"
	claudeResultEvent  = "result"         // claude -p --output-format stream-json
	codexTurnCompleted = "turn.completed" // codex exec --json
	openCodeStepFinish = "step_finish"    // opencode run --format json
)

// parseCLIUsage reads the usage a CLI reported in its JSON output. ok is false
// when no usage event arrived — other backends, plain-text output, --no-log
// runs, crashes — so the caller keeps emitting the empty usage that records
// unknown provenance.
func parseCLIUsage(backend, stdout string) (LLMAgentUsage, bool) {
	switch backend {
	case claudeBackend:
		return parseClaudeStream(stdout)
	case codexBackend:
		return parseCodexStream(stdout)
	case openCodeBackend:
		return parseOpenCodeStream(stdout)
	}
	return LLMAgentUsage{}, false
}

// eventLines returns the JSON lines of one event type. The quoted-type check
// skips large tool-output lines without decoding them.
func eventLines(stdout, eventType string) []string {
	quoted := `"` + eventType + `"`
	var lines []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, quoted) {
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &head) == nil && head.Type == eventType {
			lines = append(lines, line)
		}
	}
	return lines
}

type claudeModelUsage struct {
	InputTokens         int      `json:"inputTokens"`
	OutputTokens        int      `json:"outputTokens"`
	CacheReadTokens     int      `json:"cacheReadInputTokens"`
	CacheCreationTokens int      `json:"cacheCreationInputTokens"`
	CacheCreation1h     int      `json:"cacheCreationEphemeral1hInputTokens"`
	ThinkingTokens      int      `json:"thinkingTokens"`
	CostUSD             *float64 `json:"costUSD"`
	CostBasis           string   `json:"costBasis"`
}

type claudeResultLine struct {
	SessionID     string   `json:"session_id"`
	TotalCostUSD  *float64 `json:"total_cost_usd"`
	DurationAPIMs int64    `json:"duration_api_ms"`
	TTFTMs        int64    `json:"ttft_ms"`
	NumTurns      int      `json:"num_turns"`
	Usage         *struct {
		InputTokens         int    `json:"input_tokens"`
		OutputTokens        int    `json:"output_tokens"`
		CacheReadTokens     int    `json:"cache_read_input_tokens"`
		CacheCreationTokens int    `json:"cache_creation_input_tokens"`
		ServiceTier         string `json:"service_tier"`
		CacheCreation       struct {
			Ephemeral1h int `json:"ephemeral_1h_input_tokens"`
		} `json:"cache_creation"`
		OutputDetails struct {
			ThinkingTokens int `json:"thinking_tokens"`
		} `json:"output_tokens_details"`
		ServerToolUse struct {
			WebSearchRequests int `json:"web_search_requests"`
			WebFetchRequests  int `json:"web_fetch_requests"`
		} `json:"server_tool_use"`
	} `json:"usage"`
	ModelUsage map[string]claudeModelUsage `json:"modelUsage"`
}

// parseClaudeStream folds every result event of a -p run: a run that wakes for
// background work emits one per process turn. modelUsage, total_cost_usd and
// duration_api_ms are running totals, so the last wins; num_turns and the
// top-level usage cover their own turn, so they are summed.
func parseClaudeStream(stdout string) (LLMAgentUsage, bool) {
	var usage LLMAgentUsage
	var models map[string]claudeModelUsage
	saw := false
	for _, line := range eventLines(stdout, claudeResultEvent) {
		var event claudeResultLine
		if json.Unmarshal([]byte(line), &event) != nil || (event.Usage == nil && len(event.ModelUsage) == 0) {
			continue
		}
		if !saw {
			usage.TTFTMs = event.TTFTMs
		}
		saw = true
		usage.ProviderSessionID, usage.APIDurationMs = event.SessionID, event.DurationAPIMs
		usage.APICalls += event.NumTurns
		if event.TotalCostUSD != nil {
			usage.CostUSD, usage.CostReported = *event.TotalCostUSD, true
		}
		if u := event.Usage; u != nil {
			usage.InputTokens += u.InputTokens
			usage.OutputTokens += u.OutputTokens
			usage.CachedReadTokens += u.CacheReadTokens
			usage.CachedWriteTokens += u.CacheCreationTokens
			usage.CachedWrite1hTokens += u.CacheCreation.Ephemeral1h
			usage.ReasoningTokens += u.OutputDetails.ThinkingTokens
			usage.WebSearchRequests += u.ServerToolUse.WebSearchRequests
			usage.WebFetchRequests += u.ServerToolUse.WebFetchRequests
			if u.ServiceTier != "" {
				usage.ServiceTier = u.ServiceTier
			}
		}
		if len(event.ModelUsage) > 0 {
			models = event.ModelUsage
		}
	}
	if !saw {
		return LLMAgentUsage{}, false
	}
	usage.Source = usageSourceStreamResult
	if models != nil {
		applyClaudeModels(&usage, models)
	}
	return usage, true
}

// applyClaudeModels makes modelUsage the totals: the top-level usage covers the
// main loop only, while modelUsage also carries subagent and background models.
func applyClaudeModels(usage *LLMAgentUsage, models map[string]claudeModelUsage) {
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	usage.InputTokens, usage.OutputTokens, usage.CachedReadTokens = 0, 0, 0
	usage.CachedWriteTokens, usage.CachedWrite1hTokens, usage.ReasoningTokens = 0, 0, 0
	allPriced, modelCost := true, 0.0
	for _, name := range names {
		m := models[name]
		share := LLMAgentModelUsage{
			Model: name, InputTokens: m.InputTokens, OutputTokens: m.OutputTokens,
			CachedReadTokens: m.CacheReadTokens, CachedWriteTokens: m.CacheCreationTokens,
			CachedWrite1hTokens: m.CacheCreation1h, ReasoningTokens: m.ThinkingTokens,
			CostBasis: m.CostBasis,
		}
		if m.CostUSD != nil {
			share.CostUSD, share.CostReported = *m.CostUSD, true
			modelCost += *m.CostUSD
		} else {
			allPriced = false
		}
		usage.Models = append(usage.Models, share)
		usage.InputTokens += m.InputTokens
		usage.OutputTokens += m.OutputTokens
		usage.CachedReadTokens += m.CacheReadTokens
		usage.CachedWriteTokens += m.CacheCreationTokens
		usage.CachedWrite1hTokens += m.CacheCreation1h
		usage.ReasoningTokens += m.ThinkingTokens
	}
	if !usage.CostReported && allPriced {
		usage.CostUSD, usage.CostReported = modelCost, true
	}
}

// parseCodexStream reads codex exec --json. turn.completed carries the thread's
// running total (codex-rs exec usage_from_last_total), so the last one wins.
// Codex reports no call count, so APICalls stays empty rather than counting turns.
func parseCodexStream(stdout string) (LLMAgentUsage, bool) {
	var usage LLMAgentUsage
	saw := false
	for _, line := range eventLines(stdout, codexTurnCompleted) {
		var event struct {
			Usage *struct {
				InputTokens      int `json:"input_tokens"`
				CachedInput      int `json:"cached_input_tokens"`
				CacheWriteInput  int `json:"cache_write_input_tokens"`
				OutputTokens     int `json:"output_tokens"`
				ReasoningOutputs int `json:"reasoning_output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Usage == nil {
			continue
		}
		u := event.Usage
		usage = LLMAgentUsage{
			// Codex input_tokens is the whole prompt, cache reads and writes included.
			InputTokens:       max(0, u.InputTokens-u.CachedInput-u.CacheWriteInput),
			CachedReadTokens:  u.CachedInput,
			CachedWriteTokens: u.CacheWriteInput,
			OutputTokens:      u.OutputTokens,
			ReasoningTokens:   u.ReasoningOutputs,
			Source:            usageSourceStreamResult,
		}
		saw = true
	}
	return usage, saw
}

// parseOpenCodeStream sums step_finish parts, each of which covers its own
// step. OpenCode prints a zero cost for models it cannot price, so only a
// positive total counts as reported.
func parseOpenCodeStream(stdout string) (LLMAgentUsage, bool) {
	var usage LLMAgentUsage
	saw := false
	for _, line := range eventLines(stdout, openCodeStepFinish) {
		var event struct {
			SessionID string `json:"sessionID"`
			Part      struct {
				Cost   float64 `json:"cost"`
				Tokens *struct {
					Input     int `json:"input"`
					Output    int `json:"output"`
					Reasoning int `json:"reasoning"`
					Cache     struct {
						Read  int `json:"read"`
						Write int `json:"write"`
					} `json:"cache"`
				} `json:"tokens"`
			} `json:"part"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Part.Tokens == nil {
			continue
		}
		t := event.Part.Tokens
		usage.InputTokens += t.Input
		usage.OutputTokens += t.Output
		usage.ReasoningTokens += t.Reasoning
		usage.CachedReadTokens += t.Cache.Read
		usage.CachedWriteTokens += t.Cache.Write
		usage.CostUSD += event.Part.Cost
		usage.APICalls++
		usage.ProviderSessionID = event.SessionID
		saw = true
	}
	if !saw {
		return LLMAgentUsage{}, false
	}
	usage.CostReported = usage.CostUSD > 0
	usage.Source = usageSourceStreamResult
	return usage, true
}
