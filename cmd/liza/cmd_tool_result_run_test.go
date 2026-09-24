package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestToolResultRunCLIExitAndStdin(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(newToolResultCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetIn(strings.NewReader("specific bounded read\n"))
	root.SetArgs([]string{"tool-result", "--root", t.TempDir(), "run", "--command", "cat; exit 7", "--", "sh", "-c", "cat; exit 7"})
	err := root.Execute()
	var exit *toolResultExit
	if !errors.As(err, &exit) || exit.code != 7 || out.String() != "specific bounded read\n" {
		t.Fatalf("output=%q err=%v", out.String(), err)
	}
}
