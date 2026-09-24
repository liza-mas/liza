package toolresult

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunCapturesFailureAndDuplicateBeforeReturning(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires sh")
	}
	root := t.TempDir()
	store, err := New(root, Config{}, []string{"private-fixture-value"})
	if err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("failure evidence\n", 10000) + "private-fixture-value\n"
	for attempt := 0; attempt < 2; attempt++ {
		var output bytes.Buffer
		code, err := Run(context.Background(), store, Result{Tool: "Bash", Command: "cat; exit 7", TaskID: "DEV-778"}, []string{"sh", "-c", "cat; printf 'stderr tail\\n' >&2; exit 7"}, strings.NewReader(full), &output)
		if err != nil || code != 7 {
			t.Fatalf("code=%d err=%v", code, err)
		}
		var digest struct {
			ContentHash   string `json:"content_hash"`
			Duplicate     bool   `json:"duplicate"`
			ExitCode      int    `json:"exit_code"`
			OriginalBytes int    `json:"original_bytes"`
		}
		if err := json.Unmarshal(output.Bytes(), &digest); err != nil {
			t.Fatal(err)
		}
		if output.Len() > DefaultDigestBytes || digest.ExitCode != 7 || digest.OriginalBytes != len(full)+len("stderr tail\n") || digest.Duplicate != (attempt == 1) {
			t.Fatalf("digest=%s", output.String())
		}
		data, err := os.ReadFile(root + "/" + digest.ContentHash + ".txt")
		if err != nil || bytes.Contains(data, []byte("private-fixture-value")) || !bytes.HasSuffix(data, []byte("stderr tail\n")) {
			t.Fatalf("artifact bytes=%d err=%v", len(data), err)
		}
	}
}

func TestRunSmallReadAndCancellation(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires sh")
	}
	store, err := New(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	code, err := Run(context.Background(), store, Result{Tool: "Bash", Command: "cat"}, []string{"sh", "-c", "cat"}, strings.NewReader("targeted\n \n"), &output)
	if err != nil || code != 0 || output.String() != "targeted\n \n" {
		t.Fatalf("%q code=%d err=%v", output.String(), code, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	output.Reset()
	code, err = Run(ctx, store, Result{Tool: "Bash", Command: "sleep"}, []string{"sh", "-c", "printf before; sleep 30 & wait"}, nil, &output)
	if err != nil || code == 0 || output.String() != "before" {
		t.Fatalf("%q code=%d err=%v", output.String(), code, err)
	}
}

func TestRunDerivesMissingCommandIdentity(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires sh")
	}
	store, err := New(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	code, err := Run(context.Background(), store, Result{Tool: "exec"}, []string{"sh", "-c", "cat"}, strings.NewReader(strings.Repeat("row\n", 10000)), &output)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	var digest struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(output.Bytes(), &digest); err != nil {
		t.Fatal(err)
	}
	if digest.Command != `["sh","-c","cat"]` {
		t.Fatalf("command=%q", digest.Command)
	}
}

// TestRunBackgroundJobAndCaptureCap pins the round-5 findings: a command that
// leaves a background job holding the pipe succeeds as it would natively, and
// a runaway command is bounded in memory and marked truncated.
func TestRunBackgroundJobAndCaptureCap(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires sh")
	}
	store, err := New(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	code, err := Run(context.Background(), store, Result{Tool: "Bash", Command: "bg"}, []string{"sh", "-c", "sleep 3 & echo hi"}, nil, &output)
	if err != nil || code != 0 || !strings.Contains(output.String(), "hi") {
		t.Fatalf("background job reported as failure: %q code=%d err=%v", output.String(), code, err)
	}
	buffer := &cappedBuffer{limit: 8}
	if n, err := buffer.Write([]byte("0123456789")); n != 10 || err != nil || buffer.data.String() != "01234567" || !buffer.truncated {
		t.Fatalf("cap not enforced: n=%d err=%v data=%q truncated=%v", n, err, buffer.data.String(), buffer.truncated)
	}
}
