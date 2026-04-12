package main

import (
	"testing"

	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/spf13/cobra"
)

func TestAddJSONFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	addJSONFlag(cmd)

	f := cmd.Flags().Lookup("json")
	if f == nil {
		t.Fatal("--json flag not registered")
	}
	if f.DefValue != "false" {
		t.Errorf("expected default value 'false', got %q", f.DefValue)
	}
}

func TestIsJSON_DefaultFalse(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	addJSONFlag(cmd)

	if isJSON(cmd) {
		t.Error("expected isJSON to return false by default")
	}
}

func TestIsJSON_TrueWhenSet(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	addJSONFlag(cmd)

	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if !isJSON(cmd) {
		t.Error("expected isJSON to return true after setting --json")
	}
}

func TestIsJSON_WithoutFlagRegistered(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	// No addJSONFlag call — isJSON should return false, not panic.
	if isJSON(cmd) {
		t.Error("expected isJSON to return false when flag not registered")
	}
}

func TestErrAlreadyWritten_SuppressesStderr(t *testing.T) {
	// Verify that when a subcommand returns ErrAlreadyWritten,
	// rootCmd.Execute() propagates the error (which main() catches
	// and uses to skip stderr output).
	root := &cobra.Command{
		Use:           "test-root",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(&cobra.Command{
		Use: "fail-json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return jsonout.ErrAlreadyWritten
		},
	})

	root.SetArgs([]string{"fail-json"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error from Execute")
	}
	if err != jsonout.ErrAlreadyWritten {
		t.Errorf("expected ErrAlreadyWritten, got %v", err)
	}
}
