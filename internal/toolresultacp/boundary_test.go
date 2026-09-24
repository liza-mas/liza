package toolresultacp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/toolresult"
)

func testProxy(t *testing.T) (*proxy, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	store, err := toolresult.New(t.TempDir(), toolresult.Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent, host := &bytes.Buffer{}, &bytes.Buffer{}
	return &proxy{config: Config{Store: store, Dir: t.TempDir()}, ctx: context.Background(), agent: &wireWriter{writer: agent}, host: &wireWriter{writer: host}, reads: map[string]toolresult.Result{}, sessions: map[string]string{}, newSessions: map[string]string{}, terminals: map[string]*terminal{}, cancelled: map[string]bool{}}, agent, host
}

func TestProxyPreservesDisabledCapabilitiesAndPermissions(t *testing.T) {
	p, agent, host := testProxy(t)
	init := wireMessage{"jsonrpc": raw("2.0"), "id": raw(1), "method": raw("initialize"), "params": raw(map[string]any{"clientCapabilities": map[string]any{"terminal": false, "fs": map[string]bool{"readTextFile": false}}})}
	if err := p.fromHost(init); err != nil {
		t.Fatal(err)
	}
	if p.terminalEnabled {
		t.Fatal("terminal privilege escalated")
	}
	var forwarded wireMessage
	if err := json.Unmarshal(bytes.TrimSpace(agent.Bytes()), &forwarded); err != nil {
		t.Fatal(err)
	}
	if string(forwarded["params"]) != string(init["params"]) {
		t.Fatal("advertised capability changed")
	}
	if _, err := p.terminalCall("terminal/create", raw(terminalRequest{Command: "printf forbidden", Cwd: p.config.Dir})); err == nil {
		t.Fatal("disabled terminal executed")
	}
	permission := wireMessage{"jsonrpc": raw("2.0"), "id": raw("permission-1"), "method": raw("session/request_permission"), "params": raw(map[string]any{"options": []string{"reject"}})}
	if err := p.fromAgent(permission); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(host.Bytes(), []byte("reject")) {
		t.Fatal("permission callback consumed")
	}
}

func TestProxyReadFailureAndSmallContentRemainUnchanged(t *testing.T) {
	p, agent, _ := testProxy(t)
	req := wireMessage{"jsonrpc": raw("2.0"), "id": raw("read"), "method": raw("fs/read_text_file"), "params": raw(map[string]any{"path": "/native/file", "line": 19, "limit": 3, "sessionId": "session"})}
	if err := p.fromAgent(req); err != nil {
		t.Fatal(err)
	}
	failure := wireMessage{"jsonrpc": raw("2.0"), "id": raw("read"), "error": raw(map[string]any{"code": -32000, "message": "denied"})}
	if err := p.fromHost(failure); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(agent.Bytes(), []byte("denied")) || bytes.Contains(agent.Bytes(), []byte("result")) {
		t.Fatal("read rejection was bypassed")
	}
	agent.Reset()
	if err := p.fromAgent(req); err != nil {
		t.Fatal(err)
	}
	response := wireMessage{"jsonrpc": raw("2.0"), "id": raw("read"), "result": raw(map[string]any{"content": "line19\nline20\nline21\n", "_meta": map[string]string{"host": "retained"}})}
	if err := p.fromHost(response); err != nil {
		t.Fatal(err)
	}
	var got wireMessage
	if err := json.Unmarshal(bytes.TrimSpace(agent.Bytes()), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["result"]) != string(response["result"]) {
		t.Fatalf("small read changed: %s", agent.String())
	}
}

func TestProxyReadStoreFailureDoesNotLeakRawOutput(t *testing.T) {
	p, agent, _ := testProxy(t)
	req := wireMessage{"id": raw("read"), "method": raw("fs/read_text_file"), "params": raw(map[string]string{"path": "/file"})}
	if err := p.fromAgent(req); err != nil {
		t.Fatal(err)
	}
	// Remove the backing root after successful Store construction.
	root := filepath.Join(t.TempDir(), "removed")
	store, err := toolresult.New(root, toolresult.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.config.Store = store
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := p.fromHost(wireMessage{"id": raw("read"), "result": raw(map[string]string{"content": strings.Repeat("RAW_MUST_NOT_ESCAPE", 5000)})}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(agent.Bytes(), []byte("RAW_MUST_NOT_ESCAPE")) || !bytes.Contains(agent.Bytes(), []byte("error")) {
		t.Fatalf("fail-open result: %s", agent.String())
	}
}

func TestProxySessionCancellationAndTerminalIsolation(t *testing.T) {
	p, _, _ := testProxy(t)
	p.terminalEnabled = true
	created, err := p.terminalCall("terminal/create", raw(terminalRequest{SessionID: "owner", Command: "sleep 30", Cwd: p.config.Dir}))
	if err != nil {
		t.Fatal(err)
	}
	id := created.(map[string]string)["terminalId"]
	term := p.terminals[id]
	defer term.close()
	if _, err := p.terminalCall("terminal/output", raw(terminalRequest{SessionID: "other", TerminalID: id})); err == nil {
		t.Fatal("terminal crossed sessions")
	}
	if err := p.fromHost(wireMessage{"method": raw("session/cancel"), "params": raw(map[string]string{"sessionId": "owner"})}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-term.done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel left terminal alive")
	}
	if _, err := p.terminalCall("terminal/create", raw(terminalRequest{SessionID: "owner", Command: "printf late", Cwd: p.config.Dir})); err == nil {
		t.Fatal("cancelled turn started terminal")
	}
}

func TestProxyReportsNativeAgentCrash(t *testing.T) {
	store, err := toolresult.New(t.TempDir(), toolresult.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input, writer := io.Pipe()
	defer writer.Close()
	defer input.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = Run(ctx, Config{Command: []string{"sh", "-c", "exit 13"}, Store: store, In: input, Out: io.Discard, Dir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "unsuccessfully") {
		t.Fatalf("native crash hidden: %v", err)
	}
}
