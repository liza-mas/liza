package interactive

import (
	"bufio"

	"github.com/liza-mas/liza/internal/termutil"
)

// IsInteractive returns true if stdin is connected to a terminal.
func IsInteractive() bool {
	return termutil.IsInteractive()
}

// ReadSingleKey reads a single keypress from stdin without requiring Enter.
// Returns the lowercase key character. Falls back to ReadString('\n') if terminal is not available.
func ReadSingleKey(reader *bufio.Reader) (string, error) {
	return termutil.ReadSingleKey(reader)
}
