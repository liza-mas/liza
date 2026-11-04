package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/usage"
)

// claudeResultFixture is the shape of a claude -p stream-json result event
// captured on 2026-11-04, extended with a subagent model.
const claudeResultFixture = `{"type":"result","subtype":"success","is_error":false,"session_id":"5fd192d6-345b-4dcd-b3ad-4452aad9f9f5",` +
	`"total_cost_usd":0.25,"duration_ms":5402,"duration_api_ms":2934,"ttft_ms":3084,"num_turns":3,` +
	`"usage":{"input_tokens":2,"cache_creation_input_tokens":34091,"cache_read_input_tokens":23607,"output_tokens":13,` +
	`"output_tokens_details":{"thinking_tokens":4},"server_tool_use":{"web_search_requests":1,"web_fetch_requests":2},` +
	`"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":34091,"ephemeral_5m_input_tokens":0}},` +
	`"modelUsage":{` +
	`"claude-opus-5-5":{"inputTokens":2,"outputTokens":13,"cacheReadInputTokens":23607,"cacheCreationInputTokens":34091,` +
	`"cacheCreationEphemeral1hInputTokens":34091,"thinkingTokens":4,"costUSD":0.2},` +
	`"claude-haiku-5-5":{"inputTokens":50,"outputTokens":7,"cacheReadInputTokens":100,"cacheCreationInputTokens":900,` +
	`"cacheCreationEphemeral1hInputTokens":0,"thinkingTokens":0,"costUSD":0.05}}}`

func TestParseCLIUsageClaudeResult(t *testing.T) {
	stdout := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-5-5"}`,
		`not json at all`,
		`{"type":"assistant","message":{"usage":{"input_tokens":999}}}`,
		claudeResultFixture,
	}, "\n")
	got, ok := parseCLIUsage(stdout)
	if !ok {
		t.Fatal("parseCLIUsage found no usage in a stream ending with a result event")
	}
	want := LLMAgentUsage{
		// Totals are the modelUsage sums, so the subagent model is counted.
		InputTokens: 52, OutputTokens: 20, CachedReadTokens: 23707, CachedWriteTokens: 34991,
		CachedWrite1hTokens: 34091, ReasoningTokens: 4,
		CostUSD: 0.25, CostReported: true,
		Models: []LLMAgentModelUsage{
			{Model: "claude-haiku-5-5", InputTokens: 50, OutputTokens: 7, CachedReadTokens: 100, CachedWriteTokens: 900, CostUSD: 0.05, CostReported: true},
			{Model: "claude-opus-5-5", InputTokens: 2, OutputTokens: 13, CachedReadTokens: 23607, CachedWriteTokens: 34091,
				CachedWrite1hTokens: 34091, ReasoningTokens: 4, CostUSD: 0.2, CostReported: true},
		},
		ProviderSessionID: "5fd192d6-345b-4dcd-b3ad-4452aad9f9f5",
		Source:            usageSourceStreamResult,
		WebSearchRequests: 1, WebFetchRequests: 2,
		APIDurationMs: 2934, TTFTMs: 3084, APICalls: 3, ServiceTier: "standard",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCLIUsage =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseCLIUsageClaudeResultWithoutModelUsage(t *testing.T) {
	got, ok := parseCLIUsage(`{"type":"result","total_cost_usd":0,"usage":{"input_tokens":5,"output_tokens":3,` +
		`"cache_read_input_tokens":7,"cache_creation_input_tokens":11,"cache_creation":{"ephemeral_1h_input_tokens":6}}}`)
	if !ok {
		t.Fatal("parseCLIUsage found no usage")
	}
	if got.InputTokens != 5 || got.OutputTokens != 3 || got.CachedReadTokens != 7 || got.CachedWriteTokens != 11 ||
		got.CachedWrite1hTokens != 6 || len(got.Models) != 0 {
		t.Fatalf("usage = %+v, want the top-level usage object", got)
	}
	// A reported zero is a price, not an absent one.
	if !got.CostReported || got.CostUSD != 0 {
		t.Fatalf("cost = (%v, %v), want a reported zero", got.CostUSD, got.CostReported)
	}
}

func TestParseCLIUsageCodexTurns(t *testing.T) {
	stdout := strings.Join([]string{
		`{"type":"thread.started","thread_id":"01a1a5db"}`,
		`{"type":"turn.started"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1000,"cached_input_tokens":600,"cache_write_input_tokens":100,"output_tokens":40,"reasoning_output_tokens":30}}`,
		`{"type":"turn.completed","usage":{"input_tokens":500,"cached_input_tokens":500,"output_tokens":10,"reasoning_output_tokens":0}}`,
	}, "\n")
	got, ok := parseCLIUsage(stdout)
	if !ok {
		t.Fatal("parseCLIUsage found no usage in codex exec --json output")
	}
	// Codex input_tokens include the cache, so fresh input is what remains.
	want := LLMAgentUsage{
		InputTokens: 300, CachedReadTokens: 1100, CachedWriteTokens: 100, OutputTokens: 50, ReasoningTokens: 30,
		APICalls: 2, Source: usageSourceStreamResult,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCLIUsage = %+v, want %+v", got, want)
	}
}

func TestParseCLIUsageOpenCodeSteps(t *testing.T) {
	step := func(cost string, input int) string {
		return `{"type":"step_finish","sessionID":"ses_1","part":{"type":"step-finish","cost":` + cost +
			`,"tokens":{"input":` + strconv.Itoa(input) + `,"output":2,"reasoning":1,"cache":{"read":3,"write":4}}}}`
	}
	unpriced, ok := parseCLIUsage(step("0", 10) + "\n" + step("0", 20))
	if !ok {
		t.Fatal("parseCLIUsage found no usage in opencode --format json output")
	}
	if unpriced.InputTokens != 30 || unpriced.OutputTokens != 4 || unpriced.ReasoningTokens != 2 ||
		unpriced.CachedReadTokens != 6 || unpriced.CachedWriteTokens != 8 || unpriced.APICalls != 2 || unpriced.ProviderSessionID != "ses_1" {
		t.Fatalf("usage = %+v, want the two steps summed", unpriced)
	}
	if unpriced.CostReported {
		t.Fatal("OpenCode's zero cost marks an unpriced model, not a free one")
	}
	priced, _ := parseCLIUsage(step("0.01", 10) + "\n" + step("0.02", 10))
	if !priced.CostReported || priced.CostUSD < 0.0299 || priced.CostUSD > 0.0301 {
		t.Fatalf("cost = (%v, %v), want 0.03 reported", priced.CostUSD, priced.CostReported)
	}
}

func TestParseCLIUsageWithoutUsageEvent(t *testing.T) {
	for name, stdout := range map[string]string{
		"plain_text":       "provider output\n",
		"empty":            "",
		"malformed_result": `{"type":"result","usage":` + "\n",
		"result_no_usage":  `{"type":"result","subtype":"success","result":"done"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if got, ok := parseCLIUsage(stdout); ok || !reflect.DeepEqual(got, LLMAgentUsage{}) {
				t.Fatalf("parseCLIUsage = (%+v, %v), want no usage", got, ok)
			}
		})
	}
}

// TestCLIAgentRecordsStreamUsage drives a CLI run whose provider prints a
// Claude result event through the supervisor sink, and checks the durable
// record carries the reported tokens, models and cost as authoritative.
func TestCLIAgentRecordsStreamUsage(t *testing.T) {
	binDir := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "result.jsonl")
	if err := os.WriteFile(fixture, []byte(claudeResultFixture+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testhelpers.WriteShellStub(t, filepath.Join(binDir, "claude"), "#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo \"2.1.267 (Claude Code)\"; exit 0; fi\ncat \"$FAKE_CLI_FIXTURE\"\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_CLI_FIXTURE", fixture)

	cfg := newUsageSinkTestConfig(t, "run-stream-usage")
	result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "claude", AgentID: cfg.AgentID, TaskID: cfg.TaskID, SessionID: cfg.SessionID,
		Model: "claude-opus-5-5", Prompt: "prompt body", ProjectRoot: t.TempDir(),
		EventSink: NewUsageEventSink(cfg), LaunchGate: immediateLaunchGate,
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("Run = (%d, %v), want a successful run", result.ExitCode, err)
	}
	if result.Usage.Model != "claude-opus-5-5" || result.Usage.OutputTokens != 20 {
		t.Fatalf("result usage = %+v, want the parsed usage with the requested model", result.Usage)
	}

	records := loadUsageRecordsForTest(t, cfg.ProjectRoot)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1: %+v", len(records), records)
	}
	got := records[0]
	if got.Provenance != usage.ProvenanceTerminalAuthoritative {
		t.Fatalf("provenance = %q, want terminal_authoritative", got.Provenance)
	}
	if got.FreshInputTokens != 52 || got.CacheReadTokens != 23707 || got.CacheWriteTokens != 34991 ||
		got.CacheWrite1hTokens != 34091 || got.OutputTokens != 20 || got.ReasoningTokens != 4 {
		t.Fatalf("token counts = %+v", got)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.25 || got.RequestedModel != "claude-opus-5-5" ||
		got.ProviderSessionID != "5fd192d6-345b-4dcd-b3ad-4452aad9f9f5" || got.UsageSource != usageSourceStreamResult {
		t.Fatalf("record attribution = %+v", got)
	}
	if len(got.Models) != 2 || got.Models[1].Model != "claude-opus-5-5" || got.Models[1].CostUSD == nil || *got.Models[1].CostUSD != 0.2 {
		t.Fatalf("models = %+v, want both billed models with their cost", got.Models)
	}
}
