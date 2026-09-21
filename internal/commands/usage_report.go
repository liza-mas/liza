package commands

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/render"
	"github.com/liza-mas/liza/internal/usage"
)

// UsageReportOptions contains options for the usage report command. Time
// bounds are RFC 3339 timestamps or YYYY-MM-DD dates (UTC midnight); empty
// primary bounds default to the current sprint timeline. Either baseline bound
// makes a baseline window.
type UsageReportOptions struct {
	ProjectRoot   string // Project root directory
	Since         string // Primary window start
	Until         string // Primary window end
	AsOf          string // Outcome reading point, defaults to Until
	Role          string // Only records of this role
	TaskID        string // Only records of this task
	BaselineSince string // Baseline window start
	BaselineUntil string // Baseline window end
	Format        string // Output format: json, yaml, table, value
	Internal      bool   // If true, return the usage.Report for composition (not formatted string)

	window   usage.Window
	baseline *usage.Window
}

// Validate checks the format and parses every time bound before any read.
func (opts *UsageReportOptions) Validate() error {
	validFormats := []string{"json", "yaml", "table", "value", ""}
	if !slices.Contains(validFormats, opts.Format) {
		return fmt.Errorf("invalid format: %s (must be json, yaml, table, or value)", opts.Format)
	}
	var baseline usage.Window
	bounds := []timeBound{
		{"--since", opts.Since, &opts.window.Since},
		{"--until", opts.Until, &opts.window.Until},
		{"--as-of", opts.AsOf, &opts.window.AsOf},
		{"--baseline-since", opts.BaselineSince, &baseline.Since},
		{"--baseline-until", opts.BaselineUntil, &baseline.Until},
	}
	for _, b := range bounds {
		if b.value == "" {
			continue
		}
		parsed, err := parseUsageTime(b.value)
		if err != nil {
			return &errors.ValidationError{Message: fmt.Sprintf("%s: %v", b.flag, err)}
		}
		*b.into = parsed
	}
	if err := checkWindowOrder(opts.window, "--since", "--until"); err != nil {
		return err
	}
	if opts.BaselineSince != "" || opts.BaselineUntil != "" {
		if err := checkWindowOrder(baseline, "--baseline-since", "--baseline-until"); err != nil {
			return err
		}
		opts.baseline = &baseline
	}
	return nil
}

// timeBound pairs one flag's raw value with the window field it fills.
type timeBound struct {
	flag  string
	value string
	into  *time.Time
}

func parseUsageTime(value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", value, time.UTC); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q is not an RFC 3339 timestamp or a YYYY-MM-DD date", value)
}

func checkWindowOrder(w usage.Window, sinceFlag, untilFlag string) error {
	if !w.Since.IsZero() && !w.Until.IsZero() && w.Since.After(w.Until) {
		return &errors.ValidationError{Message: fmt.Sprintf("%s must not be after %s", sinceFlag, untilFlag)}
	}
	return nil
}

// UsageReportCommand reads state, loads the usage store once over the union
// of the primary and baseline windows and builds the report. It mutates
// nothing. With Internal set it returns the usage.Report; otherwise the
// rendered string.
func UsageReportCommand(opts UsageReportOptions) (any, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	state, err := db.For(paths.New(opts.ProjectRoot).StatePath()).Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	window := opts.window
	if window.Since.IsZero() {
		window.Since = state.Sprint.Timeline.Started
	}
	if window.Until.IsZero() {
		// A running sprint reads up to now, so the reading point is recorded
		// as as_of rather than left open.
		window.Until = time.Now().UTC()
		if state.Sprint.Timeline.Ended != nil {
			window.Until = *state.Sprint.Timeline.Ended
		}
	}

	loadSince, loadUntil := window.Since, window.Until
	if opts.baseline != nil {
		loadSince = earliest(loadSince, opts.baseline.Since)
		loadUntil = latest(loadUntil, opts.baseline.Until)
	}
	records, stats, err := usage.Load(opts.ProjectRoot, loadSince, loadUntil)
	if err != nil {
		return nil, err
	}
	records = slices.DeleteFunc(records, func(r usage.Record) bool {
		return (opts.Role != "" && r.Role != opts.Role) || (opts.TaskID != "" && r.TaskID != opts.TaskID)
	})

	report, err := usage.Build(usage.Input{
		Records:   records,
		LoadStats: stats,
		State:     state,
		Counters:  state.Sprint.Metrics.LifecycleOutcomes,
		Window:    window,
		Baseline:  opts.baseline,
	})
	if err != nil {
		return nil, err
	}
	if opts.Internal {
		return report, nil
	}
	return formatUsageReport(report, opts.Format)
}

// earliest returns the earlier bound, where a zero bound is open (earliest).
func earliest(a, b time.Time) time.Time {
	if a.IsZero() || b.IsZero() {
		return time.Time{}
	}
	if b.Before(a) {
		return b
	}
	return a
}

// latest returns the later bound, where a zero bound is open (latest).
func latest(a, b time.Time) time.Time {
	if a.IsZero() || b.IsZero() {
		return time.Time{}
	}
	if b.After(a) {
		return b
	}
	return a
}

func formatUsageReport(report usage.Report, format string) (string, error) {
	if format == "" {
		format = "table"
	}
	if format != "table" {
		return formatOutput(report, format)
	}
	headers := []string{"OUTCOME", "ROLE", "TASKS", "FRESH", "FRESH P50", "FRESH P95", "CACHE READ", "CACHE P50", "CACHE P95", "OUTPUT"}
	var rows [][]string
	for _, row := range report.Outcomes {
		rows = append(rows, []string{
			string(row.Outcome), row.Role, strconv.Itoa(row.Tasks),
			strconv.Itoa(row.FreshTokens.Total), strconv.Itoa(row.FreshTokens.Median), strconv.Itoa(row.FreshTokens.P95),
			strconv.Itoa(row.CacheReadTokens.Total), strconv.Itoa(row.CacheReadTokens.Median), strconv.Itoa(row.CacheReadTokens.P95),
			strconv.Itoa(row.OutputTokens.Total),
		})
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Window: %s .. %s (as of %s)\n",
		report.Window.Since.Format(time.RFC3339), report.Window.Until.Format(time.RFC3339), report.Window.AsOf.Format(time.RFC3339))
	if len(rows) > 0 {
		out.WriteString(render.FormatTable(headers, rows))
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&out, "warning: %s\n", warning)
	}
	return out.String(), nil
}
