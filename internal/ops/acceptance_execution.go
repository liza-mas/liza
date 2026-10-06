package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/secretmask"
)

const acceptanceOutputLimit = 1 << 20
const acceptanceFailureExcerptLimit = 8 << 10

var errAcceptanceOutputLimit = errors.New("acceptance execution output limit exceeded")

// executeAcceptanceCommands returns trusted executions only when every command
// passes, and nil results on any error. Callers own immutable input validation.
func executeAcceptanceCommands(taskID, worktree string, commands []string, timeoutSeconds int) ([]models.AcceptanceCommandResult, error) {
	return executeAcceptanceCommandsWith(context.Background(), taskID, worktree, commands, timeoutSeconds, nil)
}

// acceptanceBatchTimeout is the batch budget a contract's timeout_seconds
// grants; zero means the default.
func acceptanceBatchTimeout(timeoutSeconds int) time.Duration {
	if timeoutSeconds == 0 {
		timeoutSeconds = 600
	}
	return time.Duration(timeoutSeconds) * time.Second
}

// executeAcceptanceCommandsWith runs the canonical commands with the runtime
// inputs grant allows: every reserved name is scrubbed from each command's
// environment and only its authorized variables are overlaid. Masking covers
// the inherited environment, the overlays and the declared secret values, and
// is applied before any result, excerpt or error is built. Cancelling ctx
// kills the running command's process group like the batch timeout does.
func executeAcceptanceCommandsWith(ctx context.Context, taskID, worktree string, commands []string, timeoutSeconds int, grant *runtimeInputGrant) ([]models.AcceptanceCommandResult, error) {
	environ := os.Environ()
	masked := environ
	if grant != nil {
		masked = slices.Clone(environ)
		for _, overlay := range grant.overlays {
			masked = append(masked, overlay...)
		}
	}
	mask := acceptanceExecutionMaskWith(masked, grant.secretValues())
	fail := func(index int, reason string) error {
		return fmt.Errorf("acceptance.execution[%d] for task %s: %s", index, mask(taskID), mask(reason))
	}
	if timeoutSeconds < 0 || timeoutSeconds > referencecontract.AcceptanceMaxTimeoutSeconds {
		return nil, fail(0, fmt.Sprintf("timeout_seconds must be between 1 and %d", referencecontract.AcceptanceMaxTimeoutSeconds))
	}
	ctx, cancel := context.WithTimeout(ctx, acceptanceBatchTimeout(timeoutSeconds))
	defer cancel()
	results := make([]models.AcceptanceCommandResult, 0, len(commands))
	remaining := acceptanceOutputLimit
	maskedRemaining := acceptanceOutputLimit
	for i, command := range commands {
		if ctx.Err() != nil {
			return nil, fail(i, "command batch timed out")
		}
		output := &acceptanceOutputBuffer{remaining: remaining, cancel: cancel}
		result := models.AcceptanceCommandResult{
			Command:       mask(command),
			CommandSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(command))),
			ExitCode:      -1,
			StartedAt:     time.Now().UTC(),
		}
		cmd, err := shellCommandContext(ctx, command, worktree)
		if err != nil {
			return nil, fail(i, "cannot prepare command: "+err.Error())
		}
		cmd.Stdout, cmd.Stderr = output, output
		cmd.Env = grant.commandEnvironment(environ, command)
		configProcessGroupKill(cmd)
		cmd.WaitDelay = 5 * time.Second
		err = cmd.Run()
		result.FinishedAt = time.Now().UTC()
		if cmd.ProcessState != nil {
			result.ExitCode = cmd.ProcessState.ExitCode()
		}
		if output.overflow {
			// Do not return a truncated prefix that may contain only part of a
			// known secret and therefore evade exact-value masking.
			return nil, fail(i, "combined command output exceeds 1 MiB; produce smaller sanitized output")
		}
		result.Output = mask(output.buffer.String())
		if len(result.Output) > maskedRemaining {
			return nil, fail(i, "masked command output exceeds 1 MiB; produce smaller sanitized output")
		}
		remaining -= output.buffer.Len()
		maskedRemaining -= len(result.Output)
		results = append(results, result)
		if ctx.Err() != nil {
			return nil, fail(i, "command batch timed out")
		}
		if err != nil {
			// Bound only after masking the complete captured output: a raw
			// excerpt could split a credential and evade exact-value redaction.
			reason := fmt.Sprintf("command failed (exit %d): %s", result.ExitCode, err)
			excerpt, truncated := persistedOutputExcerpt(result.Output, acceptanceFailureExcerptLimit)
			if truncated {
				excerpt += "\n[output truncated]"
			}
			if excerpt != "" {
				reason += "\ncommand output (masked):\n" + excerpt
			}
			// Execution errors may contain paths or shell diagnostics; mask them
			// and discard all results from the failed batch.
			return nil, fail(i, reason)
		}
	}
	return results, nil
}

// Bound raw bytes before masking: redaction must not let a noisy process evade
// the limit. Cancellation also stops writers that never close their pipes.
type acceptanceOutputBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	remaining int
	overflow  bool
	cancel    context.CancelFunc
}

func (w *acceptanceOutputBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) <= w.remaining {
		w.remaining -= len(p)
		return w.buffer.Write(p)
	}
	n, _ := w.buffer.Write(p[:w.remaining])
	w.remaining = 0
	w.overflow = true
	w.cancel()
	return n, errAcceptanceOutputLimit
}

var acceptanceDSNPassword = regexp.MustCompile(`(?i)(?:^|[\s;])(?:password|pwd)\s*=\s*(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"|([^\s;]+))`)
var acceptanceDSNEscape = regexp.MustCompile(`\\(.)`)

// Extend the standard environment masker for URL/DSN variables. Explicit
// passwords also need local replacement because secretmask excludes short values.
func acceptanceExecutionMask(environ []string) func(string) string {
	return acceptanceExecutionMaskWith(environ, nil)
}

// acceptanceExecutionMaskWith also masks a declared set of values regardless
// of the variable names that carry them: brokered runtime-input secrets
// (ADR-0169), with their URL and path escapes.
func acceptanceExecutionMaskWith(environ []string, declared []string) func(string) string {
	masker := secretmask.NewFromEnv(environ)
	var credentials []string
	for _, value := range declared {
		if value != "" {
			credentials = append(credentials, value, url.QueryEscape(value), url.PathEscape(value))
		}
	}
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" {
			continue
		}
		upper := strings.ToUpper(key)
		connection := strings.HasSuffix(upper, "URL") || strings.HasSuffix(upper, "DSN")
		generation := key == brand.EnvName("AGENT_GENERATION") || key == brand.LegacyEnvName("AGENT_GENERATION")
		if connection || generation || secretmask.IsSecretKey(key) {
			credentials = append(credentials, value)
		}
		if !connection {
			continue
		}
		if parsed, err := url.Parse(value); err == nil && parsed.User != nil {
			if password, present := parsed.User.Password(); present && password != "" {
				credentials = append(credentials, password, url.QueryEscape(password), url.PathEscape(password))
			}
		}
		for _, match := range acceptanceDSNPassword.FindAllStringSubmatch(value, -1) {
			for _, password := range match[1:] {
				if password != "" {
					credentials = append(credentials, password, acceptanceDSNEscape.ReplaceAllString(password, "$1"))
				}
			}
		}
	}
	// Feed known connection values to the shared masker too. The local pass
	// additionally handles short passwords without weakening global defaults.
	entries := make([]string, len(credentials))
	for i, value := range credentials {
		entries[i] = "SECRET_ACCEPTANCE=" + value
	}
	masker.AddEntries(entries)
	sort.Slice(credentials, func(i, j int) bool { return len(credentials[i]) > len(credentials[j]) })
	replacements := make([]string, 0, 2*len(credentials))
	for _, credential := range credentials {
		replacements = append(replacements, credential, "***")
	}
	replacer := strings.NewReplacer(replacements...)
	return func(text string) string {
		return replacer.Replace(masker.MaskText(text))
	}
}
