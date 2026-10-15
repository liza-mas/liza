//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdmitCLIRunsAMatchingCommandAndReturnsItsExitCode(t *testing.T) {
	root := setupAdmissionLimitCLI(t)
	if err := executeRootCommand(t, root, "admission-limit", "set", "heavy", "--max-concurrent", "1", "--match", "heavy"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")

	err := executeRootCommand(t, root, "admit", "--max-wait", "5", "--", "sh", "-c", `echo > "$1"; exit 4`, "heavy", marker)

	var exit *toolResultExit
	if !errors.As(err, &exit) || exit.code != 4 {
		t.Fatalf("err=%v, want the command's exit code 4", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("matching command did not run: %v", statErr)
	}
}

func TestAdmitCLIRefusesAWaitOverTheCap(t *testing.T) {
	root := setupAdmissionLimitCLI(t)
	err := executeRootCommand(t, root, "admit", "--max-wait", "14401", "--", "true")
	if err == nil || !strings.Contains(err.Error(), "between 0 and 14400 seconds") {
		t.Fatalf("err=%v, want the wait bound refusal", err)
	}
}
