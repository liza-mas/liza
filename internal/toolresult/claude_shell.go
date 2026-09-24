package toolresult

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var claudeReservedShell = regexp.MustCompile(`(^|[;|&\n])\s*trap(?:\s|$)|(?:198|199)[<>]|__toolresult_`)

// ClaudeShellCommand captures in the native shell, not a child interpreter, so
// cwd, shell functions and aliases retain their native semantics. EXIT traps
// and fallback Bash 3 file descriptors are reserved for capture. Explicit use
// is refused rather than silently changing the command. Unsupported dynamic
// trap replacement fails closed when the collector finds no status record.
func ClaudeShellCommand(command string, collector []string, metadata Result) (string, error) {
	if claudeReservedShell.MatchString(command) {
		return "", fmt.Errorf("command uses shell state reserved for tool-result capture (EXIT trap or descriptors 198/199)")
	}
	if len(collector) == 0 {
		return "", fmt.Errorf("missing Claude capture executable")
	}
	metadata.Tool, metadata.Command, metadata.Content = "Bash", command, ""
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	token := hex.EncodeToString(nonce)
	payload, err := json.Marshal(struct {
		Result
		Delimiter string `json:"capture_delimiter"`
	}{metadata, token})
	if err != nil {
		return "", err
	}
	quoted := make([]string, len(collector))
	for i, v := range collector {
		quoted[i] = claudeShellQuote(v)
	}
	return strings.NewReplacer("__CAPTURE__", strings.Join(quoted, " "), "__METADATA__", claudeShellQuote(string(payload)), "__COMMAND__", claudeShellQuote(command), "__DELIMITER__", token).Replace(claudeCaptureTemplate), nil
}

func claudeShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// ReadClaudeCapture drains a FIFO before consulting the numeric status file.
// No raw regular file is written. Missing status (for example an overridden
// EXIT trap) is an explicit error, never a fallback returning original output.
func ReadClaudeCapture(input io.Reader, statusPath string) (Result, error) {
	reader := bufio.NewReader(input)
	line, err := reader.ReadString('\n')
	if err != nil {
		return Result{}, fmt.Errorf("missing Claude capture metadata")
	}
	var result Result
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		return Result{}, fmt.Errorf("invalid Claude capture metadata")
	}
	var framing struct {
		Delimiter string `json:"capture_delimiter"`
	}
	if err := json.Unmarshal([]byte(line), &framing); err != nil || len(framing.Delimiter) != 64 {
		return Result{}, fmt.Errorf("invalid Claude capture framing")
	}
	marker := []byte("\x00TOOLRESULT_" + framing.Delimiter + "\x00")
	var content []byte
	chunk := make([]byte, 32768)
	for {
		n, readErr := reader.Read(chunk)
		content = append(content, chunk[:n]...)
		if pos := bytes.Index(content, marker); pos >= 0 {
			content = content[:pos]
			break
		}
		if readErr != nil {
			return Result{}, fmt.Errorf("claude output capture failed: completion frame unavailable")
		}
	}
	status, err := os.ReadFile(statusPath)
	if err != nil {
		return Result{}, fmt.Errorf("claude output capture failed: exit status unavailable")
	}
	code, err := strconv.Atoi(string(status))
	if err != nil || code < 0 || code > 255 {
		return Result{}, fmt.Errorf("invalid Claude capture exit status")
	}
	result.Content, result.ExitCode = string(content), &code
	return result, nil
}

// CleanupClaudeCapture removes only the named FIFO and numeric metadata from a
// private capture directory. It never recursively removes arbitrary contents.
func CleanupClaudeCapture(dir string) {
	if !strings.HasPrefix(filepath.Base(dir), "toolresult.") {
		return
	}
	_ = os.Remove(filepath.Join(dir, "output"))
	_ = os.Remove(filepath.Join(dir, "status"))
	_ = os.Remove(filepath.Join(dir, "ready"))
	_ = os.Remove(filepath.Join(dir, "ack"))
	_ = os.Remove(dir)
}

// DrainClaudeBackground keeps inherited stdout valid for detached child jobs
// without holding the tool's model-facing descriptors or delaying its return.
func DrainClaudeBackground(dir string, input io.Reader) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "ack")); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _ = io.Copy(io.Discard, input)
}

const claudeCaptureTemplate = `__toolresult_d=$(mktemp -d "${TMPDIR:-/tmp}/toolresult.XXXXXX") || exit 125
mkfifo "$__toolresult_d/output" || exit 125
__CAPTURE__ "$__toolresult_d" &
__toolresult_pid=$!
if [ -n "${ZSH_VERSION:-}" ] || [ "${BASH_VERSINFO[0]:-0}" -ge 4 ]; then
 eval 'exec {__toolresult_out}>&1 {__toolresult_err}>&2'
else
 exec 198>&1 199>&2
 __toolresult_out=198; __toolresult_err=199
fi
__toolresult_finish() {
 trap - EXIT
 printf '%s' "$1" > "$__toolresult_d/status"
 printf '\000TOOLRESULT___DELIMITER__\000'
 eval "exec 1>&$__toolresult_out 2>&$__toolresult_err"
 if [ -n "${ZSH_VERSION:-}" ] || [ "${BASH_VERSINFO[0]:-0}" -ge 4 ]; then
  eval 'exec {__toolresult_out}>&- {__toolresult_err}>&-'
 else
  exec 198>&- 199>&-
 fi
 while [ ! -f "$__toolresult_d/ready" ]; do
  if ! kill -0 "$__toolresult_pid" 2>/dev/null; then
   printf '%s\n' 'Tool-result boundary failed: captured output withheld.' >&2
   exit 125
  fi
  sleep 0.01
 done
 __toolresult_filter=$(cat "$__toolresult_d/ready")
 : > "$__toolresult_d/ack"
 if [ "$__toolresult_filter" -ne 0 ]; then exit 125; fi
}
trap '__toolresult_finish "$?"' EXIT
exec 1>"$__toolresult_d/output" 2>&1
printf '%s\n' __METADATA__
eval __COMMAND__
__toolresult_code=$?
__toolresult_finish "$__toolresult_code"
trap - EXIT
(exit "$__toolresult_code")
`
