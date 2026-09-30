package commands

import (
	"os"
	"testing"
)

// TestMain makes init's pre-commit executable check hermetic: developer shells
// usually have pre-commit on PATH and CI runners do not. Tests of the check
// itself install their own lookup.
func TestMain(m *testing.M) {
	restore := SetInitPreCommitLookPathForTest(func(name string) (string, error) { return "/stub/" + name, nil })
	code := m.Run()
	restore()
	os.Exit(code)
}
