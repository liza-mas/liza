package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestLoadEnvFile(t *testing.T) {
	t.Run("missing file returns nil", func(t *testing.T) {
		got := loadEnvFile("/nonexistent/path/claude.env")
		if got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("parses KEY=VALUE lines", func(t *testing.T) {
		dir := t.TempDir()
		envFile := filepath.Join(dir, "claude.env")
		content := "FOO=bar\nBAZ=qux\n"
		if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := loadEnvFile(envFile)
		want := []string{"FOO=bar", "BAZ=qux"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("skips comments and empty lines", func(t *testing.T) {
		dir := t.TempDir()
		envFile := filepath.Join(dir, "claude.env")
		content := "# comment\n\n  \nFOO=bar\n# another comment\nBAZ=qux\n"
		if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := loadEnvFile(envFile)
		want := []string{"FOO=bar", "BAZ=qux"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("skips lines without equals", func(t *testing.T) {
		dir := t.TempDir()
		envFile := filepath.Join(dir, "claude.env")
		content := "GOOD=value\nBADLINE\nALSO_GOOD=123\n"
		if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := loadEnvFile(envFile)
		want := []string{"GOOD=value", "ALSO_GOOD=123"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("strips inline comments", func(t *testing.T) {
		dir := t.TempDir()
		envFile := filepath.Join(dir, "claude.env")
		content := "FOO=bar # this is a comment\nBAZ=qux  # another one\n"
		if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := loadEnvFile(envFile)
		want := []string{"FOO=bar", "BAZ=qux"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("handles values with equals signs", func(t *testing.T) {
		dir := t.TempDir()
		envFile := filepath.Join(dir, "claude.env")
		content := "KEY=value=with=equals\n"
		if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := loadEnvFile(envFile)
		if len(got) != 1 || got[0] != "KEY=value=with=equals" {
			t.Fatalf("got %v, want [KEY=value=with=equals]", got)
		}
	})
}

func TestAgentProcessEnvExportsBrandedAndLegacyAgentID(t *testing.T) {
	previous := brand.EnvPrefix
	brand.EnvPrefix = "ACME_AGENT"
	defer func() {
		brand.EnvPrefix = previous
	}()

	got := agentProcessEnv([]string{
		"ACME_AGENT_AGENT_ID=old-branded",
		"LIZA_AGENT_ID=old-legacy",
		"PATH=/bin",
	}, "coder-7")

	want := []string{
		"PATH=/bin",
		"ACME_AGENT_AGENT_ID=coder-7",
		"LIZA_AGENT_ID=coder-7",
	}
	if len(got) != len(want) {
		t.Fatalf("agentProcessEnv() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("agentProcessEnv()[%d] = %q, want %q (full env %v)", i, got[i], want[i], got)
		}
	}
}
