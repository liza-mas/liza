package termutil

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/term"
)

// IsInteractive returns true if stdin is connected to a terminal.
func IsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ReadSingleKey reads a single keypress from stdin without requiring Enter.
// Returns the lowercase key character. Falls back to ReadString('\n') if terminal is not available.
// Note: In raw mode, only the first character is read. If user types "yes<Enter>" instead of "y",
// the trailing "es\n" will be consumed by the next stdin read in the same command execution.
// This is acceptable for confirmation prompts since the user's intent was already clear from
// the first character, and subsequent prompts will ignore the extra input.
func ReadSingleKey(reader *bufio.Reader) (string, error) {
	if IsInteractive() {
		// Terminal is available - use raw mode for single-key input
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			// Fall back to line-based input if raw mode fails
			return ReadLineKey(reader)
		}
		defer term.Restore(int(os.Stdin.Fd()), oldState)

		// Read single byte
		b := make([]byte, 1)
		n, err := os.Stdin.Read(b)
		if err != nil || n != 1 {
			return "", err
		}
		return strings.ToLower(string(b[0])), nil
	}

	// Non-interactive or terminal not available - fall back to line-based input
	return ReadLineKey(reader)
}

// readLineKey reads a line of input and extracts the first character.
// Used as fallback when raw mode is not available.
// Exported for testing.
func ReadLineKey(reader *bufio.Reader) (string, error) {
	response, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	response = strings.TrimSpace(strings.ToLower(response))
	if len(response) > 0 {
		return string(response[0]), nil
	}
	return "", nil
}
