package embedded

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

// The session hooks ask the engine whether the CLI running them loads CORE.md
// through its contract link (DEV-954). These tests replace the engine with a
// stub whose answer they choose.

type coreLinkProbe struct {
	output   string // printed by the stub
	exitCode int
	missing  bool // no engine on PATH at all
	// activeRoot, when set, makes the stub answer "active" only for this
	// --project-root and "inactive" for any other.
	activeRoot string
}

// coreLinkSession installs both hooks under root/<cliDir>/hooks, as init
// does, and a stub engine answering probe. It returns the hook paths, the
// environment to run them with and the file the stub logs its calls to.
type coreLinkSession struct {
	root, enforceInit, sessionContext, probeLog, sessionID string
	env                                                    []string
}

func newCoreLinkSession(t *testing.T, cliDir string, probe coreLinkProbe) coreLinkSession {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub engine is a shell script, which Windows does not run by name")
	}
	s := coreLinkSession{root: t.TempDir(), sessionID: fmt.Sprintf("core-link-%d", time.Now().UnixNano())}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(os.TempDir(), brand.BinaryName+"-init-gate-"+s.sessionID)) })
	hooksDir := filepath.Join(s.root, cliDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s.enforceInit = filepath.Join(hooksDir, "enforce-init.sh")
	s.sessionContext = filepath.Join(hooksDir, "session-context.sh")
	for path, content := range map[string][]byte{s.enforceInit: enforceInitHookContent, s.sessionContext: sessionContextHookContent} {
		if err := os.WriteFile(path, renderEmbeddedAsset(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	binDir := t.TempDir()
	s.probeLog = filepath.Join(t.TempDir(), "probe.log")
	if !probe.missing {
		stub := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %q\nprintf '%%s\\n' %q\nexit %d\n", s.probeLog, probe.output, probe.exitCode)
		if probe.activeRoot != "" {
			stub = fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %q\ncase \"$*\" in\n  *\"--project-root %s\") echo 'active /x' ;;\n  *) echo inactive ;;\nesac\n", s.probeLog, probe.activeRoot)
		}
		if err := os.WriteFile(filepath.Join(binDir, brand.BinaryName), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.env = []string{"PATH=" + binDir + string(os.PathListSeparator) + pathWithoutEngine(t)}
	return s
}

// pathWithoutEngine drops every PATH entry holding a real engine, so only the
// stub (or nothing) answers the probe.
func pathWithoutEngine(t *testing.T) string {
	t.Helper()
	var kept []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(dir, brand.BinaryName)); err == nil {
			continue
		}
		kept = append(kept, dir)
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

func (s coreLinkSession) probeCalls(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(s.probeLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (s coreLinkSession) gate(t *testing.T, payload string, extraEnv []string, wantCode int) string {
	t.Helper()
	return runHookEnv(t, s.enforceInit, payload, append(append([]string{}, s.env...), extraEnv...), wantCode)
}

func (s coreLinkSession) context(t *testing.T, extraEnv []string) string {
	t.Helper()
	output := runSessionContextHook(t, s.sessionContext, sessionStartPayload(t, s.root), append(append([]string{}, s.env...), extraEnv...), 0)
	return sessionStartAdditionalContext(t, output)
}

func readToolPayload(t *testing.T, sessionID, cwd, filePath string) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{
		"session_id": sessionID,
		"cwd":        cwd,
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": filePath},
	}); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(buf.String())
}

var (
	injectedContracts = brand.EnvName("SYSTEM_PROMPT_CONTRACTS") + "=1"
	coreDoc           = "~/" + paths.GlobalDirName() + "/CORE.md"
)

func TestCoreLinkProbeNamesTheCLIFromItsHookDirectory(t *testing.T) {
	for dir, provider := range map[string]string{".claude": "claude", ".codex": "codex", ".copilot": "copilot"} {
		t.Run(provider, func(t *testing.T) {
			s := newCoreLinkSession(t, dir, coreLinkProbe{output: "active /x"})
			s.gate(t, bashPayload(t, s.sessionID, s.root, "git status"), []string{injectedContracts}, 0)
			calls := s.probeCalls(t)
			want := "contract-active --provider " + provider + " --project-root " + s.root
			if len(calls) != 1 || calls[0] != want {
				t.Fatalf("probe calls = %q, want [%q]", calls, want)
			}
		})
	}
}

// A hook copied anywhere else was not installed for a CLI: it keeps the
// behavior it had before the probe existed.
func TestCoreLinkProbeSkippedOutsideACLIHookDirectory(t *testing.T) {
	s := newCoreLinkSession(t, "elsewhere", coreLinkProbe{output: "inactive"})
	s.gate(t, bashPayload(t, s.sessionID, s.root, "git status"), []string{injectedContracts}, 0)
	if !strings.Contains(s.context(t, nil), "CORE.md is already in your system prompt") {
		t.Fatal("a hook outside a CLI hook directory must not ask for CORE.md")
	}
	if calls := s.probeCalls(t); len(calls) != 0 {
		t.Fatalf("probe ran: %q", calls)
	}
}

func TestInjectedSessionWithoutCoreLinkMustReadCore(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(s coreLinkSession, t *testing.T) string
	}{
		{"native read", func(s coreLinkSession, t *testing.T) string {
			return readToolPayload(t, s.sessionID, s.root, filepath.Join(os.Getenv("HOME"), paths.GlobalDirName(), "CORE.md"))
		}},
		{"bash read", func(s coreLinkSession, t *testing.T) string {
			return bashPayload(t, s.sessionID, s.root, "cat "+coreDoc)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "inactive"})
			blocked := s.gate(t, bashPayload(t, s.sessionID, s.root, "git status"), []string{injectedContracts}, 2)
			if !strings.Contains(blocked, coreDoc) || strings.Contains(blocked, "AGENT_TOOLS.md") {
				t.Fatalf("want only CORE.md asked for, the rest is in the system prompt:\n%s", blocked)
			}
			s.gate(t, tc.read(s, t), []string{injectedContracts}, 0)
			s.gate(t, bashPayload(t, s.sessionID, s.root, "git status"), []string{injectedContracts}, 0)

			context := s.context(t, []string{injectedContracts})
			if !strings.Contains(context, "CORE.md is NOT loaded") || !strings.Contains(context, "other "+brand.NameTitle+" contracts are in your system prompt") {
				t.Fatalf("session context does not ask for CORE.md:\n%s", context)
			}
		})
	}
}

func TestForcedReadSessionWithoutCoreLinkMustReadCore(t *testing.T) {
	s := newCoreLinkSession(t, ".codex", coreLinkProbe{output: "inactive"})
	writePairingProjectDocs(t, s.root)
	for _, cmd := range []string{
		"cat ~/" + paths.GlobalDirName() + "/AGENT_TOOLS.md",
		"cat ~/" + paths.GlobalDirName() + "/PAIRING_MODE.md",
		"cat " + filepath.ToSlash(s.root) + "/REPOSITORY.md",
		"cat " + filepath.ToSlash(s.root) + "/docs/USAGE.md",
		"cat " + filepath.ToSlash(s.root) + "/GUARDRAILS.md",
		"cat ~/" + paths.GlobalDirName() + "/COLLABORATION_CONTINUITY.md",
	} {
		s.gate(t, bashPayload(t, s.sessionID, s.root, cmd), nil, 0)
	}
	blocked := s.gate(t, bashPayload(t, s.sessionID, s.root, "git status"), nil, 2)
	if !strings.Contains(blocked, coreDoc) {
		t.Fatalf("gate cleared without CORE.md:\n%s", blocked)
	}
	s.gate(t, bashPayload(t, s.sessionID, s.root, "cat "+coreDoc), nil, 0)
	s.gate(t, bashPayload(t, s.sessionID, s.root, "git status"), nil, 0)

	context := s.context(t, nil)
	if !strings.Contains(context, "CORE.md is NOT loaded") || !strings.Contains(context, coreDoc+", ~/"+paths.GlobalDirName()+"/PAIRING_MODE.md") {
		t.Fatalf("session context does not list CORE.md first:\n%s", context)
	}
}

// An installed hook that cannot confirm the link counts it as not loaded.
func TestCoreLinkProbeFailureRequiresCore(t *testing.T) {
	for name, probe := range map[string]coreLinkProbe{
		"engine missing":    {missing: true},
		"probe error":       {output: "unknown command", exitCode: 1},
		"unexpected output": {output: "maybe"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newCoreLinkSession(t, ".claude", probe)
			if blocked := s.gate(t, bashPayload(t, s.sessionID, s.root, "git status"), []string{injectedContracts}, 2); !strings.Contains(blocked, coreDoc) {
				t.Fatalf("want CORE.md asked for:\n%s", blocked)
			}
			if !strings.Contains(s.context(t, nil), "CORE.md is NOT loaded") {
				t.Fatal("session context does not ask for CORE.md")
			}
		})
	}
}

func TestCoreLinkActiveKeepsCoreOptional(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /home/user/.claude/CLAUDE.md"})
	s.gate(t, bashPayload(t, s.sessionID, s.root, "git status"), []string{injectedContracts}, 0)
	if context := s.context(t, nil); !strings.Contains(context, "CORE.md is already in your system prompt") || strings.Contains(context, "NOT loaded") {
		t.Fatalf("an active link must not ask for CORE.md:\n%s", context)
	}

	forced := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /home/user/.claude/CLAUDE.md"})
	for _, cmd := range []string{"cat ~/" + paths.GlobalDirName() + "/AGENT_TOOLS.md", "cat ~/" + paths.GlobalDirName() + "/MULTI_AGENT_MODE.md"} {
		forced.gate(t, bashPayload(t, forced.sessionID, forced.root, cmd), nil, 0)
	}
	forced.gate(t, bashPayload(t, forced.sessionID, forced.root, "git status"), nil, 0)
}

// The gate probes once per session whatever the answer; later calls read the
// cached outcome.
func TestCoreLinkProbeRunsOncePerSession(t *testing.T) {
	for _, output := range []string{"active /x", "inactive"} {
		t.Run(output, func(t *testing.T) {
			s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: output})
			for range 3 {
				s.gate(t, readToolPayload(t, s.sessionID, s.root, filepath.Join(s.root, "README.md")), nil, 0)
			}
			if calls := s.probeCalls(t); len(calls) != 1 {
				t.Fatalf("gate probed %d times, want 1", len(calls))
			}
		})
	}
}

// A MAS session runs in its task worktree while CLAUDE_PROJECT_DIR may still
// name the parent checkout: both hooks must probe the worktree, so startup and
// the gate agree whichever of the two links the contract.
func TestCoreLinkProbeUsesTheTaskWorktreeInMASSessions(t *testing.T) {
	worktree := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", worktree).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	worktree = gitToplevel(t, worktree)
	agent := brand.EnvName("AGENT_ID") + "=coder-1"
	for _, tc := range []struct {
		name       string
		worktreeOn bool
	}{
		{"worktree linked, parent not", true},
		{"parent linked, worktree not", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			activeRoot := parent
			if tc.worktreeOn {
				activeRoot = worktree
			}
			s := newCoreLinkSession(t, ".claude", coreLinkProbe{activeRoot: activeRoot})
			env := []string{agent, injectedContracts, "CLAUDE_PROJECT_DIR=" + parent}
			gateCode := 2
			if tc.worktreeOn {
				gateCode = 0
			}
			s.gate(t, bashPayload(t, s.sessionID, worktree, "git status"), env, gateCode)
			output := runSessionContextHook(t, s.sessionContext, sessionStartPayload(t, worktree), append(append([]string{}, s.env...), env...), 0)
			if got := strings.Contains(sessionStartAdditionalContext(t, output), "CORE.md is NOT loaded"); got == tc.worktreeOn {
				t.Fatalf("startup asks for CORE.md = %v, want %v", got, !tc.worktreeOn)
			}
			want := "--project-root " + worktree
			calls := s.probeCalls(t)
			if len(calls) != 2 {
				t.Fatalf("probe calls = %q, want one per hook", calls)
			}
			for _, call := range calls {
				if !strings.HasSuffix(call, want) {
					t.Fatalf("probe call %q does not check the worktree %s", call, worktree)
				}
			}
		})
	}
}

func gitToplevel(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}
