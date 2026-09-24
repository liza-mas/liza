package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
)

// This helper subprocess is an actual MCP stdio server with local fake data.
func TestCodexMCPFixtureProcess(t *testing.T) {
	if os.Getenv("CODEX_MCP_FIXTURE") != "1" {
		t.Skip("MCP fixture subprocess only")
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var j struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Protocol  string `json:"protocolVersion"`
				Arguments struct {
					Small bool `json:"small"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &j) != nil || len(j.ID) == 0 {
			continue
		}
		var result any = map[string]any{}
		switch j.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": j.Params.Protocol, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fixture", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "large", "description": "Return local fixture output", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"small": map[string]any{"type": "boolean"}}}}}}
		case "tools/call":
			text := "tiny MCP read"
			if !j.Params.Arguments.Small {
				data, err := os.ReadFile(os.Getenv("CODEX_MCP_FIXTURE_FILE"))
				if err != nil {
					os.Exit(3)
				}
				text = string(data)
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": false}
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": j.ID, "result": result})
	}
	os.Exit(0)
}

func TestCodexNativeMCPBoundary(t *testing.T) {
	engine := os.Getenv("TOOL_RESULT_NATIVE_ENGINE")
	if engine == "" {
		t.Skip("set TOOL_RESULT_NATIVE_ENGINE to current engine binary")
	}
	if info, err := os.Stat(engine); err != nil || info.Mode()&0111 == 0 {
		t.Fatalf("native engine executable unavailable: %s", engine)
	}
	codex, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	payload := codexNativeEvidence(t)
	source := filepath.Join(root, "mcp-source.txt")
	if err = os.WriteFile(source, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	launch, err := prepareCodexToolResultLaunch(root, engine, codex)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		mu.Lock()
		requests = append(requests, request)
		n := len(requests)
		mu.Unlock()
		var item map[string]any
		if n <= 3 {
			small := n == 2
			code := fmt.Sprintf("try { const r=await tools.mcp__fixture__large({small:%t}); text({rawReached:true,value:r}); } catch(e) { text({blocked:true,reason:String(e)}); }", small)
			item = map[string]any{"type": "custom_tool_call", "id": fmt.Sprint("c_", n), "call_id": fmt.Sprint("call_", n), "name": "exec", "input": code}
		} else {
			item = map[string]any{"type": "message", "id": "done", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "checked", "annotations": []any{}}}}
		}
		events := []map[string]any{{"type": "response.created", "response": map[string]any{"id": fmt.Sprint("r_", n)}}, {"type": "response.output_item.added", "output_index": 0, "item": item}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": map[string]any{"id": fmt.Sprint("r_", n), "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
		}
	}))
	defer server.Close()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("model=\"gpt-5.5\"\nmodel_provider=\"fixture\"\napproval_policy=\"never\"\nsandbox_mode=\"workspace-write\"\n[model_providers.fixture]\nname=\"Fixture\"\nbase_url=%q\nwire_api=\"responses\"\nsupports_websockets=false\nrequest_max_retries=0\nstream_max_retries=0\n[features]\ncode_mode=true\nplugins=false\nenable_request_compression=false\n[mcp_servers.fixture]\ncommand=%q\nargs=[\"-test.run=^TestCodexMCPFixtureProcess$\"]\ndefault_tools_approval_mode=\"approve\"\nenv={CODEX_MCP_FIXTURE=\"1\",CODEX_MCP_FIXTURE_FILE=%q}\n", server.URL+"/v1", testExecutable, source)
	if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, launch.Wrapper, "exec", "--skip-git-repo-check", "--ignore-rules", "--ephemeral", "--json", "-C", root, "Fixture")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home, brand.EnvName("TOOL_RESULT_THRESHOLD_BYTES")+"=2048", brand.LegacyEnvName("TOOL_RESULT_THRESHOLD_BYTES")+"=2048", brand.EnvName("TOOL_RESULT_DIGEST_BYTES")+"=1024", brand.LegacyEnvName("TOOL_RESULT_DIGEST_BYTES")+"=1024")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native MCP: %v %s", err, output)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("requests=%d output=%s", len(requests), output)
	}
	var final []struct {
		Type   string          `json:"type"`
		CallID string          `json:"call_id"`
		Output json.RawMessage `json:"output"`
	}
	if err = json.Unmarshal(requests[3]["input"], &final); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, in := range final {
		if in.Type != "custom_tool_call_output" {
			continue
		}
		count++
		var pieces []struct {
			Text string `json:"text"`
		}
		if err = json.Unmarshal(in.Output, &pieces); err != nil {
			t.Fatal(err)
		}
		text := ""
		for _, piece := range pieces {
			text += piece.Text
		}
		if in.CallID == "call_2" {
			if !strings.Contains(text, "tiny MCP read") || strings.Contains(text, `"blocked":true`) {
				t.Fatalf("small MCP changed: %s", text)
			}
			continue
		}
		if strings.Contains(text, "fake-dev778-native-token") || strings.Contains(text, `"rawReached":true`) || !strings.Contains(text, `"blocked":true`) || !strings.Contains(text, "artifact_path") {
			t.Fatalf("nested large result not withheld: bytes=%d prefix=%.300s", len(text), text)
		}
		if in.CallID == "call_3" && !strings.Contains(text, `\"duplicate\":true`) {
			t.Fatalf("repeat not deduplicated: %s", text)
		}
	}
	if count != 3 {
		t.Fatalf("expected three nested outputs, got %d", count)
	}
	t.Logf("native code-mode MCP: original=%d, large+duplicate promises rejected with sanitized artifact digest; small read unchanged", len(payload))
}
