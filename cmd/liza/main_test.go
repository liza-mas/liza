package main

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/spf13/cobra"
)

func TestCheckSupportedPlatformAllowsAllSupportedPlatforms(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			if err := checkSupportedPlatform(goos); err != nil {
				t.Fatalf("checkSupportedPlatform(%q) = %v, want nil", goos, err)
			}
		})
	}
}

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

func TestValidateReasonFlag(t *testing.T) {
	tests := []struct {
		name        string
		reason      string
		explicit    bool
		wantMatched string
		wantErr     bool
	}{
		{name: "unchanged default", reason: "manual deletion"},
		{name: "ordinary prose", reason: "missing error handling", explicit: true},
		{name: "multiline markdown", reason: "---\n# Blockers\nMissing tests", explicit: true},
		{name: "unregistered flag-shaped prose is a deliberate boundary", reason: "--unregistered", explicit: true},
		{name: "local long flag", reason: "--agent-id", explicit: true, wantMatched: "--agent-id", wantErr: true},
		{name: "local long flag with value", reason: "--agent-id=code-reviewer-1", explicit: true, wantMatched: "--agent-id", wantErr: true},
		{name: "inherited long flag", reason: "--project-root", explicit: true, wantMatched: "--project-root", wantErr: true},
		{name: "registered shorthand acknowledged compatibility cost", reason: "-C", explicit: true, wantMatched: "-C", wantErr: true},
		{name: "registered shorthand with value", reason: "-C=/tmp/project", explicit: true, wantMatched: "-C", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := &cobra.Command{Use: "test-root"}
			root.PersistentFlags().StringP("project-root", "C", "", "project root")
			cmd := &cobra.Command{Use: "mutate"}
			cmd.Flags().String("reason", "manual deletion", "mutation reason")
			cmd.Flags().String("agent-id", "", "agent identifier")
			root.AddCommand(cmd)

			if tt.explicit {
				if err := cmd.Flags().Set("reason", tt.reason); err != nil {
					t.Fatalf("set --reason: %v", err)
				}
			}

			matched, err := validateReasonFlag(cmd)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateReasonFlag() error = %v, wantErr %v", err, tt.wantErr)
			}
			if matched != tt.wantMatched {
				t.Fatalf("validateReasonFlag() matched = %q, want %q", matched, tt.wantMatched)
			}
			if err != nil {
				for _, want := range []string{"matches registered flag", "empty shell expansion", `--reason="$reason"`, "Only registered flag tokens are detected"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("error = %q, want substring %q", err, want)
					}
				}
			}
		})
	}
}

func TestReasonCommandsInheritRootValidationHook(t *testing.T) {
	if rootCmd.PersistentPreRunE == nil {
		t.Fatal("root command has no persistent CLI validation hook")
	}

	const wantReasonCommands = 18
	reasonCommands := 0
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			if cmd.Flags().Lookup("reason") != nil {
				reasonCommands++
				for ancestor := cmd; ancestor != nil && ancestor != rootCmd; ancestor = ancestor.Parent() {
					if ancestor.PersistentPreRun != nil || ancestor.PersistentPreRunE != nil {
						t.Errorf("%s shadows root CLI validation with a persistent pre-run hook", cmd.CommandPath())
					}
				}
			}
			walk(cmd)
		}
	}
	walk(rootCmd)

	if reasonCommands != wantReasonCommands {
		t.Fatalf("commands with --reason = %d, want %d; review the root validation policy for the new registration", reasonCommands, wantReasonCommands)
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
