package toolresultacp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/toolresult"
)

// Explicit opt-in: uses existing local Devin OAuth and makes real model calls.
func TestNativeDevinBudgetBoundary(t *testing.T) {
	if os.Getenv("EE_TEST_NATIVE_DEVIN") != "1" {
		t.Skip("set EE_TEST_NATIVE_DEVIN=1 for authenticated native tool proof")
	}
	t.Setenv("DEVIN_PERMISSION_MODE", "bypass")
	dir, err := os.MkdirTemp("", "dev778-native-production-")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("native evidence directory:", dir)
	content := strings.Repeat("archive-shaped native file log row\n", 4000)
	if err = os.WriteFile(filepath.Join(dir, "large.txt"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	script := "import sys\nprint('archive-shaped execution output row\\n' * 30000, end='')\nprint('FAILURE_MARKER_778', file=sys.stderr)\nsys.exit(7)\n"
	if err = os.WriteFile(filepath.Join(dir, "emit.py"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	config := `{"read_config_from":{"claude":false,"cursor":false,"windsurf":false},"subagents_enabled":false,"notify":"never","auto_update":false}`
	if err = os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := toolresult.New(filepath.Join(dir, "artifacts"), toolresult.Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	inputR, inputW := io.Pipe()
	outputR, outputW := io.Pipe()
	defer inputR.Close()
	defer inputW.Close()
	defer outputR.Close()
	defer outputW.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Command: []string{"devin", "--config", filepath.Join(dir, "config.json"), "acp"}, Store: store, Metadata: toolresult.Result{TaskID: "DEV-778", AgentID: "boundary-proof"}, In: inputR, Out: outputW, Dir: dir})
	}()
	go func() { <-ctx.Done(); _ = outputW.Close(); _ = inputW.Close() }()
	enc := json.NewEncoder(inputW)
	dec := json.NewDecoder(outputR)
	send := func(v any) {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"terminal": true, "fs": map[string]any{"readTextFile": true}}}})
	var final strings.Builder
	reads := 0
Loop:
	for {
		var m wireMessage
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		method := field(m, "method")
		switch {
		case method == "fs/read_text_file":
			reads++
			params := decodeParams(m)
			var path string
			_ = json.Unmarshal(params["path"], &path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(m["id"]), "result": map[string]string{"content": string(data)}})
		case method == "session/request_permission":
			params := decodeParams(m)
			var opts []struct {
				OptionID string `json:"optionId"`
				Kind     string `json:"kind"`
			}
			_ = json.Unmarshal(params["options"], &opts)
			selected := ""
			for _, opt := range opts {
				if opt.Kind == "allow_once" {
					selected = opt.OptionID
					break
				}
			}
			if selected == "" {
				t.Fatal("no allow_once permission")
			}
			send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(m["id"]), "result": map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": selected}}})
		case method == "session/update":
			params := decodeParams(m)
			var update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			_ = json.Unmarshal(params["update"], &update)
			if update.SessionUpdate == "agent_message_chunk" {
				final.WriteString(update.Content.Text)
			}
		case method == "" && string(m["id"]) == "1":
			if m["error"] != nil {
				t.Fatalf("initialize: %s", m["error"])
			}
			send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "session/new", "params": map[string]any{"cwd": dir, "mcpServers": []any{}}})
		case method == "" && string(m["id"]) == "2":
			if m["error"] != nil {
				t.Fatalf("session: %s", m["error"])
			}
			var result struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(m["result"], &result)
			prompt := fmt.Sprintf("Boundary validation, no project work. Perform these 4 native tool calls sequentially: read %s/large.txt; read the SAME file again despite duplication; exec exactly python3 %s/emit.py; repeat the SAME exec despite exit 7. Do not read emit.py, do not delegate, do not use shell read commands. Each result should be a digest. Final answer report whether each digest was duplicate and list the exact exec exit codes. Do not retrieve artifact content or inspect other files.", dir, dir)
			send(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "session/prompt", "params": map[string]any{"sessionId": result.SessionID, "prompt": []map[string]string{{"type": "text", "text": prompt}}}})
		case method == "" && string(m["id"]) == "3":
			if m["error"] != nil {
				t.Fatalf("prompt: %s", m["error"])
			}
			break Loop
		}
	}
	_ = inputW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("proxy shutdown timeout")
	}
	stats, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native reads=%d stats=%+v model=%s", reads, stats, final.String())
	if reads < 2 || stats.ExternalizedCount < 4 || stats.DuplicateCount < 2 || !strings.Contains(final.String(), "7") {
		t.Fatal("native proof incomplete")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "artifacts", "events"))
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, "artifacts", "events", entry.Name()))
		var event toolresult.Event
		_ = json.Unmarshal(data, &event)
		if event.Tool == "exec" && event.ExitCode != nil && *event.ExitCode == 7 {
			failed++
		}
		if event.ContextBytes > 1024 {
			t.Fatal("unbounded model callback")
		}
	}
	if failed < 2 {
		t.Fatal("exec failures not preserved")
	}
}
