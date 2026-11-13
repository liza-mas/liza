package embedded

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

// A Claude subagent's tool calls carry the parent's session_id plus its own
// agent_id and agent_type (observed on Claude Code 2.1.322, DEV-954). These
// tests drive the installed hooks with such payloads.

type subagentCaller struct {
	id, kind string // agent_id, agent_type; empty id = the parent session
}

func (s coreLinkSession) call(t *testing.T, who subagentCaller, tool string, input map[string]any, extraEnv []string, wantCode int) string {
	t.Helper()
	payload := map[string]any{"session_id": s.sessionID, "cwd": s.root, "tool_name": tool, "tool_input": input}
	if who.id != "" {
		payload["agent_id"], payload["agent_type"] = who.id, who.kind
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		t.Fatal(err)
	}
	return s.gate(t, strings.TrimSpace(buf.String()), extraEnv, wantCode)
}

func globalDoc(name string) string { return "~/" + paths.GlobalDirName() + "/" + name }

func (s coreLinkSession) readDocs(t *testing.T, who subagentCaller, extraEnv []string, names ...string) {
	t.Helper()
	for _, name := range names {
		s.call(t, who, "Bash", map[string]any{"command": "cat " + globalDoc(name)}, extraEnv, 0)
	}
}

func (s coreLinkSession) write(t *testing.T, who subagentCaller, extraEnv []string, wantCode int) string {
	t.Helper()
	return s.call(t, who, "Write", map[string]any{"file_path": filepath.Join(s.root, "out.txt"), "content": "x"}, extraEnv, wantCode)
}

var (
	explorer = subagentCaller{"a1b2c3", "general-purpose"}
	sibling  = subagentCaller{"d4e5f6", "general-purpose"}
	fork     = subagentCaller{"f0f0f0", "fork"}
	parent   = subagentCaller{}

	injectedSubagentContracts = brand.EnvName("SUBAGENT_SYSTEM_PROMPT_CONTRACTS") + "=1"
)

func TestSubagentDoesNotInheritTheParentGate(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /x"})
	s.readDocs(t, parent, nil, "AGENT_TOOLS.md", "MULTI_AGENT_MODE.md")
	s.write(t, parent, nil, 0)

	blocked := s.write(t, explorer, nil, 2)
	for _, want := range []string{globalDoc("CORE.md"), globalDoc("SUBAGENT_MODE.md"), globalDoc("AGENT_TOOLS.md")} {
		if !strings.Contains(blocked, want) {
			t.Fatalf("subagent block does not ask for %s:\n%s", want, blocked)
		}
	}
	// The parent's injected-contracts flag is not the subagent's: its prompt is not inherited.
	s.write(t, explorer, []string{injectedContracts}, 2)
}

func TestSubagentReadOnlyToolsWorkBeforeInit(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /x"})
	for tool, input := range map[string]map[string]any{
		"Grep":             {"pattern": "x"},
		"Glob":             {"pattern": "*"},
		"WebFetch":         {"url": "https://example.com"},
		"WebSearch":        {"query": "x"},
		"SubagentHandback": {},
		"Read":             {"file_path": filepath.Join(s.root, "README.md")},
	} {
		s.call(t, explorer, tool, input, nil, 0)
	}
	s.call(t, explorer, "Bash", map[string]any{"command": "git status"}, nil, 2)
	s.call(t, explorer, "Bash", map[string]any{"command": "cat " + globalDoc("SUBAGENT_MODE.md")}, nil, 0)
}

func TestSubagentMustReadCoreEvenWhenTheLinkIsActive(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /x"})
	s.readDocs(t, explorer, nil, "AGENT_TOOLS.md", "SUBAGENT_MODE.md")
	if blocked := s.write(t, explorer, nil, 2); !strings.Contains(blocked, "CORE.md (read it even if it is already in your context)") {
		t.Fatalf("want CORE.md asked for:\n%s", blocked)
	}
	s.readDocs(t, explorer, nil, "CORE.md")
	s.write(t, explorer, nil, 0)
	if calls := s.probeCalls(t); len(calls) != 0 {
		t.Fatalf("a subagent needs no link probe: %q", calls)
	}
}

func TestSubagentNeedsSubagentModeNotAnotherMode(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /x"})
	writePairingProjectDocs(t, s.root)
	s.readDocs(t, explorer, nil, "CORE.md", "AGENT_TOOLS.md", "PAIRING_MODE.md")
	s.call(t, explorer, "Bash", map[string]any{"command": "cat " + filepath.ToSlash(s.root) + "/GUARDRAILS.md"}, nil, 0)
	blocked := s.write(t, explorer, nil, 2)
	if !strings.Contains(blocked, globalDoc("SUBAGENT_MODE.md")) {
		t.Fatalf("PAIRING_MODE.md must not count for a subagent:\n%s", blocked)
	}
	for _, unwanted := range []string{"REPOSITORY.md", "USAGE.md", "COLLABORATION_CONTINUITY.md"} {
		if strings.Contains(blocked, unwanted) {
			t.Fatalf("a subagent is asked for the pairing-only %s:\n%s", unwanted, blocked)
		}
	}
	s.readDocs(t, explorer, nil, "SUBAGENT_MODE.md")
	s.write(t, explorer, nil, 0)
}

func TestSubagentGatesAreIsolated(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /x"})
	s.readDocs(t, explorer, nil, "CORE.md", "AGENT_TOOLS.md", "SUBAGENT_MODE.md")
	s.write(t, explorer, nil, 0)
	s.write(t, sibling, nil, 2)
	s.write(t, parent, nil, 2)
}

// subagent.md carries GUARDRAILS.md, so a project with guardrails still leaves
// an injected subagent only the CORE.md read.
func TestInjectedSubagentContractsLeaveOnlyCore(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /x"})
	if err := os.WriteFile(filepath.Join(s.root, "GUARDRAILS.md"), []byte("# g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{injectedSubagentContracts}
	blocked := s.write(t, explorer, env, 2)
	if !strings.Contains(blocked, globalDoc("CORE.md")) || strings.Contains(blocked, "SUBAGENT_MODE.md") || strings.Contains(blocked, "AGENT_TOOLS.md") {
		t.Fatalf("an injected subagent should need only CORE.md:\n%s", blocked)
	}
	s.readDocs(t, explorer, env, "CORE.md")
	s.write(t, explorer, env, 0)
}

// A fork reuses the parent's prompt, so the inherited variable does not mean
// it received subagent.md.
func TestForkKeepsTheReadsDespiteTheInjectedVariable(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /x"})
	env := []string{injectedSubagentContracts, injectedContracts}
	s.readDocs(t, fork, env, "CORE.md")
	if blocked := s.write(t, fork, env, 2); !strings.Contains(blocked, globalDoc("SUBAGENT_MODE.md")) {
		t.Fatalf("a fork must still read SUBAGENT_MODE.md:\n%s", blocked)
	}
	s.readDocs(t, fork, env, "AGENT_TOOLS.md", "SUBAGENT_MODE.md")
	s.write(t, fork, env, 0)
}

func subagentStartContext(t *testing.T, s coreLinkSession, agentType string, extraEnv []string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"hook_event_name": "SubagentStart", "session_id": s.sessionID, "cwd": s.root, "agent_id": "a1b2c3", "agent_type": agentType})
	if err != nil {
		t.Fatal(err)
	}
	output := runSessionContextHook(t, s.sessionContext, string(payload), append(append([]string{}, s.env...), extraEnv...), 0)
	var got struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("SubagentStart output is not JSON: %v\n%s", err, output)
	}
	if got.HookSpecificOutput.HookEventName != "SubagentStart" {
		t.Fatalf("hookEventName = %q", got.HookSpecificOutput.HookEventName)
	}
	return got.HookSpecificOutput.AdditionalContext
}

func TestSubagentStartTellsTheSubagentHowToInitialize(t *testing.T) {
	s := newCoreLinkSession(t, ".claude", coreLinkProbe{output: "active /x"})
	if err := os.WriteFile(filepath.Join(s.root, "GUARDRAILS.md"), []byte("# g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	coreRule := "Before any state-changing tool, read " + globalDoc("CORE.md") + ", even if it is already in your context."
	for _, tc := range []struct {
		name, agentType string
		env             []string
		injected        bool
	}{
		{"reads", "general-purpose", nil, false},
		{"injected", "general-purpose", []string{injectedSubagentContracts}, true},
		{"fork with the inherited variable", "fork", []string{injectedSubagentContracts}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			context := subagentStartContext(t, s, tc.agentType, tc.env)
			// Claude shows a subagent only a preview of a large context.
			if len(context) > 1024 || !strings.Contains(context, coreRule) {
				t.Fatalf("context (%d bytes) lacks the CORE rule:\n%s", len(context), context)
			}
			if got := strings.Contains(context, "in your system prompt"); got != tc.injected {
				t.Fatalf("claims injection = %v, want %v:\n%s", got, tc.injected, context)
			}
			if !tc.injected && (!strings.Contains(context, globalDoc("SUBAGENT_MODE.md")) || !strings.Contains(context, "GUARDRAILS.md")) {
				t.Fatalf("context does not list the subagent reads:\n%s", context)
			}
		})
	}
	if calls := s.probeCalls(t); len(calls) != 0 {
		t.Fatalf("SubagentStart probed the link: %q", calls)
	}
}
