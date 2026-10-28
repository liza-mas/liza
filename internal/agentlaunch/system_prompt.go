package agentlaunch

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providers"
)

// systemPromptProvider returns "claude" or "codex" when a launch of tool must
// carry the multi-agent contracts in its system prompt: the project opted in
// with use-system-prompt, the session has an agent ID, and the tool is a
// direct CLI that accepts an injected system prompt. Everything else keeps the
// forced-read path.
func systemPromptProvider(projectRoot, agentID, toolName string, tool models.AgentToolConfig) string {
	if agentID == "" || !embedded.SystemPromptModeEnabled(projectRoot) {
		return ""
	}
	if backend := strings.TrimSpace(tool.Backend); backend != "" && backend != string(providers.BackendCLI) {
		return ""
	}
	key := strings.TrimSpace(tool.ProviderKey)
	if key == "" {
		key = toolName
	}
	// Windows caps a whole command line at 32,767 characters, below the
	// contracts Codex would carry in argv; it keeps the forced reads there.
	if key == "claude" || (key == "codex" && runtime.GOOS != "windows") {
		return key
	}
	return ""
}

// SystemPromptContractsApply reports whether an agent launched with cliName
// gets the contracts in its system prompt, so its bootstrap prompt must not
// ask it to read them.
func SystemPromptContractsApply(projectRoot, agentID, cliName string, config models.Config) bool {
	tool, ok := AgentToolRegistry(config)[cliName]
	return ok && systemPromptProvider(projectRoot, agentID, cliName, tool) != ""
}

// injectSystemPromptArgs adds the contract injection for provider.
//
// claude appends to its default system prompt (keeping its tool instructions)
// and moves cwd/env/git status out of it so the prefix is identical across agents.
// codex gets an additive developer message; a -c before the subcommand is a
// root-level override, the same place model flags go.
// write is false for an explanation: the file is built in memory, not refreshed.
func injectSystemPromptArgs(args []string, provider, projectRoot string, write bool) ([]string, error) {
	mode := embedded.SystemPromptModeMultiAgent
	path, content, err := embedded.SystemPromptFilePath(projectRoot, mode), []byte(nil), error(nil)
	if write {
		path, content, err = embedded.EnsureSystemPromptFile(projectRoot, mode)
	} else {
		content, _, err = embedded.BuildSystemPrompt(projectRoot, mode)
	}
	if err != nil {
		return nil, err
	}
	if provider == "claude" {
		return append(args, "--append-system-prompt-file", path, "--exclude-dynamic-system-prompt-sections"), nil
	}
	// Codex has no file key that adds instructions (model_instructions_file
	// replaces its own), so the contracts travel as one argv string.
	value := "developer_instructions=" + tomlBasicString(string(content))
	if len(value) >= maxCodexArgBytes {
		return nil, fmt.Errorf("%s is too large for a codex argument (%d bytes, limit %d on Linux); shorten GUARDRAILS.md or run `%s use-system-prompt --revert`", path, len(value), maxCodexArgBytes, brand.RuntimeValues().BinaryName)
	}
	return append([]string{"-c", value}, args...), nil
}

// maxCodexArgBytes is Linux MAX_ARG_STRLEN (32 pages): one execve argument,
// NUL included, must stay below it. Enforced on every OS so a project that
// launches on macOS also launches on a Linux host.
const maxCodexArgBytes = 32 * 4096

// tomlBasicString quotes s as a TOML basic string, so codex parses -c values
// exactly instead of falling back to a raw literal.
func tomlBasicString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/16 + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				b.WriteString(`\u00`)
				b.WriteByte("0123456789abcdef"[r>>4])
				b.WriteByte("0123456789abcdef"[r&0xf])
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
