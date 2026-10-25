package main

import (
	"fmt"
	"io"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/spf13/cobra"
)

var useSystemPromptCmd = &cobra.Command{
	Use:   "use-system-prompt",
	Short: "Deliver the agent contracts in the CLI system prompt instead of forced reads (opt-in)",
	Long: fmt.Sprintf(`Opt this project into system-prompt mode.

Builds the contract files under %[2]s/system-prompts/ from the installed
contracts (multi_agent.md, pairing.md, subagent.md) and writes the marker
%[2]s/system-prompt-mode.json. Hook configuration is not edited; installed
enforce-init.sh/session-context.sh scripts older than this binary are refreshed.

Multi-agent sessions launched on claude or codex then receive multi_agent.md in
their system prompt, run with %[3]s=1 (which lets them past the
enforce-init gate) and skip the contract-read turns; the shared prefix lets
every agent of a burst reuse the provider prompt cache. Other providers, ACPX
launches and pairing sessions keep the gate and read the contracts. Re-running
refreshes the files.

Use --revert to remove the files and the marker.`, brand.NameTitle, brand.RuntimeValues().ProjectDirName, brand.EnvName("SYSTEM_PROMPT_CONTRACTS")),
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		revert, _ := cmd.Flags().GetBool("revert")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		out := cmd.OutOrStdout()
		if revert {
			report, err := embedded.DisableSystemPromptMode(projectRoot, dryRun)
			if err != nil {
				return err
			}
			if !report.WasEnabled {
				fmt.Fprintln(out, "System-prompt mode is not enabled; nothing to revert.")
				return nil
			}
			printSystemPromptReport(out, dryRun, "removed", report)
			return nil
		}
		report, err := embedded.EnableSystemPromptMode(projectRoot, dryRun)
		if err != nil {
			return err
		}
		printSystemPromptReport(out, dryRun, "written", report)
		for i, mode := range embedded.SystemPromptModes {
			fmt.Fprintf(out, "  %s: %d bytes (%s)\n", mode, len(report.FileContents[mode]), report.Files[i])
		}
		return nil
	},
}

func printSystemPromptReport(out io.Writer, dryRun bool, fileVerb string, report embedded.SystemPromptModeReport) {
	prefix := ""
	if dryRun {
		prefix = "[dry-run, nothing written] "
	}
	fmt.Fprintf(out, "%ssystem prompt files %s: %d\n", prefix, fileVerb, len(report.Files))
	fmt.Fprintf(out, "%smarker %s: %s\n", prefix, fileVerb, report.MarkerPath)
	for _, path := range report.RefreshedHooks {
		fmt.Fprintf(out, "%sgate hook refreshed to this binary's version: %s\n", prefix, path)
	}
}

func init() {
	rootCmd.AddCommand(useSystemPromptCmd)
	useSystemPromptCmd.Flags().Bool("revert", false, "remove the system prompt files and marker (forced reads resume)")
	useSystemPromptCmd.Flags().Bool("dry-run", false, "report what would change without writing")
}
