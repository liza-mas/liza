package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func runToolResultCLI(t *testing.T, root, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newToolResultCmd()
	var output bytes.Buffer
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(append([]string{"--root", root}, args...))
	err := cmd.Execute()
	return output.String(), err
}

func TestToolResultCLIBudgetEnvironmentAndFlags(t *testing.T) {
	// A non-default prefix keeps the branded and legacy names distinct, so the
	// precedence between them is observable in the default build too.
	previousPrefix := brand.EnvPrefix
	brand.EnvPrefix = "ACME"
	t.Cleanup(func() { brand.EnvPrefix = previousPrefix })
	threshold := brand.EnvName("TOOL_RESULT_THRESHOLD_BYTES")
	digest := brand.EnvName("TOOL_RESULT_DIGEST_BYTES")
	t.Setenv(threshold, "2048")
	t.Setenv(digest, "1024")
	t.Setenv(brand.LegacyEnvName("TOOL_RESULT_THRESHOLD_BYTES"), "8192")
	t.Setenv(brand.LegacyEnvName("TOOL_RESULT_DIGEST_BYTES"), "2048")
	input := strings.Repeat("bounded data\n", 300)
	got, err := runToolResultCLI(t, t.TempDir(), input, "filter", "--tool", "exec")
	if err != nil || len(got) > 1024 || got == input {
		t.Fatalf("branded env budget: %d bytes, %v", len(got), err)
	}
	got, err = runToolResultCLI(t, t.TempDir(), input, "filter", "--tool", "exec", "--threshold-bytes", "8192", "--digest-bytes", "4096")
	if err != nil || got != input {
		t.Fatalf("explicit flag precedence: %d bytes, %v", len(got), err)
	}
	t.Setenv(threshold, "")
	t.Setenv(digest, "")
	got, err = runToolResultCLI(t, t.TempDir(), input, "filter", "--tool", "exec")
	if err != nil || got != input {
		t.Fatalf("legacy env fallback: %d bytes, %v", len(got), err)
	}
	for _, bad := range []string{"invalid-secret-value", "65537", "-1"} {
		t.Setenv(threshold, bad)
		got, err = runToolResultCLI(t, t.TempDir(), input, "filter", "--tool", "exec")
		if err == nil || strings.Contains(got, "invalid-secret-value") || strings.Contains(got, input) {
			t.Fatalf("invalid environment did not fail closed: %q, %v", got, err)
		}
	}
}

func TestToolResultCLIActualFilterReadAndStats(t *testing.T) {
	root := t.TempDir()
	small := "file.go:42: useful focused read\n"
	got, err := runToolResultCLI(t, root, small, "filter", "--tool", "read")
	if err != nil || got != small {
		t.Fatalf("small filter: %q, %v", got, err)
	}
	content := strings.Repeat("search.go:42: important match\n", 3000)
	payload, _ := json.Marshal(map[string]any{"tool": "exec", "command": "search important", "exit_code": 7, "content": content, "task_id": "DEV-778", "session_id": "cli-session"})
	digest, err := runToolResultCLI(t, root, string(payload), "filter", "--json-input")
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) > 4096 || digest == content {
		t.Fatalf("unbounded digest: %d bytes", len(digest))
	}
	hash := regexp.MustCompile(`[a-f0-9]{64}`).FindString(digest)
	if hash == "" {
		t.Fatalf("missing hash: %s", digest)
	}
	page, err := runToolResultCLI(t, root, "", "read", hash, "--offset", "10", "--limit", "100")
	if err != nil || page != content[10:110] {
		t.Fatalf("artifact read: %q, %v", page, err)
	}
	defaultPage, err := runToolResultCLI(t, root, "", "read", hash)
	if err != nil || defaultPage != content[:32768] {
		t.Fatalf("default artifact read exceeded threshold: %d bytes, %v", len(defaultPage), err)
	}
	if _, err := runToolResultCLI(t, root, string(payload), "filter", "--json-input"); err != nil {
		t.Fatal(err)
	}
	stats, err := runToolResultCLI(t, root, "", "stats")
	if err != nil {
		t.Fatal(err)
	}
	var metrics map[string]any
	if err := json.Unmarshal([]byte(stats), &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics["duplicate_count"] != float64(1) {
		t.Fatalf("missing duplicate telemetry: %s", stats)
	}
}

func TestToolResultCLIRejectsInvalidEnvelopeAndReadBounds(t *testing.T) {
	root := t.TempDir()
	for _, payload := range []string{`{"content":"safe","unknown":1}`, `{"content":"safe"} {}`, `not-json`, `null`} {
		if _, err := runToolResultCLI(t, root, payload, "filter", "--json-input"); err == nil {
			t.Fatalf("accepted invalid envelope %q", payload)
		}
	}
	for _, args := range [][]string{{"read", "../secrets"}, {"read", strings.Repeat("a", 64), "--limit", "65537"}, {"read", strings.Repeat("a", 64), "--offset", "-1"}} {
		if _, err := runToolResultCLI(t, root, "", args...); err == nil {
			t.Fatalf("accepted invalid read %v", args)
		}
	}
}
