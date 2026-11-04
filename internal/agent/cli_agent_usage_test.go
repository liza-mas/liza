package agent

import (
	"context"
	"fmt"
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
	`"cacheCreationEphemeral1hInputTokens":34091,"thinkingTokens":4,"costUSD":0.2,"costBasis":"list"},` +
	`"claude-haiku-5-5":{"inputTokens":50,"outputTokens":7,"cacheReadInputTokens":100,"cacheCreationInputTokens":900,` +
	`"cacheCreationEphemeral1hInputTokens":0,"thinkingTokens":0,"costUSD":0.05}}}`

func TestParseCLIUsageClaudeResult(t *testing.T) {
	stdout := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-5-5"}`,
		`not json at all`,
		`{"type":"assistant","message":{"usage":{"input_tokens":999}}}`,
		claudeResultFixture,
	}, "\n")
	got, ok := parseCLIUsage(claudeBackend, stdout)
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
				CachedWrite1hTokens: 34091, ReasoningTokens: 4, CostUSD: 0.2, CostReported: true, CostBasis: "list"},
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
	got, ok := parseCLIUsage(claudeBackend, `{"type":"result","total_cost_usd":0,"usage":{"input_tokens":5,"output_tokens":3,`+
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
		`{"type":"turn.completed","usage":{"input_tokens":1500,"cached_input_tokens":1100,"cache_write_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":30}}`,
	}, "\n")
	got, ok := parseCLIUsage(codexBackend, stdout)
	if !ok {
		t.Fatal("parseCLIUsage found no usage in codex exec --json output")
	}
	// Each turn.completed is the thread's running total, so the last wins; its
	// input_tokens include the cache, so fresh input is what remains.
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
	unpriced, ok := parseCLIUsage(openCodeBackend, step("0", 10)+"\n"+step("0", 20))
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
	priced, _ := parseCLIUsage(openCodeBackend, step("0.01", 10)+"\n"+step("0.02", 10))
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
			if got, ok := parseCLIUsage(claudeBackend, stdout); ok || !reflect.DeepEqual(got, LLMAgentUsage{}) {
				t.Fatalf("parseCLIUsage = (%+v, %v), want no usage", got, ok)
			}
		})
	}
}

// claudeTurnResult is one result event of a -p run that woke for background
// work, shaped after a capture from 2026-11-04: num_turns and usage cover the
// turn, modelUsage, total_cost_usd and duration_api_ms run across the process.
func claudeTurnResult(turns, input, output, ttft, apiMs int, cost float64, models string) string {
	return fmt.Sprintf(`{"type":"result","session_id":"s1","num_turns":%d,"ttft_ms":%d,"duration_api_ms":%d,"total_cost_usd":%g,`+
		`"usage":{"input_tokens":%d,"output_tokens":%d,"server_tool_use":{"web_search_requests":1,"web_fetch_requests":0},"service_tier":"standard"}%s}`,
		turns, ttft, apiMs, cost, input, output, models)
}

func TestParseCLIUsageClaudeBackgroundTurns(t *testing.T) {
	first := claudeTurnResult(2, 4, 933, 5549, 9225, 0.0088, `,"modelUsage":{"claude-haiku-5-5":{"inputTokens":4,"outputTokens":933,"costUSD":0.0088}}`)
	last := claudeTurnResult(1, 4, 241, 3579, 12989, 0.0097, `,"modelUsage":{"claude-haiku-5-5":{"inputTokens":8,"outputTokens":1174,"costUSD":0.0097}}`)
	got, ok := parseCLIUsage(claudeBackend, first+"\n"+last)
	if !ok {
		t.Fatal("parseCLIUsage found no usage")
	}
	if got.InputTokens != 8 || got.OutputTokens != 1174 || len(got.Models) != 1 || got.CostUSD != 0.0097 {
		t.Fatalf("usage = %+v, want the last result's running totals, not a sum", got)
	}
	if got.APICalls != 3 || got.WebSearchRequests != 2 || got.TTFTMs != 5549 || got.APIDurationMs != 12989 {
		t.Fatalf("usage = %+v, want turns and tool counts summed, first TTFT, last API duration", got)
	}

	// Without modelUsage the per-turn usage is all there is, so it is summed.
	plain, _ := parseCLIUsage(claudeBackend, claudeTurnResult(2, 4, 933, 1, 1, 0.0088, "")+"\n"+claudeTurnResult(1, 4, 241, 1, 1, 0.0097, ""))
	if plain.InputTokens != 8 || plain.OutputTokens != 1174 {
		t.Fatalf("usage = %+v, want both turns' usage summed", plain)
	}
}

func TestParseCLIUsageClaudeModelCosts(t *testing.T) {
	// A model without costUSD stays unpriced instead of inheriting the total.
	got, _ := parseCLIUsage(claudeBackend, `{"type":"result","total_cost_usd":0.3,"modelUsage":{`+
		`"a":{"inputTokens":1,"costUSD":0.3},"b":{"inputTokens":2}}}`)
	if !got.CostReported || got.CostUSD != 0.3 || !got.Models[0].CostReported || got.Models[1].CostReported {
		t.Fatalf("usage = %+v, want the total and model a priced, model b unpriced", got)
	}
	// No total but every model priced: the total is their sum.
	summed, _ := parseCLIUsage(claudeBackend, `{"type":"result","modelUsage":{"a":{"inputTokens":1,"costUSD":0.25},"b":{"inputTokens":2,"costUSD":0.5}}}`)
	if !summed.CostReported || summed.CostUSD != 0.75 {
		t.Fatalf("cost = (%v, %v), want the model sum", summed.CostUSD, summed.CostReported)
	}
	partial, _ := parseCLIUsage(claudeBackend, `{"type":"result","modelUsage":{"a":{"inputTokens":1,"costUSD":0.25},"b":{"inputTokens":2}}}`)
	if partial.CostReported {
		t.Fatalf("cost = %v reported, want unpriced when a model has no price", partial.CostUSD)
	}
}

// TestParseCLIUsageReadsOnlyTheBackendsOwnEvent pins the backend gate: a
// stream-json backend whose usage semantics are unverified records unknown,
// even when it prints an event shaped like a verified one.
func TestParseCLIUsageReadsOnlyTheBackendsOwnEvent(t *testing.T) {
	codexTurn := `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":1}}`
	for _, tc := range []struct{ backend, stdout string }{
		{"gemini", claudeResultFixture},
		{"qwen", claudeResultFixture},
		{"cursor", claudeResultFixture},
		{claudeBackend, codexTurn},
		{codexBackend, claudeResultFixture},
	} {
		if got, ok := parseCLIUsage(tc.backend, tc.stdout); ok || !reflect.DeepEqual(got, LLMAgentUsage{}) {
			t.Fatalf("parseCLIUsage(%s) = (%+v, %v), want no usage", tc.backend, got, ok)
		}
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

// TestCLIAgentRecordsCodexAndOpenCodeUsage drives the other two verified
// carriers through the supervisor sink to a durable record.
func TestCLIAgentRecordsCodexAndOpenCodeUsage(t *testing.T) {
	for _, tc := range []struct {
		backend, version, stdout string
		fresh, cacheRead, output int
	}{
		{codexBackend, "codex-cli 0.154.0", `{"type":"turn.completed","usage":{"input_tokens":1000,"cached_input_tokens":600,"output_tokens":40}}`, 400, 600, 40},
		{openCodeBackend, "1.0.0", `{"type":"step_finish","sessionID":"ses_1","part":{"cost":0,"tokens":{"input":30,"output":4,"cache":{"read":6,"write":0}}}}`, 30, 6, 4},
	} {
		t.Run(tc.backend, func(t *testing.T) {
			binDir := t.TempDir()
			fixture := filepath.Join(t.TempDir(), "events.jsonl")
			if err := os.WriteFile(fixture, []byte(tc.stdout+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			testhelpers.WriteShellStub(t, filepath.Join(binDir, tc.backend), "#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo \""+tc.version+"\"; exit 0; fi\ncat \"$FAKE_CLI_FIXTURE\"\n")
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FAKE_CLI_FIXTURE", fixture)

			cfg := newUsageSinkTestConfig(t, "run-"+tc.backend)
			result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
				BackendName: tc.backend, AgentID: cfg.AgentID, TaskID: cfg.TaskID, SessionID: cfg.SessionID,
				Prompt: "prompt body", ProjectRoot: t.TempDir(),
				EventSink: NewUsageEventSink(cfg), LaunchGate: immediateLaunchGate,
			})
			if err != nil || result.ExitCode != 0 {
				t.Fatalf("Run = (%d, %v), want a successful run", result.ExitCode, err)
			}
			records := loadUsageRecordsForTest(t, cfg.ProjectRoot)
			if len(records) != 1 {
				t.Fatalf("records = %d, want 1: %+v", len(records), records)
			}
			got := records[0]
			if got.Provenance != usage.ProvenanceTerminalAuthoritative || got.FreshInputTokens != tc.fresh ||
				got.CacheReadTokens != tc.cacheRead || got.OutputTokens != tc.output || got.CostUSD != nil {
				t.Fatalf("record = %+v, want fresh %d, cache read %d, output %d, unpriced", got, tc.fresh, tc.cacheRead, tc.output)
			}
		})
	}
}
