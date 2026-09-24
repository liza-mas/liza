package toolresultacp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/toolresult"
)

func TestProxyRealChildFilesystemResponseBoundary(t *testing.T) {
	store, err := toolresult.New(t.TempDir(), toolresult.Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer inR.Close()
	defer inW.Close()
	defer outR.Close()
	defer outW.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Command: []string{os.Args[0], "-test.run=^TestACPAdapterHelper$"}, Store: store, Metadata: toolresult.Result{TaskID: "DEV-778"}, In: inR, Out: outW, Dir: t.TempDir()})
	}()
	enc := json.NewEncoder(inW)
	dec := json.NewDecoder(outR)
	if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"clientCapabilities": map[string]any{"terminal": true, "fs": map[string]bool{"readTextFile": true}}}}); err != nil {
		t.Fatal(err)
	}
	var callback wireMessage
	if err := dec.Decode(&callback); err != nil {
		t.Fatal(err)
	}
	if field(callback, "method") != "fs/read_text_file" {
		t.Fatalf("callback not forwarded: %s", raw(callback))
	}
	content := strings.Repeat("real archive output row\n", 5000)
	if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "read-1", "result": map[string]string{"content": content}}); err != nil {
		t.Fatal(err)
	}
	var observed wireMessage
	if err := dec.Decode(&observed); err != nil {
		t.Fatal(err)
	}
	var params map[string]string
	if err := json.Unmarshal(observed["params"], &params); err != nil {
		t.Fatal(err)
	}
	var digest toolresult.Digest
	if err := json.Unmarshal([]byte(params["observed"]), &digest); err != nil {
		t.Fatal(err)
	}
	if digest.OriginalBytes != len(content) || len(params["observed"]) > 1024 {
		t.Fatalf("unbounded callback: %+v", digest)
	}
	_ = inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("proxy did not finish")
	}
}

func TestACPAdapterHelper(t *testing.T) {
	// Only the subprocess test invocation has no normal parent test runner flags.
	if len(os.Args) != 2 || os.Args[1] != "-test.run=^TestACPAdapterHelper$" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() {
		os.Exit(2)
	}
	var init wireMessage
	_ = json.Unmarshal(scanner.Bytes(), &init)
	params := decodeParams(init)
	var caps map[string]any
	_ = json.Unmarshal(params["clientCapabilities"], &caps)
	if caps["terminal"] != true {
		os.Exit(3)
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "read-1", "method": "fs/read_text_file", "params": map[string]any{"sessionId": "session", "path": "/native.txt", "line": 7, "limit": 5000}})
	if !scanner.Scan() {
		os.Exit(4)
	}
	var reply struct {
		Result struct {
			Content string `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(scanner.Bytes(), &reply) != nil {
		os.Exit(5)
	}
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "probe/observed", "params": map[string]string{"observed": reply.Result.Content}})
	// Avoid test-runner PASS on protocol stdout.
	os.Exit(0)
}
