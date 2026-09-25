package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// stopGuardCommandLimit bounds how much of each job's command the block reason
// quotes, so a long gate command does not flood the agent's context.
const stopGuardCommandLimit = 200

// awaitSubcommands are the session-holding waits whose own instructions tell
// the agent to end the turn when the harness backgrounds them.
var awaitSubcommands = map[string]struct{}{
	"await-verdict":      {},
	"await-resubmission": {},
}

type stopGuardDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// StopGuardCommand is the backend of the Claude Code Stop hook for agent
// sessions. A headless session kills every background job it started when the
// turn ends, so a turn that ends during a long validation or submit gate
// silently destroys that run. The hook keeps the turn open while such a job is
// live; the provider stops honouring the block after its own consecutive-block
// limit, and the supervisor's post-exit detection covers what is left.
//
// Writes a block decision to out, or nothing to let the turn end. Only agent
// sessions are guarded: interactive Pairing sessions keep background jobs
// alive across turns.
func StopGuardCommand(in io.Reader, out io.Writer, getenv func(string) string) error {
	if brand.LookupEnv(getenv, "AGENT_ID").Value == "" {
		return nil
	}
	input, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read stop hook input: %w", err)
	}
	reason, block := stopGuardReason(input, brand.RuntimeValues().BinaryName)
	if !block {
		return nil
	}
	return json.NewEncoder(out).Encode(stopGuardDecision{Decision: "block", Reason: reason})
}

// stopGuardReason decides whether the turn must stay open. It fails closed on
// input it cannot read: a live job it could not recognise is exactly the job
// this guard exists to protect.
func stopGuardReason(input []byte, binaryName string) (string, bool) {
	const unreadable = "The Stop hook could not read the background job list, so a job you started may still be running. Ending the turn kills it. Check your background jobs and wait for any that is still running, as AGENT_TOOLS.md describes."

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(input, &envelope); err != nil {
		return unreadable, true
	}
	raw, ok := envelope["background_tasks"]
	if !ok || string(raw) == "null" {
		return "", false
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return unreadable, true
	}

	var live []string
	for _, entry := range entries {
		var task struct {
			ID      *string `json:"id"`
			Status  *string `json:"status"`
			Command *string `json:"command"`
		}
		if err := json.Unmarshal(entry, &task); err != nil || task.ID == nil || task.Status == nil {
			return unreadable, true
		}
		if *task.Status != "running" {
			continue
		}
		command := ""
		if task.Command != nil {
			command = *task.Command
		}
		if isAwaitCommand(command, binaryName) {
			continue
		}
		live = append(live, fmt.Sprintf("%s (%s)", *task.ID, truncateStopGuardCommand(command)))
	}
	if len(live) == 0 {
		return "", false
	}
	return fmt.Sprintf("Background job(s) you started are still running: %s. Ending the turn now kills them. Wait for each to exit as AGENT_TOOLS.md describes, then act on its result before ending the turn.", strings.Join(live, "; ")), true
}

// isAwaitCommand reports whether command is a plain invocation of an await
// subcommand. Anything else that merely mentions one, such as a compound shell
// command, is not exempt. Syntax outside plain words and literal quotes is
// rejected rather than interpreted, so the guard errs toward blocking.
func isAwaitCommand(command, binaryName string) bool {
	if binaryName == "" || strings.ContainsAny(command, ";&|<>`$()\\\n*?[]{}~#!") {
		return false
	}
	tokens, ok := shellWords(command)
	if !ok || len(tokens) < 2 {
		return false
	}
	executable := strings.TrimSuffix(filepath.Base(tokens[0]), ".exe")
	if executable != binaryName {
		return false
	}
	for i := 1; i < len(tokens); i++ {
		token := tokens[i]
		switch {
		case token == "-C" || token == "--project-root":
			i++
		case strings.HasPrefix(token, "-C=") || strings.HasPrefix(token, "--project-root="),
			token == "-v" || token == "--verbose":
		default:
			_, ok := awaitSubcommands[token]
			return ok
		}
	}
	return false
}

// shellWords splits command into shell words, honouring single and double
// quotes. It relies on isAwaitCommand having rejected every character that
// would make quoting interpret anything ($, backslash, backtick), so quoted
// text is literal. An unterminated quote reports false.
func shellWords(command string) ([]string, bool) {
	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, true
}

func truncateStopGuardCommand(command string) string {
	command = strings.Join(strings.Fields(command), " ")
	runes := []rune(command)
	if len(runes) <= stopGuardCommandLimit {
		return command
	}
	return string(runes[:stopGuardCommandLimit]) + "…"
}
