package toolresultacp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	safeTerminalFailure = "Terminal operation failed safely"
	unknownTerminalOp   = Method("terminal/frobnicate")
)

func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell")
	}
}

func decodeWire(t *testing.T, buf *bytes.Buffer) []wireMessage {
	t.Helper()
	var out []wireMessage
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m wireMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func requireForwardedUnchanged(t *testing.T, sink *bytes.Buffer, sent wireMessage) {
	t.Helper()
	got := decodeWire(t, sink)
	if len(got) != 1 {
		t.Fatalf("expected one forwarded message, got %d: %s", len(got), sink.String())
	}
	if len(got[0]) != len(sent) {
		t.Fatalf("forwarded fields changed: %s", sink.String())
	}
	for k, v := range sent {
		if string(got[0][k]) != string(v) {
			t.Fatalf("forwarded %q changed: %s", k, sink.String())
		}
	}
	sink.Reset()
}

// agentCall sends an agent request through fromAgent and returns the reply the
// terminal worker writes back to the agent.
func agentCall(t *testing.T, p *proxy, agent *bytes.Buffer, id int, method Method, params any) wireMessage {
	t.Helper()
	agent.Reset()
	req := wireMessage{"jsonrpc": raw("2.0"), "id": raw(id), "method": raw(string(method)), "params": raw(params)}
	if err := p.fromAgent(req); err != nil {
		t.Fatal(err)
	}
	p.workers.Wait()
	replies := decodeWire(t, agent)
	if len(replies) != 1 || string(replies[0]["id"]) != string(req["id"]) {
		t.Fatalf("expected one reply to %s: %s", method, agent.String())
	}
	agent.Reset()
	return replies[0]
}

func requireWireFailure(t *testing.T, reply wireMessage) {
	t.Helper()
	var e struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if reply["result"] != nil || json.Unmarshal(reply["error"], &e) != nil || e.Code != -32603 || e.Message != safeTerminalFailure {
		t.Fatalf("terminal failure not mapped to the safe wire error: %v", reply)
	}
}

func TestMethodConstantsMatchWireNames(t *testing.T) {
	for want, got := range map[string]Method{
		"initialize":             MethodInitialize,
		"session/new":            MethodSessionNew,
		"session/prompt":         MethodSessionPrompt,
		"session/cancel":         MethodSessionCancel,
		"fs/read_text_file":      MethodFSReadTextFile,
		"terminal/create":        MethodTerminalCreate,
		"terminal/output":        MethodTerminalOutput,
		"terminal/wait_for_exit": MethodTerminalWait,
		"terminal/kill":          MethodTerminalKill,
		"terminal/release":       MethodTerminalRelease,
	} {
		if string(got) != want {
			t.Errorf("method constant %q != wire name %q", got, want)
		}
	}
}

func TestProxyForwardsEmptyAndUnknownMethodsUnchanged(t *testing.T) {
	p, agent, host := testProxy(t)
	p.terminalEnabled = true
	fromHost := []wireMessage{
		{"jsonrpc": raw("2.0"), "id": raw("permission-1"), "result": raw(map[string]string{"outcome": "reject"})},
		{"jsonrpc": raw("2.0"), "id": raw(3), "method": raw("custom/ping"), "params": raw(map[string]int{"n": 1})},
		{"jsonrpc": raw("2.0"), "id": raw(4), "method": raw("terminal/create"), "params": raw(terminalRequest{Command: "true"})},
	}
	for _, m := range fromHost {
		if err := p.fromHost(m); err != nil {
			t.Fatal(err)
		}
		requireForwardedUnchanged(t, agent, m)
	}
	fromAgent := []wireMessage{
		{"jsonrpc": raw("2.0"), "id": raw(5), "result": raw(map[string]string{"stopReason": "end_turn"})},
		{"jsonrpc": raw("2.0"), "id": raw(6), "method": raw("custom/ping"), "params": raw(map[string]int{"n": 2})},
		{"jsonrpc": raw("2.0"), "id": raw(7), "method": raw("fs/write_text_file"), "params": raw(map[string]string{"path": "/x"})},
		{"jsonrpc": raw("2.0"), "method": raw("session/update"), "params": raw(map[string]string{"sessionId": "s"})},
	}
	for _, m := range fromAgent {
		if err := p.fromAgent(m); err != nil {
			t.Fatal(err)
		}
		requireForwardedUnchanged(t, host, m)
	}
	p.workers.Wait()
	if len(p.terminals) != 0 || len(p.reads) != 0 || len(p.sessions) != 0 || len(p.newSessions) != 0 {
		t.Fatal("unknown or empty methods changed proxy state")
	}
}

func TestProxyTerminalNotificationsWithoutIDAreForwardedNotHandled(t *testing.T) {
	p, agent, host := testProxy(t)
	p.terminalEnabled = true
	for _, method := range []Method{MethodTerminalCreate, MethodTerminalOutput, MethodTerminalRelease, unknownTerminalOp} {
		note := wireMessage{"jsonrpc": raw("2.0"), "method": raw(string(method)), "params": raw(terminalRequest{SessionID: "s", Command: "true"})}
		if err := p.fromAgent(note); err != nil {
			t.Fatal(err)
		}
		requireForwardedUnchanged(t, host, note)
	}
	p.workers.Wait()
	if len(p.terminals) != 0 || agent.Len() != 0 {
		t.Fatalf("terminal notification was handled: terminals=%d agent=%q", len(p.terminals), agent.String())
	}
}

func TestProxyNewSessionResponseRegistersSessionAndForwards(t *testing.T) {
	p, agent, host := testProxy(t)
	if err := p.fromHost(wireMessage{"jsonrpc": raw("2.0"), "id": raw(7), "method": raw("session/new"), "params": raw(map[string]string{"cwd": "/work"})}); err != nil {
		t.Fatal(err)
	}
	if p.newSessions["7"] != "/work" {
		t.Fatalf("pending session not recorded: %v", p.newSessions)
	}
	agent.Reset()
	response := wireMessage{"jsonrpc": raw("2.0"), "id": raw(7), "result": raw(map[string]string{"sessionId": "s1"})}
	if err := p.fromAgent(response); err != nil {
		t.Fatal(err)
	}
	if p.sessions["s1"] != "/work" || len(p.newSessions) != 0 {
		t.Fatalf("session not registered: sessions=%v pending=%v", p.sessions, p.newSessions)
	}
	requireForwardedUnchanged(t, host, response)
}

func TestTerminalCallUnknownMethodKeepsCheckOrder(t *testing.T) {
	requirePOSIXShell(t)
	p, _, _ := testProxy(t)
	malformed := json.RawMessage(`"not an object"`)
	valid := func(session, terminalID string) json.RawMessage {
		return raw(terminalRequest{SessionID: session, TerminalID: terminalID})
	}
	wantErr := func(label string, params json.RawMessage, want string) {
		t.Helper()
		if _, err := p.terminalCall(unknownTerminalOp, params); err == nil || err.Error() != want {
			t.Fatalf("%s: got %v, want %q", label, err, want)
		}
	}

	wantErr("malformed params before capability", malformed, "invalid terminal arguments")
	wantErr("disabled capability before terminal lookup", valid("owner", "ee-terminal-1"), "terminal capability is disabled")

	p.terminalEnabled = true
	wantErr("malformed params when enabled", malformed, "invalid terminal arguments")
	wantErr("missing terminal", valid("owner", "ee-terminal-404"), "unknown terminal")

	created, err := p.terminalCall(MethodTerminalCreate, raw(terminalRequest{SessionID: "owner", Command: "sleep 30", Cwd: p.config.Dir, TerminalID: "ee-terminal-404"}))
	if err != nil {
		t.Fatalf("create must not look up its terminal id: %v", err)
	}
	id := created.(map[string]string)["terminalId"]
	defer p.terminals[id].close()

	wantErr("wrong-session terminal", valid("other", id), "unknown terminal")
	wantErr("existing valid terminal", valid("owner", id), "unsupported terminal operation")

	p.terminalEnabled = false
	wantErr("capability disabled again with an existing terminal", valid("owner", id), "terminal capability is disabled")
}

func TestProxyUnknownTerminalRequestMapsToSafeWireError(t *testing.T) {
	requirePOSIXShell(t)
	p, agent, host := testProxy(t)
	p.terminalEnabled = true
	created := agentCall(t, p, agent, 1, MethodTerminalCreate, terminalRequest{SessionID: "owner", Command: "sleep 30", Cwd: p.config.Dir})
	var made map[string]string
	if err := json.Unmarshal(created["result"], &made); err != nil || made["terminalId"] == "" {
		t.Fatalf("setup terminal not created: %v", created)
	}
	defer p.terminals[made["terminalId"]].close()

	for i, tc := range []struct {
		name   string
		params any
		on     bool
	}{
		{"malformed params", "not an object", true},
		{"disabled capability", terminalRequest{SessionID: "owner", TerminalID: made["terminalId"]}, false},
		{"missing terminal", terminalRequest{SessionID: "owner", TerminalID: "ee-terminal-404"}, true},
		{"wrong session", terminalRequest{SessionID: "other", TerminalID: made["terminalId"]}, true},
		{"existing terminal", terminalRequest{SessionID: "owner", TerminalID: made["terminalId"]}, true},
	} {
		p.terminalEnabled = tc.on
		reply := agentCall(t, p, agent, 10+i, unknownTerminalOp, tc.params)
		if string(reply["jsonrpc"]) != `"2.0"` {
			t.Errorf("%s: reply is not JSON-RPC 2.0: %v", tc.name, reply)
		}
		requireWireFailure(t, reply)
	}
	if host.Len() != 0 {
		t.Fatalf("terminal-prefixed request leaked to the host: %s", host.String())
	}
}

func TestProxyHandlesAllTenMethods(t *testing.T) {
	requirePOSIXShell(t)
	p, agent, host := testProxy(t)
	dir := t.TempDir()

	// initialize: terminal capability is read, request forwarded.
	if err := p.fromHost(wireMessage{"jsonrpc": raw("2.0"), "id": raw(1), "method": raw(string(MethodInitialize)), "params": raw(map[string]any{"clientCapabilities": map[string]bool{"terminal": true}})}); err != nil {
		t.Fatal(err)
	}
	if !p.terminalEnabled || agent.Len() == 0 {
		t.Fatal("initialize not handled and forwarded")
	}

	// session/new: cwd remembered until the agent answers.
	if err := p.fromHost(wireMessage{"jsonrpc": raw("2.0"), "id": raw(2), "method": raw(string(MethodSessionNew)), "params": raw(map[string]string{"cwd": dir})}); err != nil {
		t.Fatal(err)
	}
	if err := p.fromAgent(wireMessage{"jsonrpc": raw("2.0"), "id": raw(2), "result": raw(map[string]string{"sessionId": "s1"})}); err != nil {
		t.Fatal(err)
	}
	if p.sessions["s1"] != dir {
		t.Fatalf("session cwd not tracked: %v", p.sessions)
	}

	// session/prompt: clears an earlier cancellation of the session.
	p.cancelled["s1"] = true
	if err := p.fromHost(wireMessage{"jsonrpc": raw("2.0"), "id": raw(3), "method": raw(string(MethodSessionPrompt)), "params": raw(map[string]string{"sessionId": "s1"})}); err != nil {
		t.Fatal(err)
	}
	if p.cancelled["s1"] {
		t.Fatal("prompt did not clear cancellation")
	}

	// fs/read_text_file: the pending read is registered, then consumed by the response.
	if err := p.fromAgent(wireMessage{"jsonrpc": raw("2.0"), "id": raw("r1"), "method": raw(string(MethodFSReadTextFile)), "params": raw(map[string]string{"sessionId": "s1", "path": "/f"})}); err != nil {
		t.Fatal(err)
	}
	if meta, ok := p.reads[`"r1"`]; !ok || meta.Tool != "read" || meta.SessionID != "s1" {
		t.Fatalf("read not registered: %v", p.reads)
	}
	if err := p.fromHost(wireMessage{"jsonrpc": raw("2.0"), "id": raw("r1"), "result": raw(map[string]string{"content": "small"})}); err != nil {
		t.Fatal(err)
	}
	if len(p.reads) != 0 {
		t.Fatal("read response did not consume the pending read")
	}
	host.Reset()
	agent.Reset()

	// terminal/create takes its directory from the session; wait_for_exit, output, release follow.
	created := agentCall(t, p, agent, 20, MethodTerminalCreate, terminalRequest{SessionID: "s1", Command: "pwd -P"})
	var made map[string]string
	if err := json.Unmarshal(created["result"], &made); err != nil || made["terminalId"] == "" {
		t.Fatalf("create failed: %v", created)
	}
	id := made["terminalId"]
	waited := agentCall(t, p, agent, 21, MethodTerminalWait, terminalRequest{SessionID: "s1", TerminalID: id})
	var status terminalExit
	if err := json.Unmarshal(waited["result"], &status); err != nil || status.ExitCode == nil || *status.ExitCode != 0 {
		t.Fatalf("wait_for_exit did not report exit 0: %v", waited)
	}
	out := agentCall(t, p, agent, 22, MethodTerminalOutput, terminalRequest{SessionID: "s1", TerminalID: id})
	var output terminalOutput
	if err := json.Unmarshal(out["result"], &output); err != nil {
		t.Fatal(err)
	}
	gotDir, _ := filepath.EvalSymlinks(strings.TrimSpace(output.Output))
	wantDir, _ := filepath.EvalSymlinks(dir)
	if gotDir != wantDir {
		t.Fatalf("terminal did not start in the session cwd: %q", output.Output)
	}
	if reply := agentCall(t, p, agent, 23, MethodTerminalRelease, terminalRequest{SessionID: "s1", TerminalID: id}); reply["error"] != nil || len(p.terminals) != 0 {
		t.Fatalf("release failed: %v", reply)
	}

	// terminal/kill ends a running terminal.
	created = agentCall(t, p, agent, 24, MethodTerminalCreate, terminalRequest{SessionID: "s1", Command: "sleep 30"})
	if err := json.Unmarshal(created["result"], &made); err != nil {
		t.Fatal(err)
	}
	killed := p.terminals[made["terminalId"]]
	if reply := agentCall(t, p, agent, 25, MethodTerminalKill, terminalRequest{SessionID: "s1", TerminalID: made["terminalId"]}); reply["error"] != nil {
		t.Fatalf("kill failed: %v", reply)
	}
	select {
	case <-killed.done:
	case <-time.After(3 * time.Second):
		t.Fatal("kill left terminal alive")
	}
	killed.close()

	// session/cancel kills the session's terminals and marks it cancelled.
	created = agentCall(t, p, agent, 26, MethodTerminalCreate, terminalRequest{SessionID: "s1", Command: "sleep 30"})
	if err := json.Unmarshal(created["result"], &made); err != nil {
		t.Fatal(err)
	}
	running := p.terminals[made["terminalId"]]
	defer running.close()
	if err := p.fromHost(wireMessage{"jsonrpc": raw("2.0"), "method": raw(string(MethodSessionCancel)), "params": raw(map[string]string{"sessionId": "s1"})}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-running.done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel left terminal alive")
	}
	if !p.cancelled["s1"] {
		t.Fatal("cancel not recorded")
	}
	if reply := agentCall(t, p, agent, 27, MethodTerminalCreate, terminalRequest{SessionID: "s1", Command: "true"}); reply["result"] != nil {
		t.Fatalf("cancelled session started a terminal: %v", reply)
	} else {
		requireWireFailure(t, reply)
	}
}
