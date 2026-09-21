package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/spf13/cobra"
)

// The usage commands are read-only: they read state and the usage store and
// mutate nothing, so like get they carry no RBAC entry and no agent identity.
var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Report provider token usage by task outcome",
}

var usageReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Attribute provider usage to terminal task outcomes",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		opts := commands.UsageReportOptions{Internal: isJSON(cmd)}
		opts.Format, _ = cmd.Flags().GetString("format")
		opts.Since, _ = cmd.Flags().GetString("since")
		opts.Until, _ = cmd.Flags().GetString("until")
		opts.AsOf, _ = cmd.Flags().GetString("as-of")
		opts.Role, _ = cmd.Flags().GetString("role")
		opts.TaskID, _ = cmd.Flags().GetString("task")
		opts.BaselineSince, _ = cmd.Flags().GetString("baseline-since")
		opts.BaselineUntil, _ = cmd.Flags().GetString("baseline-until")

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		opts.ProjectRoot = projectRoot

		result, err := commands.UsageReportCommand(opts)
		if err != nil {
			return err // deferred guard handles JSON
		}
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, nil)
		}
		cmd.Print(result)
		return nil
	},
}

func usageLong() string {
	return fmt.Sprintf(`Report provider token usage recorded by %s supervisors.

Records are joined to durable task history when the report is built, so the
same records read at a later time reflect later lifecycle events.

Examples:
  %s`, brand.NameTitle, brand.Command("usage", "report", "--json"))
}

func usageReportLong() string {
	return fmt.Sprintf(`Attribute provider token usage to the outcome each task reached.

The report shows, per outcome class and role, the total, median and p95 fresh,
cache-read and output tokens; cache-read tokens per merged task with the
cache-hit percentage; tokens spent after each task's last useful transition,
by role and failure category; provenance counts for unknown, partial and
conflicting records; and the lifecycle counter rows that show suppressed
duplicate and preflight-rejected calls. An unavailable store or counter file
is reported as a warning, never as zero.

The window defaults to the current sprint: from its start until it ended, or
now. Time bounds accept RFC 3339 timestamps or YYYY-MM-DD dates (UTC).
--as-of reads task outcomes as they stood at that point, so a reading can be
reproduced after later reconciliation. A baseline window renders a second
report over the same records and the deltas between the two.

Examples:
  %[1]s --json
  %[1]s --since 2026-09-07 --until 2026-09-10 --role coder
  %[1]s --task task-1 --as-of 2026-09-09T12:00:00Z --format yaml
  %[1]s --baseline-since 2026-09-01 --baseline-until 2026-09-06 --json`,
		brand.Command("usage", "report"))
}

func init() {
	usageCmd.Long = usageLong()
	usageReportCmd.Long = usageReportLong()
	rootCmd.AddCommand(usageCmd)
	usageCmd.AddCommand(usageReportCmd)
	usageReportCmd.Flags().String("format", "table", "output format: json, yaml, table, or value")
	usageReportCmd.Flags().String("since", "", "window start (RFC 3339 or YYYY-MM-DD); defaults to the sprint start")
	usageReportCmd.Flags().String("until", "", "window end (RFC 3339 or YYYY-MM-DD); defaults to the sprint end or now")
	usageReportCmd.Flags().String("as-of", "", "read task outcomes as of this time; defaults to --until")
	usageReportCmd.Flags().String("role", "", "only records of this role")
	usageReportCmd.Flags().String("task", "", "only records of this task")
	usageReportCmd.Flags().String("baseline-since", "", "baseline window start for a before/after comparison")
	usageReportCmd.Flags().String("baseline-until", "", "baseline window end for a before/after comparison")
	addJSONFlag(usageReportCmd)
}
