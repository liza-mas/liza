package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/brandrender"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSessionHookSembleProviderBudgetAndCorpusLock(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	binary := buildNonDefaultBrandBinary(t)
	values := brand.Normalize(brand.Values{NameLower: "acme-agent", NameUpper: "ACME_AGENT", NameTitle: "Acme Agent", Repo: "acme/agent", BinaryName: "acme-agent", EnvPrefix: "ACME_AGENT"})
	render := func(path string) []byte {
		t.Helper()
		source, err := os.ReadFile(filepath.Join("..", "..", "internal", "embedded", path))
		if err != nil {
			t.Fatal(err)
		}
		// Runtime assets use these placeholders (embedded.renderEmbeddedAsset),
		// while the corpus renderer below handles section-sign master macros.
		for key, value := range brand.MacroMap(values) {
			source = []byte(strings.ReplaceAll(string(source), "__"+key+"__", value))
		}
		out, err := brandrender.RenderBytes(source, values)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Timeout int `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(render("claude-settings.json"), &settings); err != nil {
		t.Fatal(err)
	}
	budget := time.Duration(settings.Hooks["SessionStart"][0].Hooks[0].Timeout) * time.Second
	if budget <= semble.SembleValidationTimeout {
		t.Fatalf("provider budget %v must contain optional query %v", budget, semble.SembleValidationTimeout)
	}
	for _, tt := range []struct {
		name, delay     string
		busy, wantReady bool
	}{
		{"slow ready", "6", false, true},
		{"query timeout", "45", false, false},
		{"preparation busy", "0", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if !semble.EnsureProjectRootIgnore(root).Safe {
				t.Fatal("root safety failed")
			}
			ignore, err := os.OpenFile(filepath.Join(root, ".sembleignore"), os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ignore.WriteString(".acme-agent/\n"); err != nil {
				t.Fatal(err)
			}
			if err := ignore.Close(); err != nil {
				t.Fatal(err)
			}
			hook := filepath.Join(root, "session-context.sh")
			if err := os.WriteFile(hook, render("hooks/session-context.sh"), 0755); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			calls := filepath.Join(root, "calls")
			testhelpers.WriteShellStub(t, filepath.Join(bin, "semble"), "#!/bin/sh\ntest \"$HF_HUB_OFFLINE\" = 1 || exit 3\ntest \"$7\" = all || exit 4\nprintf query > \"$SEMBLE_TEST_CALLS\"\nsleep \"$SEMBLE_TEST_DELAY\"\n")
			if tt.busy {
				resolved, err := filepath.EvalSymlinks(root)
				if err != nil {
					t.Fatal(err)
				}
				lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("semble-repository-%x", sha256.Sum256([]byte(resolved))))
				held, acquired, err := filelock.New(lockPath).TryHold("preparation")
				if err != nil || !acquired {
					t.Fatalf("lock = %v/%v", acquired, err)
				}
				defer held.Release()
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			// Rendered hook resolves the actual Acme CLI, not a fake readiness
			// backend. Only the external Semble process is replaced.
			cmd := exec.CommandContext(ctx, "bash", hook)
			cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(binary)+string(os.PathListSeparator)+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "CLAUDE_PROJECT_DIR="+root,
				"ACME_AGENT_ENABLE_SEMBLE=true", "ACME_AGENT_SKIP_AUTO_UPDATE=1", "ACME_AGENT_AGENT_ID=", "LIZA_AGENT_ID=", "SEMBLE_TEST_CALLS="+filepath.ToSlash(calls), "SEMBLE_TEST_DELAY="+tt.delay)
			cmd.Stdin = strings.NewReader(`{"cwd":""}`)
			out, err := cmd.CombinedOutput()
			if err != nil || ctx.Err() != nil {
				t.Fatalf("hook failed within provider budget: %v/%v\n%s", err, ctx.Err(), out)
			}
			var result struct {
				HookSpecificOutput struct {
					AdditionalContext string `json:"additionalContext"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatalf("invalid startup output: %v\n%s", err, out)
			}
			text := result.HookSpecificOutput.AdditionalContext
			if !strings.Contains(text, "MANDATORY: Read CORE.md") || strings.Contains(text, "Semble semantic search is available") != tt.wantReady {
				t.Fatalf("startup context readiness/fallback incorrect: %s", text)
			}
			assertNoDefaultBrandLeaks(t, "session context", text)
			if _, err := os.Stat(calls); tt.busy && !os.IsNotExist(err) {
				t.Fatal("hook queried corpus during preparation")
			}
		})
	}
}
