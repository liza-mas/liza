package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/spf13/cobra"
)

// refreshAssetCheck reports a release-owned project file refresh-project
// wrote (ok) or left alone (failed), per specs/protocols/outcome-checks.md.
var refreshAssetCheck = jsonout.CheckDefinition{ID: "refresh.asset", Severity: jsonout.SeverityWarn}

var refreshProjectCmd = &cobra.Command{
	Use:   "refresh-project",
	Short: fmt.Sprintf("Refresh release-owned %s files installed in this project", brand.NameTitle),
	Long: fmt.Sprintf(`Refresh the release-owned files init installed in the current repository,
the project counterpart of refresh-global. The repository is the one holding
the working directory; in a linked worktree, that worktree.

For each agent whose hook config registers the %[1]s gate hook the way init
writes it (.claude/settings.json, .codex/hooks.json, .github/hooks/%[2]s),
the hook scripts under .claude/hooks/, .codex/hooks/ or .copilot/hooks/ are
brought to this release: missing scripts are added and outdated or edited ones
replaced. Every file written is listed. A hook script that is a symlink or not
a regular file is left alone and named in a warning.

When the project uses system-prompt mode, the files under
%[3]s/system-prompts/ and the marker are rebuilt as use-system-prompt builds
them.

Hook configs, settings, contract links and every other file are never written,
so a hook script new in this release is installed but not registered until
init runs again. In a directory init never prepared nothing is written. It is
safe to run non-interactively after a release update.

With --json, stdout carries one outcome envelope: an ok refresh.asset check per
file written and a failed one per file left alone. Exit 4 when a file was left
alone, 1 on a failure.`, brand.NameTitle, embedded.CopilotHooksFileName(), brand.RuntimeValues().ProjectDirName),
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithOutcome(cmd, "refresh-project", func(report *jsonout.Report) error {
			return runRefreshProject(cmd, report)
		})
	},
}

// runRefreshProject refreshes the project holding the working directory and
// records a check per release-owned file it wrote or left alone.
func runRefreshProject(cmd *cobra.Command, report *jsonout.Report) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	// The working tree's own root: a linked worktree holds its own hooks.
	toplevel, err := gitenv.Output(".", "rev-parse", "--show-toplevel")
	if err != nil {
		// init cannot have installed anything without a repository root.
		fmt.Fprintf(out, "not in a git repository; no %s project assets refreshed\n", brand.NameTitle)
		return nil
	}
	projectRoot := strings.TrimSpace(string(toplevel))
	retry := fmt.Sprintf("re-run %s", brand.Command("refresh-project"))

	hooks, err := embedded.RefreshInstalledHooks(projectRoot)
	for _, path := range hooks.MalformedConfigs {
		message := fmt.Sprintf("cannot parse %s; its hook scripts were not refreshed", path)
		fmt.Fprintf(errOut, "Warning: %s\n", message)
		report.Add(refreshAssetCheck.Check(jsonout.CheckFailed, "", path, message, fmt.Sprintf("repair the JSON in %s, then %s", path, retry)))
	}
	for _, path := range hooks.Skipped {
		message := fmt.Sprintf("left %s alone: not a regular file", path)
		fmt.Fprintf(errOut, "Warning: %s\n", message)
		report.Add(refreshAssetCheck.Check(jsonout.CheckFailed, "", path, message, fmt.Sprintf("move %s aside, then %s", path, retry)))
	}
	bundlesEnabled := embedded.SystemPromptModeEnabled(projectRoot)
	var bundles []string
	if err == nil && bundlesEnabled {
		bundles, err = embedded.RebuildSystemPromptBundles(projectRoot)
	}
	if err == nil && !hooks.Installed && !bundlesEnabled {
		fmt.Fprintf(out, "no %s project assets in %s; nothing refreshed\n", brand.NameTitle, projectRoot)
		return nil
	}

	// Listed on failure too: files written before it are not silently replaced.
	refreshed := slices.Concat(hooks.Refreshed, bundles)
	if err == nil || len(refreshed) > 0 {
		fmt.Fprintf(out, "refreshed %d release-owned project assets in %s\n", len(refreshed), projectRoot)
		for _, path := range refreshed {
			fmt.Fprintf(out, "  %s\n", path)
			report.Add(refreshAssetCheck.Check(jsonout.CheckOK, "", path, "refreshed to this release", ""))
		}
	}
	if err != nil {
		return refreshProjectError{err}
	}
	return nil
}

// refreshProjectError carries a refresh failure into the --json envelope,
// whose error.message the launcher shows. Untyped errors there read only
// "internal error"; this message is the path and filesystem error text, the
// same content outcome checks carry (specs/protocols/outcome-checks.md).
type refreshProjectError struct{ err error }

func (e refreshProjectError) Error() string { return e.err.Error() }

func (e refreshProjectError) Unwrap() error { return e.err }

func (e refreshProjectError) SafeClassification() (string, string) {
	return "refresh_failed", e.err.Error()
}

func init() {
	addJSONFlag(refreshProjectCmd)
	rootCmd.AddCommand(refreshProjectCmd)
}
