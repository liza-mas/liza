// Package toolresultacp budgets ACP client-owned tool results before the agent
// receives them. Notifications remain observation, never an interception claim.
package toolresultacp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/liza-mas/liza/internal/toolresult"
)

type Config struct {
	Command  []string
	Store    *toolresult.Store
	Metadata toolresult.Result
	In       io.Reader
	Out      io.Writer
	Err      io.Writer
	Dir      string
}

type wireMessage map[string]json.RawMessage

type wireWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *wireWriter) send(m wireMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.writer.Write(append(b, '\n'))
	return err
}

type proxy struct {
	config          Config
	ctx             context.Context
	agent, host     *wireWriter
	mu              sync.Mutex
	reads           map[string]toolresult.Result
	sessions        map[string]string
	newSessions     map[string]string
	terminals       map[string]*terminal
	next            atomic.Uint64
	workers         sync.WaitGroup
	cancelled       map[string]bool
	terminalEnabled bool
}

// Run wraps a native ACP server. Filesystem callbacks remain delegated to the
// original client, preserving its advertised capability, access checks and ranges.
func Run(ctx context.Context, cfg Config) error {
	if len(cfg.Command) == 0 || cfg.Store == nil || cfg.In == nil || cfg.Out == nil {
		return errors.New("ACP proxy requires command, store and protocol streams")
	}
	if cfg.Dir == "" {
		var err error
		cfg.Dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	if !filepath.IsAbs(cfg.Dir) {
		return errors.New("ACP proxy cwd must be absolute")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cfg.Dir
	cmd.Stderr = cfg.Err
	configureProcess(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return errors.New("cannot launch native ACP agent")
	}
	p := &proxy{config: cfg, ctx: ctx, agent: &wireWriter{writer: input}, host: &wireWriter{writer: cfg.Out}, reads: map[string]toolresult.Result{}, sessions: map[string]string{}, newSessions: map[string]string{}, terminals: map[string]*terminal{}, cancelled: map[string]bool{}}
	type pumpEnd struct {
		err  error
		host bool
	}
	ends := make(chan pumpEnd, 2)
	go func() { ends <- pumpEnd{readMessages(cfg.In, p.fromHost), true} }()
	agentDone := make(chan struct{})
	go func() { defer close(agentDone); ends <- pumpEnd{readMessages(output, p.fromAgent), false} }()
	var end error
	nativeEnded := false
	select {
	case result := <-ends:
		end = result.err
		nativeEnded = !result.host
	case <-ctx.Done():
		end = ctx.Err()
	}
	var waitErr error
	waited := nativeEnded && errors.Is(end, io.EOF)
	if waited {
		waitErr = cmd.Wait()
	}
	cancel()
	if closer, ok := cfg.In.(io.Closer); ok {
		_ = closer.Close()
	}
	_ = killProcess(cmd)
	_ = input.Close()
	_ = output.Close()
	<-agentDone
	p.workers.Wait()
	p.mu.Lock()
	terminals := make([]*terminal, 0, len(p.terminals))
	for _, t := range p.terminals {
		terminals = append(terminals, t)
	}
	p.mu.Unlock()
	for _, t := range terminals {
		t.close()
	}
	p.workers.Wait()
	_ = output.Close()
	if !waited {
		_ = cmd.Wait()
	}
	if waited && waitErr != nil {
		return errors.New("native ACP agent exited unsuccessfully")
	}
	if errors.Is(end, io.EOF) {
		return nil
	}
	return end
}

func readMessages(r io.Reader, handle func(wireMessage) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var m wireMessage
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			return errors.New("invalid ACP protocol JSON")
		}
		if err := handle(m); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return errors.New("ACP protocol frame read failed")
	}
	return io.EOF
}
func field(m wireMessage, k string) string { var s string; _ = json.Unmarshal(m[k], &s); return s }
func raw(v any) json.RawMessage            { b, _ := json.Marshal(v); return b }
func decodeParams(m wireMessage) map[string]json.RawMessage {
	var p map[string]json.RawMessage
	_ = json.Unmarshal(m["params"], &p)
	return p
}

func (p *proxy) fromHost(m wireMessage) error {
	method := field(m, "method")
	if method == "initialize" {
		params := decodeParams(m)
		var caps map[string]json.RawMessage
		_ = json.Unmarshal(params["clientCapabilities"], &caps)
		var enabled bool
		_ = json.Unmarshal(caps["terminal"], &enabled)
		p.mu.Lock()
		p.terminalEnabled = enabled
		p.mu.Unlock()
	}
	if method == "session/new" {
		params := decodeParams(m)
		var cwd string
		_ = json.Unmarshal(params["cwd"], &cwd)
		p.mu.Lock()
		p.newSessions[string(m["id"])] = cwd
		p.mu.Unlock()
	}
	if method == "session/prompt" {
		params := decodeParams(m)
		var sid string
		_ = json.Unmarshal(params["sessionId"], &sid)
		p.mu.Lock()
		delete(p.cancelled, sid)
		p.mu.Unlock()
	}
	if method == "session/cancel" {
		params := decodeParams(m)
		var sid string
		_ = json.Unmarshal(params["sessionId"], &sid)
		p.mu.Lock()
		p.cancelled[sid] = true
		for _, t := range p.terminals {
			if t.session == sid {
				_ = killProcess(t.cmd)
			}
		}
		p.mu.Unlock()
	}
	if method == "" {
		key := string(m["id"])
		p.mu.Lock()
		meta, ok := p.reads[key]
		delete(p.reads, key)
		p.mu.Unlock()
		if ok && m["error"] == nil {
			var result map[string]json.RawMessage
			if json.Unmarshal(m["result"], &result) != nil {
				return errors.New("invalid filesystem callback response")
			}
			if json.Unmarshal(result["content"], &meta.Content) != nil {
				return errors.New("filesystem callback content is not text")
			}
			bounded, err := p.config.Store.Process(meta)
			if err != nil {
				delete(m, "result")
				m["error"] = raw(map[string]any{"code": -32603, "message": "Tool result could not be safely retained"})
			} else {
				result["content"] = raw(bounded)
				m["result"] = raw(result)
			}
		}
	}
	return p.agent.send(m)
}

func (p *proxy) fromAgent(m wireMessage) error {
	method := field(m, "method")
	if method == "" {
		p.mu.Lock()
		cwd, ok := p.newSessions[string(m["id"])]
		delete(p.newSessions, string(m["id"]))
		if ok {
			var result map[string]json.RawMessage
			_ = json.Unmarshal(m["result"], &result)
			var sid string
			_ = json.Unmarshal(result["sessionId"], &sid)
			if sid != "" {
				p.sessions[sid] = cwd
			}
		}
		p.mu.Unlock()
	}
	if method == "fs/read_text_file" && m["id"] != nil {
		params := decodeParams(m)
		meta := p.config.Metadata
		meta.Tool = "read"
		meta.Command = string(m["params"])
		_ = json.Unmarshal(params["sessionId"], &meta.SessionID)
		p.mu.Lock()
		p.reads[string(m["id"])] = meta
		p.mu.Unlock()
	}
	if strings.HasPrefix(method, "terminal/") && m["id"] != nil {
		p.workers.Add(1)
		go func() {
			defer p.workers.Done()
			result, err := p.terminalCall(method, m["params"])
			reply := wireMessage{"jsonrpc": raw("2.0"), "id": m["id"]}
			if err != nil {
				reply["error"] = raw(map[string]any{"code": -32603, "message": "Terminal operation failed safely"})
			} else {
				reply["result"] = raw(result)
			}
			_ = p.agent.send(reply)
		}()
		return nil
	}
	return p.host.send(m)
}

func (p *proxy) terminalCall(method string, params json.RawMessage) (any, error) {
	var req terminalRequest
	if json.Unmarshal(params, &req) != nil {
		return nil, errors.New("invalid terminal arguments")
	}
	p.mu.Lock()
	enabled := p.terminalEnabled
	p.mu.Unlock()
	if !enabled {
		return nil, errors.New("terminal capability is disabled")
	}
	if method == "terminal/create" {
		p.mu.Lock()
		dir := p.sessions[req.SessionID]
		cancelled := p.cancelled[req.SessionID]
		p.mu.Unlock()
		if cancelled {
			return nil, context.Canceled
		}
		if dir == "" {
			dir = p.config.Dir
		}
		t, err := startTerminal(p.ctx, req, dir)
		if err != nil {
			return nil, err
		}
		id := "ee-terminal-" + strconv.FormatUint(p.next.Add(1), 10)
		p.mu.Lock()
		if p.cancelled[req.SessionID] {
			p.mu.Unlock()
			t.close()
			return nil, context.Canceled
		}
		p.terminals[id] = t
		p.mu.Unlock()
		return map[string]string{"terminalId": id}, nil
	}
	p.mu.Lock()
	t := p.terminals[req.TerminalID]
	p.mu.Unlock()
	if t == nil || t.session != req.SessionID {
		return nil, errors.New("unknown terminal")
	}
	switch method {
	case "terminal/output":
		return t.output(p.config.Store, p.config.Metadata)
	case "terminal/wait_for_exit":
		select {
		case <-t.done:
			t.mu.Lock()
			defer t.mu.Unlock()
			return t.status, nil
		case <-p.ctx.Done():
			return nil, p.ctx.Err()
		}
	case "terminal/kill":
		_ = killProcess(t.cmd)
		return map[string]any{}, nil
	case "terminal/release":
		t.close()
		p.mu.Lock()
		delete(p.terminals, req.TerminalID)
		p.mu.Unlock()
		return map[string]any{}, nil
	default:
		return nil, errors.New("unsupported terminal operation")
	}
}
