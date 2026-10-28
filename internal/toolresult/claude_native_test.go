package toolresult

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func claudeStoredEvents(t *testing.T, s *Store) []Event {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(s.path, "events"))
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(s.path, "events", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var event Event
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

// claudeDigestIn returns the digest the replacement embeds in the tool's
// native display field.
func claudeDigestIn(t *testing.T, tool string, out map[string]any) Digest {
	t.Helper()
	updated := out["hookSpecificOutput"].(map[string]any)["updatedToolOutput"].(map[string]any)
	var text string
	switch tool {
	case "Read":
		text = updated["file"].(map[string]any)["content"].(string)
	case "Grep":
		text = updated["content"].(string)
	case "Glob":
		text = updated["filenames"].([]any)[0].(string)
	}
	return parseDigest(t, text)
}

func TestClaudeNativeRoutingAndShapeRefusals(t *testing.T) {
	var nilMap map[string]any
	const (
		invalidShape   = "invalid native Claude response shape"
		invalidRead    = "invalid Claude Read output shape"
		invalidContent = "invalid Claude Read content shape"
		missing        = "missing Claude tool response"
		invalidMCP     = "invalid Claude MCP content-block shape"
	)
	for _, tc := range []struct {
		name    string
		hook    ClaudeHookInput
		wantErr string
	}{
		{"failure event is a no-op before shape checks", ClaudeHookInput{Event: "PostToolUseFailure", Tool: "Read", Response: "x"}, ""},
		{"unknown event is a no-op", ClaudeHookInput{Event: "SessionStart", Tool: "Grep", Response: "x"}, ""},
		{"empty event is a no-op", ClaudeHookInput{Tool: "Grep", Response: "x"}, ""},
		{"empty batch delegates", ClaudeHookInput{Event: "PostToolBatch"}, ""},
		{"MCP routes before the native map assertion", ClaudeHookInput{Event: "PostToolUse", Tool: "mcp__fixture__lookup", Response: map[string]any{"type": "text"}}, invalidMCP},
		{"non-map Read response", ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: "x"}, invalidShape},
		{"non-map unsupported tool response", ClaudeHookInput{Event: "PostToolUse", Tool: "Bash", Response: "x"}, invalidShape},
		{"absent unsupported tool response", ClaudeHookInput{Event: "PostToolUse", Tool: "Bash"}, invalidShape},
		{"typed nil map on unsupported tool", ClaudeHookInput{Event: "PostToolUse", Tool: "Bash", Response: nilMap}, ""},
		{"typed nil map on Read is non-text", ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: nilMap}, ""},
		{"typed nil map on Grep", ClaudeHookInput{Event: "PostToolUse", Tool: "Grep", Response: nilMap}, missing},
		{"typed nil map on Glob", ClaudeHookInput{Event: "PostToolUse", Tool: "Glob", Response: nilMap}, missing},
		{"unsupported tool with map response", ClaudeHookInput{Event: "PostToolUse", Tool: "Bash", Response: map[string]any{"stdout": strings.Repeat("x", 9000)}}, ""},
		{"non-text Read", ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "image", "file": "ignored"}}, ""},
		{"text Read without file", ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "text"}}, invalidRead},
		{"text Read with non-object file", ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "text", "file": "x"}}, invalidRead},
		{"text Read without content", ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "text", "file": map[string]any{}}}, invalidContent},
		{"text Read with non-string content", ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "text", "file": map[string]any{"content": 7}}}, invalidContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			out, err := ClaudePostToolResult(s, tc.hook, Result{})
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr || out != nil {
					t.Fatalf("out=%v err=%v, want nil output and error %q", out, err, tc.wantErr)
				}
			} else if err != nil || out == nil || len(out) != 0 {
				t.Fatalf("out=%v err=%v, want empty object", out, err)
			}
			if events := claudeStoredEvents(t, s); len(events) != 0 {
				t.Fatalf("routing or refusal persisted %d events", len(events))
			}
		})
	}
}

func TestClaudeNativeInputEncodingFailureFollowsContentEncoding(t *testing.T) {
	s := testStore(t)
	hook := ClaudeHookInput{Event: "PostToolUse", Tool: "Grep", Response: map[string]any{"content": "x"}, Input: map[string]any{"bad": make(chan int)}}
	out, err := ClaudePostToolResult(s, hook, Result{})
	if err == nil || err.Error() != "invalid Claude input encoding" || out != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
	hook.Response = map[string]any{"content": make(chan int)}
	out, err = ClaudePostToolResult(s, hook, Result{})
	if err == nil || err.Error() != "invalid Claude response encoding" || out != nil {
		t.Fatalf("content encoding must be reported first: out=%v err=%v", out, err)
	}
}

func TestClaudeNativeMetadataAndTruncation(t *testing.T) {
	big := strings.Repeat("real-shaped source line\n", 400)
	grep := func(extra map[string]any) map[string]any {
		response := map[string]any{"mode": "content", "content": big, "numFiles": 1, "filenames": []string{"/repo/source.go"}}
		maps.Copy(response, extra)
		return response
	}
	read := func(file map[string]any) map[string]any {
		file["filePath"], file["content"] = "/repo/source.go", big
		return map[string]any{"type": "text", "file": file}
	}
	for _, tc := range []struct {
		name     string
		tool     string
		response map[string]any
		seed     bool
		want     bool
	}{
		{"explicit truncated", "Grep", grep(map[string]any{"truncated": true}), false, true},
		{"explicit false overwrites seeded truncation", "Grep", grep(map[string]any{"truncated": false}), true, false},
		{"seed survives without the field", "Grep", grep(nil), true, true},
		{"non-bool truncated is ignored", "Grep", grep(map[string]any{"truncated": "true"}), false, false},
		{"float64 total over int lines", "Grep", grep(map[string]any{"totalLines": float64(10), "numLines": 5}), false, true},
		{"int total over float64 lines", "Grep", grep(map[string]any{"totalLines": 10, "numLines": float64(5)}), false, true},
		{"zero lines never truncate", "Grep", grep(map[string]any{"totalLines": 10, "numLines": 0}), false, false},
		{"equal counts", "Grep", grep(map[string]any{"totalLines": 5, "numLines": 5}), false, false},
		{"int64 counts are ignored", "Grep", grep(map[string]any{"totalLines": int64(10), "numLines": int64(5)}), false, false},
		{"string counts are ignored", "Grep", grep(map[string]any{"totalLines": "10", "numLines": "5"}), false, false},
		{"explicit false then root counts", "Grep", grep(map[string]any{"truncated": false, "totalLines": 10, "numLines": 5}), false, true},
		{"file counts", "Read", read(map[string]any{"totalLines": 10, "numLines": 5}), false, true},
		{"file float64 counts", "Read", read(map[string]any{"totalLines": float64(10), "numLines": float64(5)}), false, true},
		{"file zero lines", "Read", read(map[string]any{"totalLines": 10, "numLines": 0}), false, false},
		{"file int32 counts are ignored", "Read", read(map[string]any{"totalLines": int32(10), "numLines": int32(5)}), false, false},
		{"file counts do not clear the seed", "Read", read(map[string]any{"totalLines": 5, "numLines": 5}), true, true},
		{"root counts on Read", "Read", func() map[string]any {
			r := read(map[string]any{})
			r["totalLines"], r["numLines"] = 10, 5
			return r
		}(), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			input := map[string]any{"path": "/repo", "pattern": "needle"}
			hook := ClaudeHookInput{Event: "PostToolUse", Tool: HookTool(tc.tool), Response: tc.response, Input: input, SessionID: "claude-session"}
			out, err := ClaudePostToolResult(s, hook, Result{TaskID: "task-778", AgentID: "coder-1", Role: "coder", Truncated: tc.seed})
			if err != nil {
				t.Fatal(err)
			}
			events := claudeStoredEvents(t, s)
			if len(events) != 1 {
				t.Fatalf("Store.Process must run once: %d events", len(events))
			}
			event := events[0]
			if event.SourceTruncated != tc.want || event.Tool != tc.tool || event.SessionID != "claude-session" ||
				event.TaskID != "task-778" || event.AgentID != "coder-1" || event.Role != "coder" || !event.Externalized {
				t.Fatalf("event metadata: %+v", event)
			}
			content, _ := json.Marshal(tc.response)
			if event.OriginalBytes != len(content) {
				t.Fatalf("original bytes %d != encoded response %d", event.OriginalBytes, len(content))
			}
			digest := claudeDigestIn(t, tc.tool, out)
			command, _ := json.Marshal(input)
			if digest.Command != string(command) || digest.Tool != tc.tool || digest.SourceTruncated != tc.want {
				t.Fatalf("digest metadata: %+v", digest)
			}
		})
	}
}

func TestClaudeNativeExternalizedDisplayFields(t *testing.T) {
	big := strings.Repeat("real-shaped source line\n", 400)
	t.Run("Read", func(t *testing.T) {
		s := testStore(t)
		file := map[string]any{"filePath": "/repo/fake-known-secret.go", "content": big, "numLines": 400, "startLine": 7, "totalLines": 400}
		out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "text", "file": file}}, Result{})
		if err != nil {
			t.Fatal(err)
		}
		hook := out["hookSpecificOutput"].(map[string]any)
		if len(hook) != 2 || hook["hookEventName"] != "PostToolUse" {
			t.Fatalf("envelope: %v", hook)
		}
		updated := hook["updatedToolOutput"].(map[string]any)
		got := updated["file"].(map[string]any)
		digest := claudeDigestIn(t, "Read", out)
		if updated["type"] != "text" || got["numLines"] != 1 || got["startLine"] != 1 || got["totalLines"] != float64(400) || digest.ArtifactID == "" {
			t.Fatalf("Read display fields: %v", updated)
		}
		if strings.Contains(got["filePath"].(string), "fake-known-secret") || got["filePath"] == "" {
			t.Fatalf("Read must carry the sanitized original path: %v", got["filePath"])
		}
		if len(updated) != 2 || len(got) != 5 {
			t.Fatalf("Read keys: %v", updated)
		}
	})
	t.Run("Grep", func(t *testing.T) {
		s := testStore(t)
		response := map[string]any{"mode": "files_with_matches", "content": big, "numFiles": 3, "filenames": []string{"/repo/a.go"}, "numLines": 400, "appliedLimit": 250}
		out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolUse", Tool: "Grep", Response: response}, Result{})
		if err != nil {
			t.Fatal(err)
		}
		updated := out["hookSpecificOutput"].(map[string]any)["updatedToolOutput"].(map[string]any)
		filenames, isSlice := updated["filenames"].([]any)
		if updated["mode"] != "content" || !isSlice || len(filenames) != 0 || updated["numFiles"] != 0 || updated["numLines"] != 1 ||
			updated["appliedLimit"] != float64(250) || claudeDigestIn(t, "Grep", out).ArtifactID == "" || len(updated) != 6 {
			t.Fatalf("Grep display fields: %v", updated)
		}
	})
	t.Run("Glob", func(t *testing.T) {
		s := testStore(t)
		response := map[string]any{"filenames": strings.Split(big, "\n"), "numFiles": 400, "durationMs": 2, "truncated": false}
		out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolUse", Tool: "Glob", Response: response}, Result{})
		if err != nil {
			t.Fatal(err)
		}
		updated := out["hookSpecificOutput"].(map[string]any)["updatedToolOutput"].(map[string]any)
		filenames := updated["filenames"].([]any)
		if len(filenames) != 1 || updated["numFiles"] != 1 || updated["truncated"] != true || updated["durationMs"] != float64(2) ||
			claudeDigestIn(t, "Glob", out).ArtifactID == "" || len(updated) != 4 {
			t.Fatalf("Glob display fields: %v", updated)
		}
	})
}

func TestClaudeNativeSmallResults(t *testing.T) {
	t.Run("clean results are no-ops that still record one event", func(t *testing.T) {
		for tool, response := range map[string]map[string]any{
			"Grep": {"mode": "content", "content": "a.go:1:match\n", "numFiles": 1, "filenames": []string{"a.go"}},
			"Glob": {"filenames": []string{"a.go"}, "numFiles": 1, "truncated": false},
		} {
			s := testStore(t)
			out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolUse", Tool: HookTool(tool), Response: response}, Result{})
			if err != nil || out == nil || len(out) != 0 {
				t.Fatalf("%s: out=%v err=%v", tool, out, err)
			}
			events := claudeStoredEvents(t, s)
			if len(events) != 1 || events[0].Externalized || events[0].Tool != tool {
				t.Fatalf("%s: events=%+v", tool, events)
			}
		}
	})
	t.Run("redacted results keep their native shape", func(t *testing.T) {
		for tool, response := range map[string]map[string]any{
			"Grep": {"mode": "content", "content": "a.go:1:token fake-known-secret\n", "numFiles": 1, "filenames": []string{"a.go"}, "numLines": 1},
			"Glob": {"filenames": []string{"fake-known-secret.txt"}, "numFiles": 1, "truncated": false},
		} {
			s := testStore(t)
			out, err := ClaudePostToolResult(s, ClaudeHookInput{Event: "PostToolUse", Tool: HookTool(tool), Response: response}, Result{})
			if err != nil {
				t.Fatalf("%s: %v", tool, err)
			}
			encoded, _ := json.Marshal(out)
			updated := out["hookSpecificOutput"].(map[string]any)["updatedToolOutput"].(map[string]any)
			if strings.Contains(string(encoded), "fake-known-secret") || strings.Contains(string(encoded), "artifact_id") || len(updated) != len(response) {
				t.Fatalf("%s: %s", tool, encoded)
			}
			for key := range response {
				if _, ok := updated[key]; !ok {
					t.Fatalf("%s: native key %q lost: %s", tool, key, encoded)
				}
			}
			if events := claudeStoredEvents(t, s); len(events) != 1 || events[0].Externalized {
				t.Fatalf("%s: events=%+v", tool, events)
			}
		}
	})
}
