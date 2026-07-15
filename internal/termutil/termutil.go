package termutil

import (
	"bufio"
	"os"
	"strings"
)

// IsInteractive returns true if stdin is connected to a terminal.
func IsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ReadSingleKey reads a CLI confirmation response and returns its lowercase first character.
// CLI confirmations remain line-based so familiar inputs such as "y<Enter>" and "yes<Enter>"
// are consumed completely and cannot leak into a subsequent prompt. TUI confirmations handle
// single-key input in their own event loop.
func ReadSingleKey(reader *bufio.Reader) (string, error) {
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
