package toolresultacp

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/toolresult"
)

func TestTerminalCompleteCaptureFailureAndDuplicate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires sh")
	}
	store, err := toolresult.New(t.TempDir(), toolresult.Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	limit := uint64(16)
	terminal, err := startTerminal(context.Background(), terminalRequest{SessionID: "session", Command: "i=0; while [ $i -lt 3000 ]; do printf 'archive-log-shape row %s\\n' $i; i=$((i+1)); done; printf 'failure-tail' >&2; exit 7", Cwd: t.TempDir(), OutputByteLimit: &limit}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.close()
	select {
	case <-terminal.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	first, err := terminal.output(store, toolresult.Result{TaskID: "DEV-778"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ExitStatus == nil || *first.ExitStatus.ExitCode != 7 || !first.Truncated || len(first.Output) > 1024 {
		t.Fatalf("invalid first result: %+v", first)
	}
	if len(first.Output) > int(limit) {
		t.Fatalf("ACP outputByteLimit ignored: %d", len(first.Output))
	}
	stats, err := store.Stats()
	if err != nil || stats.ExternalizedCount != 1 || stats.OriginalBytes < 60000 {
		t.Fatalf("lost full output: stats=%+v err=%v", stats, err)
	}
	second, err := terminal.output(store, toolresult.Result{})
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("duplicate result exceeded limit: %+v", second)
	}
	stats, err = store.Stats()
	if err != nil || stats.Count != 1 || stats.DuplicateCount != 0 {
		t.Fatalf("terminal was recorded more than once: %+v %v", stats, err)
	}
}

func TestTerminalSmallOutputAndPolling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell")
	}
	store, err := toolresult.New(t.TempDir(), toolresult.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := startTerminal(context.Background(), terminalRequest{SessionID: "s", Command: "printf first; sleep 0.1; printf last", Cwd: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.close()
	first, err := terminal.output(store, toolresult.Result{})
	if err != nil {
		t.Fatal(err)
	}
	if first.ExitStatus != nil {
		t.Fatal("premature exit status")
	}
	<-terminal.done
	final, err := terminal.output(store, toolresult.Result{})
	if err != nil {
		t.Fatal(err)
	}
	if final.Output != "firstlast" || final.Truncated {
		t.Fatalf("small output altered: %+v", final)
	}
}

func TestTerminalPreservesArgumentBoundariesCwdAndEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell")
	}
	store, err := toolresult.New(t.TempDir(), toolresult.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	req := terminalRequest{SessionID: "s", Command: "/bin/sh", Args: []string{"-c", `printf '%s|%s|%s' "$1" "$EE_TEST_VALUE" "$PWD"`, "sh", "space ; ' literal"}, Cwd: dir}
	req.Env = append(req.Env, struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}{"EE_TEST_VALUE", "env-value"})
	terminal, err := startTerminal(context.Background(), req, "")
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.close()
	<-terminal.done
	result, err := terminal.output(store, toolresult.Result{})
	if err != nil {
		t.Fatal(err)
	}
	// macOS shell resolves /var symlink in PWD; compare through the filesystem.
	parts := strings.Split(result.Output, "|")
	if len(parts) != 3 || parts[0] != "space ; ' literal" || parts[1] != "env-value" {
		t.Fatalf("args/env changed: %q", result.Output)
	}
	actual, _ := filepath.EvalSymlinks(parts[2])
	want, _ := filepath.EvalSymlinks(dir)
	if actual != want {
		t.Fatalf("cwd changed: %q", result.Output)
	}
}

func TestTerminalOversizedPollingRetainsEventualFullArtifact(t *testing.T) {
	store, err := toolresult.New(t.TempDir(), toolresult.Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	terminal := &terminal{command: "real asynchronous log stream", session: "session"}
	firstChunk := strings.Repeat("earlier command output\n", 500)
	if _, err := terminal.Write([]byte(firstChunk)); err != nil {
		t.Fatal(err)
	}
	partial, err := terminal.output(store, toolresult.Result{})
	if err != nil {
		t.Fatal(err)
	}
	if partial.ExitStatus != nil || !partial.Truncated || len(partial.Output) > 2048 {
		t.Fatalf("invalid running output: %+v", partial)
	}
	stats, err := store.Stats()
	if err != nil || stats.Count != 0 {
		t.Fatalf("running poll created persisted evidence: %+v %v", stats, err)
	}
	tail := "final failure detail\n"
	_, _ = terminal.Write([]byte(tail))
	exit := 7
	terminal.status = &terminalExit{ExitCode: &exit}
	final, err := terminal.output(store, toolresult.Result{})
	if err != nil {
		t.Fatal(err)
	}
	var after toolresult.Digest
	if err := json.Unmarshal([]byte(final.Output), &after); err != nil {
		t.Fatal(err)
	}
	if after.OriginalBytes != len(firstChunk)+len(tail) || *after.ExitCode != 7 {
		t.Fatalf("lost final evidence: %+v", after)
	}
	retained, err := store.Read(after.ContentHash, 0, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if retained != firstChunk+tail {
		t.Fatal("final artifact omitted earlier partial output")
	}
}

func TestTerminalOutputByteLimitPersistsSmallFullOutput(t *testing.T) {
	store, err := toolresult.New(t.TempDir(), toolresult.Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	limit := uint64(16)
	terminal := &terminal{command: "printf", session: "s", limit: &limit}
	raw := strings.Repeat("é", 300)
	_, _ = terminal.Write([]byte(raw))
	exit := 0
	terminal.status = &terminalExit{ExitCode: &exit}
	got, err := terminal.output(store, toolresult.Result{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Output, "supersecret") {
		t.Fatalf("secret leaked in bounded tail: %q", got.Output)
	}
	if !got.Truncated || len(got.Output) > int(limit) || !utf8.ValidString(got.Output) {
		t.Fatalf("unbounded or invalid output: %+v", got)
	}
	stats, err := store.Stats()
	if err != nil || stats.ExternalizedCount != 1 {
		t.Fatalf("full output not persisted: %+v %v", stats, err)
	}
}

func TestTerminalCaptureIsBoundedWhileDraining(t *testing.T) {
	terminal := &terminal{}
	input := []byte(strings.Repeat("x", terminalCaptureBytes+1024))
	n, err := terminal.Write(input)
	if err != nil || n != len(input) {
		t.Fatalf("terminal writer stopped draining: n=%d err=%v", n, err)
	}
	if terminal.data.Len() != terminalCaptureBytes || !terminal.captureTruncated {
		t.Fatalf("terminal capture was unbounded: bytes=%d truncated=%t", terminal.data.Len(), terminal.captureTruncated)
	}
}
