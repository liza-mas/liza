package agent

import (
	"encoding/json"
	"sort"
	"strings"
)

// usageSourceStreamResult marks counts read from a CLI's own JSON output stream.
const usageSourceStreamResult = "stream_result"

// The JSON events that carry a CLI run's usage. Each CLI emits only its own,
// so one pass over stdout serves every backend without knowing which one ran.
const (
	claudeResultEvent      = "result"         // claude -p --output-format stream-json
	codexTurnCompleted     = "turn.completed" // codex exec --json
	openCodeStepFinish     = "step_finish"    // opencode run --format json
	jsonObjectStreamPrefix = "{"
)

// parseCLIUsage reads the usage a CLI reported in its JSON output. ok is false
// when no usage event arrived — plain-text providers, --no-log runs, crashes —
// so the caller keeps emitting the empty usage that records unknown provenance.
func parseCLIUsage(stdout string) (LLMAgentUsage, bool) {
	var claude, codex, openCode LLMAgentUsage
	var sawClaude, sawCodex, sawOpenCode bool
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, jsonObjectStreamPrefix) {
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &head) != nil {
			continue
		}
		switch head.Type {
		case claudeResultEvent:
			// A -p run ends with one result; a later one supersedes an earlier.
			if parsed, ok := parseClaudeResult(line); ok {
				claude, sawClaude = parsed, true
			}
		case codexTurnCompleted:
			if addCodexTurn(&codex, line) {
				sawCodex = true
			}
		case openCodeStepFinish:
			if addOpenCodeStep(&openCode, line) {
				sawOpenCode = true
			}
		}
	}
	switch {
	case sawClaude:
		return claude, true
	case sawCodex:
		return codex, true
	case sawOpenCode:
		return openCode, true
	}
	return LLMAgentUsage{}, false
}

type claudeModelUsage struct {
	InputTokens         int     `json:"inputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	CacheReadTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationTokens int     `json:"cacheCreationInputTokens"`
	CacheCreation1h     int     `json:"cacheCreationEphemeral1hInputTokens"`
	ThinkingTokens      int     `json:"thinkingTokens"`
	CostUSD             float64 `json:"costUSD"`
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

// parseClaudeResult maps Claude's result event. When modelUsage is present its
// per-model sums are the totals: the top-level usage covers the main loop only,
// while modelUsage also carries subagent and background models.
func parseClaudeResult(line string) (LLMAgentUsage, bool) {
	var event claudeResultLine
	if json.Unmarshal([]byte(line), &event) != nil || (event.Usage == nil && len(event.ModelUsage) == 0) {
		return LLMAgentUsage{}, false
	}
	usage := LLMAgentUsage{
		Source:            usageSourceStreamResult,
		ProviderSessionID: event.SessionID,
		APIDurationMs:     event.DurationAPIMs,
		TTFTMs:            event.TTFTMs,
		APICalls:          event.NumTurns,
	}
	if event.TotalCostUSD != nil {
		usage.CostUSD, usage.CostReported = *event.TotalCostUSD, true
	}
	if u := event.Usage; u != nil {
		usage.InputTokens, usage.OutputTokens = u.InputTokens, u.OutputTokens
		usage.CachedReadTokens, usage.CachedWriteTokens = u.CacheReadTokens, u.CacheCreationTokens
		usage.CachedWrite1hTokens = u.CacheCreation.Ephemeral1h
		usage.ReasoningTokens = u.OutputDetails.ThinkingTokens
		usage.WebSearchRequests = u.ServerToolUse.WebSearchRequests
		usage.WebFetchRequests = u.ServerToolUse.WebFetchRequests
		usage.ServiceTier = u.ServiceTier
	}
	if len(event.ModelUsage) == 0 {
		return usage, true
	}
	names := make([]string, 0, len(event.ModelUsage))
	for name := range event.ModelUsage {
		names = append(names, name)
	}
	sort.Strings(names)
	usage.InputTokens, usage.OutputTokens, usage.CachedReadTokens = 0, 0, 0
	usage.CachedWriteTokens, usage.CachedWrite1hTokens, usage.ReasoningTokens = 0, 0, 0
	for _, name := range names {
		m := event.ModelUsage[name]
		usage.Models = append(usage.Models, LLMAgentModelUsage{
			Model: name, InputTokens: m.InputTokens, OutputTokens: m.OutputTokens,
			CachedReadTokens: m.CacheReadTokens, CachedWriteTokens: m.CacheCreationTokens,
			CachedWrite1hTokens: m.CacheCreation1h, ReasoningTokens: m.ThinkingTokens,
			CostUSD: m.CostUSD, CostReported: usage.CostReported,
		})
		usage.InputTokens += m.InputTokens
		usage.OutputTokens += m.OutputTokens
		usage.CachedReadTokens += m.CacheReadTokens
		usage.CachedWriteTokens += m.CacheCreationTokens
		usage.CachedWrite1hTokens += m.CacheCreation1h
		usage.ReasoningTokens += m.ThinkingTokens
	}
	return usage, true
}

// addCodexTurn adds one turn.completed usage. Each event covers its own turn,
// so an exec run with several turns sums them.
func addCodexTurn(usage *LLMAgentUsage, line string) bool {
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
		return false
	}
	u := event.Usage
	// Codex input_tokens is the whole prompt, cache reads and writes included.
	usage.InputTokens += max(0, u.InputTokens-u.CachedInput-u.CacheWriteInput)
	usage.CachedReadTokens += u.CachedInput
	usage.CachedWriteTokens += u.CacheWriteInput
	usage.OutputTokens += u.OutputTokens
	usage.ReasoningTokens += u.ReasoningOutputs
	usage.APICalls++
	usage.Source = usageSourceStreamResult
	return true
}

// addOpenCodeStep adds one step_finish part. OpenCode prints a zero cost for
// models it cannot price, so only a positive total counts as reported.
func addOpenCodeStep(usage *LLMAgentUsage, line string) bool {
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
		return false
	}
	t := event.Part.Tokens
	usage.InputTokens += t.Input
	usage.OutputTokens += t.Output
	usage.ReasoningTokens += t.Reasoning
	usage.CachedReadTokens += t.Cache.Read
	usage.CachedWriteTokens += t.Cache.Write
	usage.CostUSD += event.Part.Cost
	usage.CostReported = usage.CostUSD > 0
	usage.APICalls++
	usage.ProviderSessionID = event.SessionID
	usage.Source = usageSourceStreamResult
	return true
}
