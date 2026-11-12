package main

import (
	"fmt"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/spf13/cobra"
)

// doctorCmd checks what setup and init produced, as outcome checks
// (specs/protocols/outcome-checks.md). It never writes: the caller decides
// what to do with the report.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: fmt.Sprintf("Check what %s setup and init produced", brand.NameTitle),
	Long: fmt.Sprintf(`Check that the files and links %[1]s setup and init produce are in place,
without changing anything.

Global checks always run: the files in ~/%[2]s, the skill links of every
provider set up, the provider catalog cache, and the tools of the configured
toolchain profile. --provider adds that provider's skill links and its global
contract; the contract of a provider not named is never checked. --repo adds
the current repository: where each named provider's contract is active, the
activation state, the provider settings and hooks, and the index hooks.

Exit code 0 when every check is ok, 4 when any check is not, 1 when the checks
cannot run.`, brand.NameTitle, brand.GlobalDirName),
	Args: cobra.NoArgs,
	// A command's own hook replaces the root's, which can write (workspace
	// report replay); doctor needs no project and must not write.
	PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	RunE: func(cmd *cobra.Command, _ []string) error {
		providerIDs, _ := cmd.Flags().GetStringArray("provider")
		repo, _ := cmd.Flags().GetBool("repo")
		return runWithOutcome(cmd, "doctor", func(report *jsonout.Report) error {
			opts := commands.DoctorOptions{SkillProviders: providerIDs, ContractProviders: providerIDs, Toolchain: true}
			if repo {
				lizaPaths, err := paths.LizaPathsFromGit()
				if err != nil {
					return fmt.Errorf("--repo needs a git repository: %w", err)
				}
				opts.ProjectRoot = lizaPaths.ProjectRoot()
			}
			if err := commands.Doctor(opts, report); err != nil {
				return err
			}
			if !isJSON(cmd) {
				fmt.Fprintf(cmd.OutOrStdout(), "doctor: %d checks, %d not ok\n", len(report.Checks), len(report.NotOK()))
			}
			return nil
		})
	},
}

func init() {
	doctorCmd.Flags().StringArray("provider", nil, "provider id whose contract to check (repeatable)")
	doctorCmd.Flags().Bool("repo", false, "also check the current git repository")
	addJSONFlag(doctorCmd)
	rootCmd.AddCommand(doctorCmd)
}
