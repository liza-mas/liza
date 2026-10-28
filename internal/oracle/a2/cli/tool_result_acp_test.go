//go:build linux

package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA2ToolResultACPProxy(t *testing.T) {
	f := newFixture(t)
	goTool, err := exec.LookPath("go")
	must(t, err)
	peer := filepath.Join(f.root, "acp-peer")
	build := exec.Command(goTool, "build", "-o", peer, "./testdata/fixtures/acp/main.go")
	build.Env, err = oracle.ChildEnv(oracle.EnvBase{Home: f.home, Path: os.Getenv("PATH"), Tmp: f.tmp, Go: true})
	must(t, err)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fixture ACP build: %v: %s", err, f.norm.Text(string(out)))
	}
	cmd, cancel := f.command(f.resultArgs("acp-proxy", "--session-id", "fixture-session", "--", peer)...)
	defer cancel()
	in, err := cmd.StdinPipe()
	must(t, err)
	defer in.Close()
	out, err := cmd.StdoutPipe()
	must(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	must(t, cmd.Start())
	waited := false
	defer func() {
		if !waited {
			cancel()
			_ = cmd.Wait()
		}
	}()
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() {
		t.Fatal("ACP peer did not issue its filesystem callback")
	}
	frames := []json.RawMessage{append([]byte(nil), scanner.Bytes()...)}
	request := decode[struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}](t, frames[0])
	if request.ID != 7 || request.Method != "fs/read_text_file" {
		t.Fatal("unexpected ACP callback")
	}
	content := input(t, "tool-result/large.txt")
	must(t, json.NewEncoder(in).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": string(content)}}))
	// Keep stdin open: an early EOF cancels the proxy before the peer's ack.
	if !scanner.Scan() {
		t.Fatal("ACP peer did not acknowledge its filtered response")
	}
	frames = append(frames, append([]byte(nil), scanner.Bytes()...))
	observed := decode[struct {
		Method string `json:"method"`
		Params struct {
			Content string `json:"content"`
		} `json:"params"`
	}](t, frames[1])
	if observed.Method != "fixture/observed" || len(observed.Params.Content) > 1024 {
		t.Fatal("oversized ACP content crossed the budget boundary")
	}
	f.retained(oracle.Result{Stdout: []byte(observed.Params.Content)}, content)
	if scanner.Scan() {
		t.Fatal("unexpected extra ACP frame")
	}
	must(t, scanner.Err())
	waitErr := cmd.Wait()
	waited = true
	code := 0
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) {
		code = exit.ExitCode()
	} else {
		must(t, waitErr)
	}
	encoded, err := json.Marshal(frames)
	must(t, err)
	f.check("tool-result-acp/exchange", oracle.JSON, oracle.Result{Stdout: encoded, Stderr: stderr.Bytes(), ExitCode: code}, 0)
}
