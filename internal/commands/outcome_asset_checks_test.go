package commands

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	bashpolicycli "github.com/liza-mas/liza/internal/bash-policy-cli"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
)

// assetFixture isolates one converted warning site: a project root, a HOME,
// and a report the site must reach.
type assetFixture struct {
	root   string
	home   string
	report *jsonout.Report
}

func newAssetFixture(t *testing.T) assetFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	unsetEnvForTest(t, bashpolicycli.EnvEnableBashPolicy)
	return assetFixture{root: t.TempDir(), home: home, report: jsonout.NewReport()}
}

// block makes path unusable for a writer: a regular file where a directory
// is needed, or a directory where a file is needed.
func blockWithFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("blocker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func blockWithDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertReportedCheck(t *testing.T, report *jsonout.Report, id string, status jsonout.CheckStatus, messagePart string) jsonout.Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id && check.Status == status && strings.Contains(check.Message, messagePart) {
			if check.Severity != jsonout.SeverityWarn {
				t.Fatalf("check %+v severity = %s, want warn", check, check.Severity)
			}
			return check
		}
	}
	t.Fatalf("no %s %s check mentioning %q in %+v", status, id, messagePart, report.Checks)
	return jsonout.Check{}
}

func emptyStdin() *bufio.Reader { return bufio.NewReader(strings.NewReader("")) }

func assetProvider(id string, assets providers.ActivationAssets) providers.Provider {
	return providers.Provider{ID: id, Setup: providers.Setup{ActivationAssets: assets}}
}

// TestPairingAssetFailuresReachTheReport: each asset write in pairing init
// that fails yields an init.asset check (one row per call site).
func TestPairingAssetFailuresReachTheReport(t *testing.T) {
	rows := []struct {
		name      string
		selection pairingSelection
		block     func(t *testing.T, f assetFixture)
		asset     string
	}{
		{"claude settings", pairingSelection{hasClaude: true}, func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.root, ".claude")) }, "claude-settings.json"},
		{"codex config", pairingSelection{hasCodex: true}, func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.home, ".codex")) }, "codex config"},
		{"codex hooks", pairingSelection{hasCodex: true}, func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.root, ".codex")) }, "codex hooks"},
		{"bash policy", pairingSelection{hasBashPolicyClaude: true}, func(t *testing.T, f assetFixture) {
			t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")
			t.Cleanup(setInitBashPolicyHooksForTest(func(string) (string, error) { return "", errors.New("not found") }, nil))
			blockWithDir(t, filepath.Join(f.root, ".bash-policy.yaml"))
		}, ".bash-policy.yaml"},
		{"opencode exec tool", pairingSelection{hasOpenCode: true}, func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.root, ".opencode")) }, "opencode exec tool"},
		{"copilot hooks", pairingSelection{hasCopilot: true}, func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.root, ".github")) }, "copilot hooks"},
		{"pi init gate", pairingSelection{hasPi: true}, func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.home, paths.GlobalDirName())) }, "pi init gate"},
		{"claudeignore", pairingSelection{hasClaude: true}, func(t *testing.T, f assetFixture) { blockWithDir(t, filepath.Join(f.root, ".claudeignore")) }, ".claudeignore"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f := newAssetFixture(t)
			row.block(t, f)
			row.selection.installAssets(f.root, strings.NewReader(""), emptyStdin(), true, f.report)
			assertReportedCheck(t, f.report, "init.asset", jsonout.CheckFailed, row.asset)
		})
	}
}

// TestMASAssetFailuresReachTheReport: the same per-site coverage for full
// workspace init.
func TestMASAssetFailuresReachTheReport(t *testing.T) {
	newMAS := func(f assetFixture, selected ...providers.Provider) *masInit {
		return &masInit{
			params:            InitParams{AutoConfirm: true, Report: f.report},
			rawStdin:          strings.NewReader(""),
			stdin:             emptyStdin(),
			confirmOptions:    embedded.ConfirmOptions{AutoConfirm: true},
			selectedProviders: selected,
			lizaPaths:         paths.New(f.root),
		}
	}
	codex := assetProvider("codex", providers.ActivationAssets{CodexConfig: true, CodexHooks: true})
	rows := []struct {
		name  string
		block func(t *testing.T, f assetFixture)
		run   func(m *masInit)
		asset string
	}{
		{"claude settings", func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.root, ".claude")) }, (*masInit).installProviderAssets, "claude-settings.json"},
		{"claudeignore", func(t *testing.T, f assetFixture) { blockWithDir(t, filepath.Join(f.root, ".claudeignore")) }, (*masInit).installProviderAssets, ".claudeignore"},
		{"codex config", func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.home, ".codex")) }, (*masInit).installCodexConfig, "codex config"},
		{"codex hooks", func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.root, ".codex")) }, (*masInit).installCodexConfig, "codex hooks"},
		{"bash policy", func(t *testing.T, f assetFixture) {
			t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")
			t.Cleanup(setInitBashPolicyHooksForTest(func(string) (string, error) { return "", errors.New("not found") }, nil))
			blockWithDir(t, filepath.Join(f.root, ".bash-policy.yaml"))
		}, (*masInit).installBashPolicy, ".bash-policy.yaml"},
		{"opencode exec tool", func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.root, ".opencode")) }, (*masInit).installProviderExtras, "opencode exec tool"},
		{"copilot hooks", func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.root, ".github")) }, (*masInit).installProviderExtras, "copilot hooks"},
		{"pi init gate", func(t *testing.T, f assetFixture) { blockWithFile(t, filepath.Join(f.home, paths.GlobalDirName())) }, (*masInit).installProviderExtras, "pi init gate"},
	}
	extras := []providers.Provider{
		codex,
		assetProvider("opencode", providers.ActivationAssets{OpenCodeExecTool: true}),
		assetProvider("copilot", providers.ActivationAssets{CopilotHooks: true}),
		assetProvider("pi", providers.ActivationAssets{PiExtension: true}),
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f := newAssetFixture(t)
			row.block(t, f)
			row.run(newMAS(f, extras...))
			assertReportedCheck(t, f.report, "init.asset", jsonout.CheckFailed, row.asset)
		})
	}
	t.Run("guardrails", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a non-root POSIX user to make the project root unwritable")
		}
		// GUARDRAILS.md is written only when absent, so nothing at its path can
		// make it fail; an unwritable project root does.
		f := newAssetFixture(t)
		if err := os.Chmod(f.root, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(f.root, 0o755) })
		newMAS(f).installProviderAssets()
		assertReportedCheck(t, f.report, "init.asset", jsonout.CheckFailed, "GUARDRAILS.md")
	})
}

// TestBashPolicyHookOutcomesReachTheReport covers the two non-write
// bash-policy failures: the tool missing and the hook activation failing.
func TestBashPolicyHookOutcomesReachTheReport(t *testing.T) {
	t.Run("tool missing", func(t *testing.T) {
		f := newAssetFixture(t)
		t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")
		t.Cleanup(setInitBashPolicyHooksForTest(func(string) (string, error) { return "", errors.New("not found") }, nil))
		runBashPolicyInits(f.root, []string{"claude"}, strings.NewReader(""), true, f.report)
		check := assertReportedCheck(t, f.report, "init.asset", jsonout.CheckMissing, "not found on PATH")
		if check.Provider != "claude" {
			t.Fatalf("check provider = %q, want claude", check.Provider)
		}
	})
	t.Run("activation failed", func(t *testing.T) {
		f := newAssetFixture(t)
		t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")
		runner := &initBashPolicyTestRunner{err: errors.New("hook install failed")}
		t.Cleanup(setInitBashPolicyHooksForTest(func(string) (string, error) { return filepath.Join(f.root, "bin", "bash-policy"), nil }, runner))
		runBashPolicyInits(f.root, []string{"claude"}, strings.NewReader(""), true, f.report)
		check := assertReportedCheck(t, f.report, "init.asset", jsonout.CheckFailed, "bash-policy hooks")
		if strings.Contains(check.Message, "hook install failed") {
			t.Fatalf("check message %q carries the tool's output, which must stay on stderr", check.Message)
		}
	})
}

// TestSetupDegradedSitesReachTheReport covers setup's converted sites other
// than the occupied skill link, which the CLI tests cover end to end.
func TestSetupDegradedSitesReachTheReport(t *testing.T) {
	t.Run("pi init gate", func(t *testing.T) {
		f := newAssetFixture(t)
		globalDir := filepath.Join(t.TempDir(), "global")
		blockWithDir(t, filepath.Join(globalDir, "skills"))
		blockWithFile(t, filepath.Join(f.home, paths.GlobalDirName()))
		pi := providers.Provider{ID: "pi", Setup: providers.Setup{ConfigDir: ".pi", SkillsDir: "skills", ActivationAssets: providers.ActivationAssets{PiExtension: true}}}
		if err := installSetupProviders(SetupParams{TargetDir: globalDir, AutoConfirm: true, Report: f.report}, f.home, []providers.Provider{pi}, emptyStdin()); err != nil {
			t.Fatal(err)
		}
		assertReportedCheck(t, f.report, "setup.asset", jsonout.CheckFailed, "pi init gate")
	})
	t.Run("skill link answer unreadable", func(t *testing.T) {
		f := newAssetFixture(t)
		globalDir := filepath.Join(t.TempDir(), "global")
		blockWithDir(t, filepath.Join(globalDir, "skills", "debugging"))
		link := filepath.Join(f.home, ".claude", "skills", "debugging")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(f.home, "elsewhere"), link); err != nil {
			t.Fatal(err)
		}
		claude := providers.Provider{ID: "claude", Setup: providers.Setup{ConfigDir: ".claude", SkillsDir: "skills"}}
		if err := setupAgentSymlinks(f.home, globalDir, []providers.Provider{claude}, emptyStdin(), false, f.report); err != nil {
			t.Fatal(err)
		}
		check := assertReportedCheck(t, f.report, "setup.provider-link", jsonout.CheckFailed, "could not be read")
		if check.Provider != "claude" || check.Path != link {
			t.Fatalf("check = %+v, want provider claude at %s", check, link)
		}
	})
	t.Run("declined replacement is a choice, not degraded", func(t *testing.T) {
		f := newAssetFixture(t)
		globalDir := filepath.Join(t.TempDir(), "global")
		blockWithDir(t, filepath.Join(globalDir, "skills", "debugging"))
		link := filepath.Join(f.home, ".claude", "skills", "debugging")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(f.home, "elsewhere"), link); err != nil {
			t.Fatal(err)
		}
		claude := providers.Provider{ID: "claude", Setup: providers.Setup{ConfigDir: ".claude", SkillsDir: "skills"}}
		if err := setupAgentSymlinks(f.home, globalDir, []providers.Provider{claude}, bufio.NewReader(strings.NewReader("n\n")), false, f.report); err != nil {
			t.Fatal(err)
		}
		if f.report.Degraded() {
			t.Fatalf("checks = %+v, want none for a declined replacement", f.report.Checks)
		}
	})
	t.Run("global file answer unreadable", func(t *testing.T) {
		f := newAssetFixture(t)
		targetDir := t.TempDir()
		agentTools := filepath.Join(targetDir, setupAgentToolsFile)
		blockWithFile(t, agentTools)
		// "y" answers the overall overwrite prompt; the per-file prompt then
		// hits end of input.
		if _, err := confirmOverwrites([]string{agentTools}, nil, true, false, targetDir, bufio.NewReader(strings.NewReader("y\n")), nil, f.report); err != nil {
			t.Fatal(err)
		}
		check := assertReportedCheck(t, f.report, "setup.global-file", jsonout.CheckFailed, "could not be read")
		if check.Path != agentTools {
			t.Fatalf("check path = %q, want %s", check.Path, agentTools)
		}
	})
	mistralConfig := func(t *testing.T, f assetFixture) (core, config string) {
		t.Helper()
		core = filepath.Join(t.TempDir(), "CORE.md")
		blockWithFile(t, core)
		config = filepath.Join(f.home, mistralConfigDirName, mistralConfigFileName)
		if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config, []byte(mistralSystemPromptKey+" = \"other\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return core, config
	}
	t.Run("mistral config answer unreadable", func(t *testing.T) {
		f := newAssetFixture(t)
		core, config := mistralConfig(t, f)
		// The prompt link is fresh; the existing config selects another
		// prompt, and end of input leaves it unchanged without a user choice.
		if err := setupMistralContract(core, emptyStdin(), false, f.report); err != nil {
			t.Fatal(err)
		}
		check := assertReportedCheck(t, f.report, "contract.placement", jsonout.CheckFailed, "could not be read")
		if check.Provider != mistralProviderID || check.Path != config {
			t.Fatalf("check = %+v, want provider %s at %s", check, mistralProviderID, config)
		}
	})
	t.Run("mistral config declined is a choice, not degraded", func(t *testing.T) {
		f := newAssetFixture(t)
		core, _ := mistralConfig(t, f)
		if err := setupMistralContract(core, bufio.NewReader(strings.NewReader("n\n")), false, f.report); err != nil {
			t.Fatal(err)
		}
		if f.report.Degraded() {
			t.Fatalf("checks = %+v, want none for a declined config update", f.report.Checks)
		}
	})
	t.Run("mistral prompt link answer unreadable", func(t *testing.T) {
		f := newAssetFixture(t)
		core := filepath.Join(t.TempDir(), "CORE.md")
		blockWithFile(t, core)
		link := filepath.Join(f.home, mistralConfigDirName, mistralPromptsDirName, brand.CanonicalMistralPromptID+".md")
		blockWithFile(t, link)
		// The existing prompt file triggers the overwrite prompt; end of input
		// leaves the contract unlinked without a user choice.
		if err := setupMistralContract(core, emptyStdin(), false, f.report); err != nil {
			t.Fatal(err)
		}
		check := assertReportedCheck(t, f.report, "contract.placement", jsonout.CheckFailed, "could not be read")
		if check.Provider != mistralProviderID || check.Path != link {
			t.Fatalf("check = %+v, want provider %s at %s", check, mistralProviderID, link)
		}
	})
}
