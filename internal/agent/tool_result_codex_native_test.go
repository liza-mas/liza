package agent

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/toolresult"
)

// The native provider is real; only its remote model is replaced with a local
// Responses fixture. This checks what the model actually receives, not logs.
func TestCodexNativeToolResultBoundary(t *testing.T) {
	engine := os.Getenv("TOOL_RESULT_NATIVE_ENGINE")
	if engine == "" {
		t.Skip("set TOOL_RESULT_NATIVE_ENGINE to the current engine executable")
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
	source := filepath.Join(root, "archive-output.txt")
	if err = os.WriteFile(source, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	launch, err := prepareCodexToolResultLaunch(root, engine, codex)
	if err != nil {
		t.Fatal(err)
	}
	command := "cat " + codexToolResultShellQuote(source)
	var mu sync.Mutex
	var requests []map[string]json.RawMessage
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			mu.Lock()
			handlerErr = err
			mu.Unlock()
			http.Error(w, "bad request", 400)
			return
		}
		mu.Lock()
		requests = append(requests, request)
		n := len(requests)
		mu.Unlock()
		var item map[string]any
		name := "exec_command"
		args := map[string]any{"cmd": "printf 'ordinary bounded read\\n'", "max_output_tokens": 2000}
		switch n {
		case 1:
		case 2:
			args = map[string]any{"cmd": command + "; sleep 1", "max_output_tokens": 2000, "yield_time_ms": 100}
		case 3:
			var input []struct {
				Type   string `json:"type"`
				Output string `json:"output"`
			}
			_ = json.Unmarshal(request["input"], &input)
			session := ""
			for _, in := range input {
				if in.Type == "function_call_output" {
					if m := regexp.MustCompile(`session ID ([0-9]+)`).FindStringSubmatch(in.Output); len(m) == 2 {
						session = m[1]
					}
				}
			}
			id, _ := strconv.Atoi(session)
			if id == 0 {
				mu.Lock()
				handlerErr = fmt.Errorf("no streaming session id")
				mu.Unlock()
			}
			name = "write_stdin"
			args = map[string]any{"session_id": id, "chars": "", "yield_time_ms": 2000, "max_output_tokens": 2000}
		case 4:
			args = map[string]any{"cmd": command, "max_output_tokens": 2000}
		case 5:
			args = map[string]any{"cmd": command + "; exit 7", "max_output_tokens": 2000}
		}
		if n <= 5 {
			encoded, _ := json.Marshal(args)
			item = map[string]any{"type": "function_call", "id": fmt.Sprint("fc_", n), "call_id": fmt.Sprint("call_", n), "name": name, "arguments": string(encoded)}
		} else {
			item = map[string]any{"type": "message", "id": "msg_done", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "native boundary checked", "annotations": []any{}}}}
		}
		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": fmt.Sprint("resp_", n)}},
			{"type": "response.output_item.added", "output_index": 0, "item": item},
			{"type": "response.output_item.done", "output_index": 0, "item": item},
			{"type": "response.completed", "response": map[string]any{"id": fmt.Sprint("resp_", n), "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			encoded, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], encoded)
		}
	}))
	defer server.Close()
	counter := filepath.Join(root, "existing-hooks.txt")
	config := fmt.Sprintf("model=\"gpt-5.5\"\nmodel_provider=\"fixture\"\napproval_policy=\"never\"\nsandbox_mode=\"workspace-write\"\n[model_providers.fixture]\nname=\"Fixture\"\nbase_url=%q\nwire_api=\"responses\"\nsupports_websockets=false\nrequest_max_retries=0\nstream_max_retries=0\n[features]\ncode_mode=false\nplugins=false\nenable_request_compression=false\n[[hooks.PreToolUse]]\nmatcher=\"Bash\"\n[[hooks.PreToolUse.hooks]]\ntype=\"command\"\ncommand=%q\n", server.URL+"/v1", "echo inline >> "+codexToolResultShellQuote(counter))
	if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	hooks, _ := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "echo json >> " + codexToolResultShellQuote(counter)}}}}}})
	if err = os.WriteFile(filepath.Join(home, "hooks.json"), hooks, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, launch.Wrapper, "exec", "--skip-git-repo-check", "--ignore-rules", "--ephemeral", "--json", "-C", root, "Exercise fixture tools")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home, brand.EnvName("TOOL_RESULT_THRESHOLD_BYTES")+"=2048", brand.LegacyEnvName("TOOL_RESULT_THRESHOLD_BYTES")+"=2048", brand.EnvName("TOOL_RESULT_DIGEST_BYTES")+"=1024", brand.LegacyEnvName("TOOL_RESULT_DIGEST_BYTES")+"=1024")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native Codex: %v: %s", err, output)
	}
	mu.Lock()
	defer mu.Unlock()
	if handlerErr != nil || len(requests) != 6 {
		t.Fatalf("requests=%d handler=%v output=%s", len(requests), handlerErr, output)
	}
	var final []struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Output string `json:"output"`
	}
	if err = json.Unmarshal(requests[5]["input"], &final); err != nil {
		t.Fatal(err)
	}
	var digests []toolresult.Digest
	for _, in := range final {
		if in.Type != "function_call_output" {
			continue
		}
		if strings.Contains(in.Output, "fake-dev778-native-token") {
			t.Fatalf("unsanitized secret reached model: call=%s bytes=%d prefix=%.300s provider=%.1800s", in.CallID, len(in.Output), in.Output, output)
		}
		switch in.CallID {
		case "call_1":
			if !strings.HasSuffix(in.Output, "ordinary bounded read\n") {
				t.Fatalf("small read changed: %q", in.Output)
			}
		case "call_2":
			if !strings.HasSuffix(in.Output, "Output:\n") {
				t.Fatalf("raw streaming output escaped: %q", in.Output)
			}
		default:
			start := strings.Index(in.Output, "{")
			if start < 0 {
				t.Fatalf("missing digest: %s", in.Output)
			}
			var d toolresult.Digest
			if err = json.Unmarshal([]byte(in.Output[start:]), &d); err != nil {
				t.Fatalf("invalid digest: %s", in.Output)
			}
			if len(in.Output[start:]) > 1024 {
				t.Fatal("digest exceeds budget")
			}
			digests = append(digests, d)
		}
	}
	if len(digests) != 3 || digests[0].Duplicate || !digests[1].Duplicate || !digests[2].Duplicate || digests[2].ExitCode == nil || *digests[2].ExitCode != 7 {
		t.Fatalf("metadata/dedup: %#v", digests)
	}
	full, err := os.ReadFile(digests[0].ArtifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) < len(payload)-100 || strings.Contains(string(full), "fake-dev778-native-token") {
		t.Fatal("artifact missing content or unsanitized")
	}
	for _, d := range digests {
		if d.ContentHash != digests[0].ContentHash {
			t.Fatal("same output got different identity")
		}
	}
	counts, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(counts), "inline\n") != 4 || strings.Count(string(counts), "json\n") != 4 {
		t.Fatalf("existing hooks lost: %s", counts)
	}
	t.Logf("native 0.154+ six model requests; original=%d artifact=%d; digests=3 duplicate=2 exit=7; both existing hooks invoked four times", len(payload), len(full))
}

func codexNativeEvidence(t *testing.T) string {
	t.Helper()
	path := os.Getenv("TOOL_RESULT_EVIDENCE_ARCHIVE")
	if path != "" {
		archive, err := zip.OpenReader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer archive.Close()
		for _, file := range archive.File {
			if !strings.HasSuffix(file.Name, "/coder-1-20260710-025708.txt") {
				continue
			}
			r, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(r)
			scanner.Buffer(make([]byte, 65536), 8<<20)
			for scanner.Scan() {
				var j struct {
					Params struct {
						Update struct {
							RawOutput struct {
								Output string `json:"formatted_output"`
							} `json:"rawOutput"`
						} `json:"update"`
					} `json:"params"`
				}
				if json.Unmarshal(scanner.Bytes(), &j) == nil && len(j.Params.Update.RawOutput.Output) > 32768 {
					_ = r.Close()
					return j.Params.Update.RawOutput.Output + "\nAuthorization: Bearer fake-dev778-native-token\n"
				}
			}
			_ = r.Close()
			if err = scanner.Err(); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatal("archive had no cited large native-output record")
	}
	return strings.Repeat("test failure: expected workspace output metadata; observed tool_call_update formatted_output\n", 1500) + "Authorization: Bearer fake-dev778-native-token\n"
}
