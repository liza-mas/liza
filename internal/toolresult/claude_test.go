package toolresult

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestClaudePostNativeOutputs(t *testing.T) {
	for _, tool := range []string{"Read", "Grep", "Glob"} {
		t.Run(tool, func(t *testing.T) {
			s := testStore(t)
			big := strings.Repeat("real-shaped source line\n", 4000)
			response := map[string]any{}
			switch tool {
			case "Read":
				response = map[string]any{"type": "text", "file": map[string]any{"filePath": "/repo/source.go", "content": big, "numLines": 4000, "startLine": 1, "totalLines": 4000}}
			case "Grep":
				response = map[string]any{"mode": "content", "content": big, "numFiles": 1, "filenames": []string{"/repo/source.go"}, "numLines": 4000}
			case "Glob":
				response = map[string]any{"filenames": strings.Split(big, "\n"), "numFiles": 4000, "durationMs": 2, "truncated": false}
			}
			hook := ClaudeHookInput{Event: "PostToolUse", Tool: tool, Response: response, Input: map[string]any{"path": "/repo"}, SessionID: "claude-session"}
			out, err := ClaudePostToolResult(s, hook, Result{TaskID: "task-778", AgentID: "coder-1"})
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(out)
			if len(encoded) > 5000 || !strings.Contains(string(encoded), "artifact_id") {
				t.Fatalf("not a bounded digest: %d", len(encoded))
			}
			stats, err := s.Stats()
			if err != nil || stats.ExternalizedCount != 1 {
				t.Fatalf("stats=%+v err=%v", stats, err)
			}
			again, err := ClaudePostToolResult(s, hook, Result{})
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ = json.Marshal(again)
			if !strings.Contains(string(encoded), `duplicate\":true`) {
				t.Fatalf("repeat not deduplicated: %s", encoded)
			}
		})
	}
}

func TestClaudePostSmallReadUnchanged(t *testing.T) {
	s := testStore(t)
	out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "text", "file": map[string]any{"filePath": "/repo/a.go", "content": "package main\n"}}}, Result{})
	if err != nil || len(out) != 0 {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestClaudeMCPTextAndBinaryPreserved(t *testing.T) {
	s := testStore(t)
	image := map[string]any{"type": "image", "data": "eA==", "mimeType": "image/png"}
	hook := ClaudeHookInput{Event: "PostToolUse", Tool: "mcp__fixture__lookup", Response: []any{map[string]any{"type": "text", "text": strings.Repeat("mcp output\n", 5000)}, image}}
	out, err := ClaudePostToolResult(s, hook, Result{})
	if err != nil {
		t.Fatal(err)
	}
	blocks := out["hookSpecificOutput"].(map[string]any)["updatedToolOutput"].([]any)
	if len(blocks) != 2 || blocks[1].(map[string]any)["data"] != "eA==" || !strings.Contains(blocks[0].(map[string]any)["text"].(string), "artifact_id") {
		t.Fatalf("unsafe MCP rewrite: %v", out)
	}
}

func TestClaudeFailureBatchStopsOversizedExactlyOnce(t *testing.T) {
	s := testStore(t)
	value := strings.Repeat("provider failure detail\n", 5000)
	// PostToolUseFailure is intentionally a no-op: Claude ignores that hook's
	// stop response for failed MCP output. The following PostToolBatch owns the
	// single durable receipt and terminal stop.
	out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolUseFailure", Tool: "mcp__fixture__lookup", Error: value}, Result{})
	if err != nil || len(out) != 0 {
		t.Fatalf("failure hook must defer to batch: out=%v err=%v", out, err)
	}
	out, err = ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolBatch", SessionID: "claude-session", ToolCalls: []ClaudeHookInput{{Tool: "mcp__fixture__lookup", Response: value, Input: map[string]any{"query": "fixture"}}}}, Result{TaskID: "DEV-778", AgentID: "coder-1", Role: "coder"})
	if err != nil {
		t.Fatal(err)
	}
	if out["continue"] != false || !strings.Contains(out["stopReason"].(string), "artifact_id") {
		t.Fatalf("large error not stopped from batch: %v", out)
	}
	stats, err := s.Stats()
	if err != nil || stats.ExternalizedCount != 1 {
		t.Fatalf("failure must persist one event: stats=%+v err=%v", stats, err)
	}
	// Small failures need neither a receipt nor an interruption.
	out, err = ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolBatch", ToolCalls: []ClaudeHookInput{{Tool: "mcp__fixture__lookup", Response: "not found"}}}, Result{})
	if err != nil || len(out) != 0 {
		t.Fatalf("small failure was changed: out=%v err=%v", out, err)
	}
}

// TestClaudePostSmallSecretReadStaysNative pins the round-5 blocker: a small
// Read of a .env or YAML file is redacted on its decoded content, keeps every
// line, and never stops the session.
func TestClaudePostSmallSecretReadStaysNative(t *testing.T) {
	for content, want := range map[string]string{
		"API_TOKEN=abc123\nDEBUG=true\nPORT=8080\nLOG_LEVEL=info\n": "API_TOKEN=[REDACTED]\nDEBUG=true\nPORT=8080\nLOG_LEVEL=info\n",
		"# db settings\npassword: Hunter2Secret99 # prod\n":         "# db settings\npassword: [REDACTED] # prod\n",
	} {
		s := testStore(t)
		hook := ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "text", "file": map[string]any{"filePath": "/repo/.env", "content": content, "numLines": 4}}}
		out, err := ClaudePostToolResult(s, hook, Result{})
		if err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		encoded, _ := json.Marshal(out)
		if strings.Contains(string(encoded), "Hunter2Secret99") || strings.Contains(string(encoded), "abc123") || strings.Contains(string(encoded), `"continue":false`) {
			t.Fatalf("secret leaked or session stopped: %s", encoded)
		}
		if !strings.Contains(string(encoded), strings.ReplaceAll(strings.ReplaceAll(want, "\n", `\n`), `"`, `\"`)) {
			t.Fatalf("lines not preserved: %s", encoded)
		}
	}
}

// numberedRead renders text the way PostToolBatch delivers a Read result: the
// model-facing, line-number-prefixed form, not PostToolUse's structured object.
func numberedRead(lines ...string) string {
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, line)
	}
	return b.String()
}

// defaultBatchStore uses the production threshold so the payloads reproduce the
// observed D91 band: PostToolUse passed the structured Read (<= 32 KiB) while
// the batch saw 32,869-34,051 bytes of line-numbered text.
func defaultBatchStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestClaudeBatchPassesOversizedNativeSuccess pins D91: PostToolUse owns the byte
// budget of successful native results, so the batch boundary must not stop the
// session on their longer model-facing rendering.
func TestClaudeBatchPassesOversizedNativeSuccess(t *testing.T) {
	lines := make([]string, 1100)
	for i := range lines {
		lines[i] = "real-shaped source line"
	}
	paths := make([]string, 1340)
	for i := range paths {
		paths[i] = fmt.Sprintf("/repo/pkg/source_%04d.go", i)
	}
	grep := strings.Repeat("/repo/pkg/source.go:12:real-shaped source line\n", 750)
	for tool, response := range map[string]string{"Read": numberedRead(lines...), "Grep": grep, "Glob": strings.Join(paths, "\n")} {
		t.Run(tool, func(t *testing.T) {
			// GIVEN a successful native result just over the threshold, with no secrets
			s := defaultBatchStore(t)
			if len(response) <= DefaultThresholdBytes || len(response) > 36*1024 {
				t.Fatalf("fixture outside the D91 band: %d bytes", len(response))
			}
			// WHEN the batch boundary inspects it
			out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolBatch", SessionID: "claude-session", ToolCalls: []ClaudeHookInput{{Tool: tool, Response: response, Input: map[string]any{"path": "/repo"}}}}, Result{})
			if err != nil {
				t.Fatal(err)
			}
			// THEN the session continues and nothing is recorded as a failure
			if len(out) != 0 {
				t.Fatalf("successful native result stopped the session: continue=%v", out["continue"])
			}
			stats, err := s.Stats()
			if err != nil || stats.ExternalizedCount != 0 {
				t.Fatalf("successful native result recorded as failure: stats=%+v err=%v", stats, err)
			}
		})
	}
}

// TestClaudeBatchNativeSkipKeepsScanning guards that exempting a native entry
// from the size check continues to later entries instead of returning early.
func TestClaudeBatchNativeSkipKeepsScanning(t *testing.T) {
	lines := make([]string, 1100)
	for i := range lines {
		lines[i] = "real-shaped source line"
	}
	s := defaultBatchStore(t)
	failure := strings.Repeat("provider failure detail\n", 2000)
	out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolBatch", SessionID: "claude-session", ToolCalls: []ClaudeHookInput{
		{Tool: "Read", Response: numberedRead(lines...), Input: map[string]any{"file_path": "/repo/source.go"}},
		{Tool: "mcp__fixture__lookup", Response: failure, Input: map[string]any{"query": "fixture"}},
	}}, Result{})
	if err != nil {
		t.Fatal(err)
	}
	reason, _ := out["stopReason"].(string)
	if out["continue"] != false || !strings.Contains(reason, `"tool":"mcp__fixture__lookup"`) {
		t.Fatalf("oversized MCP failure after a native entry was not the stop cause: continue=%v reason=%.200s", out["continue"], reason)
	}
	stats, err := s.Stats()
	if err != nil || stats.ExternalizedCount != 1 {
		t.Fatalf("only the MCP failure must persist: stats=%+v err=%v", stats, err)
	}
}

// TestClaudeBatchStopsUnsanitizedNative guards the retained backstop: residual
// native content the sanitizer would change still stops without leaking it.
func TestClaudeBatchStopsUnsanitizedNative(t *testing.T) {
	s := testStore(t)
	response := numberedRead("API_TOKEN=abc123", "DEBUG=true")
	out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolBatch", ToolCalls: []ClaudeHookInput{{Tool: "Read", Response: response, Input: map[string]any{"file_path": "/repo/.env"}}}}, Result{})
	if err != nil {
		t.Fatal(err)
	}
	reason, _ := out["stopReason"].(string)
	if out["continue"] != false || strings.Contains(reason, "abc123") {
		t.Fatalf("unsanitized native content not stopped safely: %v", out)
	}
}

// TestClaudeBatchKeepsRedactedNativeReplacement guards that the retained
// backstop accepts PostToolUse's already-redacted output in its batch form.
func TestClaudeBatchKeepsRedactedNativeReplacement(t *testing.T) {
	s := testStore(t)
	response := numberedRead("API_TOKEN=[REDACTED]", "DEBUG=true", "PORT=8080", "LOG_LEVEL=info")
	out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolBatch", ToolCalls: []ClaudeHookInput{{Tool: "Read", Response: response, Input: map[string]any{"file_path": "/repo/.env"}}}}, Result{})
	if err != nil || len(out) != 0 {
		t.Fatalf("redacted native replacement stopped the session: out=%v err=%v", out, err)
	}
}
