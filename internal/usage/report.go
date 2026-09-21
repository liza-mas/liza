package usage

// This file joins usage records to durable task history and builds the usage
// report, including the before/after comparison and the compact summary.
// Only terminal_authoritative records enter token totals; other provenance is
// counted, never folded in. Unavailable sources are reported as absent blocks
// plus warnings, never as zeros.

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// postTransitionDetailLimit bounds the per-task detail of one post-transition
// row; the largest tails are kept and the rest counted in TasksOmitted.
const postTransitionDetailLimit = 20

// Window bounds a report reading. Records are selected by start time within
// [Since, Until] (a zero bound is open); outcomes are read at AsOf, which
// defaults to Until.
type Window struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
	AsOf  time.Time `json:"as_of"`
}

// Input is everything Build reads. Records and LoadStats come from one Load
// over the union of Window and Baseline, so identity dedup spans both windows.
// Counters is the lifecycle counter projection supplied by the caller.
type Input struct {
	Records   []Record
	LoadStats LoadStats
	State     *models.State
	Counters  *models.LifecycleOutcomeMetrics
	Window    Window
	Baseline  *Window
}

// Distribution summarizes one token dimension over the units of a row: tasks,
// or individual records for unattributed usage. Median and P95 use the
// nearest-rank method, so both are observed unit values.
type Distribution struct {
	Total  int `json:"total"`
	Median int `json:"median"`
	P95    int `json:"p95"`
}

// OutcomeRow is the authoritative usage of one role on tasks of one outcome class.
type OutcomeRow struct {
	Outcome         OutcomeClass `json:"outcome"`
	Role            string       `json:"role"`
	Tasks           int          `json:"tasks"`
	Records         int          `json:"records"`
	FreshTokens     Distribution `json:"fresh_tokens"`
	CacheReadTokens Distribution `json:"cache_read_tokens"`
	OutputTokens    Distribution `json:"output_tokens"`
}

// PerCompletedTask relates cache reuse to delivered work. A ratio is nil when
// its denominator is zero. CacheHitPercent is cache-read over fresh plus
// cache-read input across every authoritative record of the window.
type PerCompletedTask struct {
	MergedTasks                  int      `json:"merged_tasks"`
	CacheReadTokensPerMergedTask *float64 `json:"cache_read_tokens_per_merged_task"`
	CacheHitPercent              *float64 `json:"cache_hit_percent"`
}

// PostTransitionRow is usage started after the task's last useful transition,
// by role and failure category. Tokens (fresh + cache-read + output) come from
// authoritative records only; Records counts every record of the bucket.
type PostTransitionRow struct {
	Role         string               `json:"role"`
	Category     FailureCategory      `json:"category"`
	Tokens       int                  `json:"tokens"`
	Records      int                  `json:"records"`
	Tasks        []PostTransitionTask `json:"tasks"`
	TasksOmitted int                  `json:"tasks_omitted,omitempty"`
}

// PostTransitionTask is one task's share of a post-transition row. LastUseful
// is nil when the task has no useful transition at or before as_of.
type PostTransitionTask struct {
	TaskID     string      `json:"task_id"`
	Tokens     int         `json:"tokens"`
	Records    int         `json:"records"`
	LastUseful *Transition `json:"last_useful,omitempty"`
}

// ProvenanceCounts counts the window's records by provenance, plus the store
// anomalies Load observed over the whole loaded range.
type ProvenanceCounts struct {
	TerminalAuthoritative int `json:"terminal_authoritative"`
	Partial               int `json:"partial"`
	Unknown               int `json:"unknown"`
	Conflicting           int `json:"conflicting"`
	DuplicatesCollapsed   int `json:"duplicates_collapsed"`
	MalformedLines        int `json:"malformed_lines"`
	ConflictingRecords    int `json:"conflicting_records"`
}

// SuppressedCalls projects the lifecycle counter rows that show suppressed
// duplicates and preflight rejections. It carries the counter file's own
// availability; an unavailable file has no rows rather than zero rows.
type SuppressedCalls struct {
	Available     bool                `json:"available"`
	ObservedSince *time.Time          `json:"observed_since,omitempty"`
	Warning       string              `json:"warning,omitempty"`
	Rows          []SuppressedCallRow `json:"rows,omitempty"`
}

// SuppressedCallRow is one operation x outcome counter.
type SuppressedCallRow struct {
	Operation string `json:"operation"`
	Outcome   string `json:"outcome"`
	Count     uint64 `json:"count"`
}

// Delta kinds and metrics.
const (
	DeltaKindOutcome        = "outcome"
	DeltaKindPostTransition = "post_transition"

	MetricFreshTokens     = "fresh_tokens"
	MetricCacheReadTokens = "cache_read_tokens"
	MetricOutputTokens    = "output_tokens"
	MetricTokens          = "tokens"
)

// Delta compares one metric between the baseline and the primary window:
// token totals per outcome x role, and post-transition tokens per role x
// failure category. Change is Primary - Baseline.
type Delta struct {
	Kind     string          `json:"kind"`
	Outcome  OutcomeClass    `json:"outcome,omitempty"`
	Role     string          `json:"role"`
	Category FailureCategory `json:"category,omitempty"`
	Metric   string          `json:"metric"`
	Baseline int             `json:"baseline"`
	Primary  int             `json:"primary"`
	Change   int             `json:"change"`
}

// Report is the usage report of one window. Figure blocks are absent when the
// store is unavailable. Baseline, Deltas and SuppressedCalls appear only on the
// primary report.
type Report struct {
	Window           Window              `json:"window"`
	Outcomes         []OutcomeRow        `json:"outcomes,omitempty"`
	OutcomeTotals    []SummaryOutcome    `json:"outcome_totals,omitempty"`
	PerCompletedTask *PerCompletedTask   `json:"per_completed_task,omitempty"`
	PostTransition   []PostTransitionRow `json:"post_transition,omitempty"`
	Provenance       *ProvenanceCounts   `json:"provenance,omitempty"`
	SuppressedCalls  *SuppressedCalls    `json:"suppressed_calls,omitempty"`
	Baseline         *Report             `json:"baseline,omitempty"`
	Deltas           []Delta             `json:"deltas,omitempty"`
	Warnings         []string            `json:"warnings,omitempty"`
}

// SummaryOutcome is the authoritative usage of one outcome class across roles.
// Tasks counts distinct tasks, so a task worked by two roles counts once.
type SummaryOutcome struct {
	Outcome         OutcomeClass `json:"outcome"`
	Tasks           int          `json:"tasks"`
	FreshTokens     int          `json:"fresh_tokens"`
	CacheReadTokens int          `json:"cache_read_tokens"`
	OutputTokens    int          `json:"output_tokens"`
}

// Summary is the compact projection of a Report embedded in metrics output:
// no distributions, post-transition rows or deltas.
type Summary struct {
	Window                       Window            `json:"window"`
	Outcomes                     []SummaryOutcome  `json:"outcomes,omitempty"`
	CacheReadTokensPerMergedTask *float64          `json:"cache_read_tokens_per_merged_task,omitempty"`
	CacheHitPercent              *float64          `json:"cache_hit_percent,omitempty"`
	Provenance                   *ProvenanceCounts `json:"provenance,omitempty"`
	Warnings                     []string          `json:"warnings,omitempty"`
}

// Summarize projects a report onto its summary.
func Summarize(r Report) Summary {
	s := Summary{Window: r.Window, Outcomes: r.OutcomeTotals, Provenance: r.Provenance, Warnings: r.Warnings}
	if r.PerCompletedTask != nil {
		s.CacheReadTokensPerMergedTask = r.PerCompletedTask.CacheReadTokensPerMergedTask
		s.CacheHitPercent = r.PerCompletedTask.CacheHitPercent
	}
	return s
}

// Build computes the report for in.Window and, when in.Baseline is set, the
// baseline report over the same record set and the deltas between them.
func Build(in Input) (Report, error) {
	if in.State == nil {
		return Report{}, errors.New("usage report: state is required")
	}
	if err := in.Window.validate(); err != nil {
		return Report{}, err
	}
	if in.Baseline != nil {
		if err := in.Baseline.validate(); err != nil {
			return Report{}, fmt.Errorf("baseline: %w", err)
		}
	}

	report, err := buildWindow(in, in.Window)
	if err != nil {
		return Report{}, err
	}
	report.SuppressedCalls = projectSuppressedCalls(in.Counters)
	report.Warnings = append(sourceWarnings(in.LoadStats, report.SuppressedCalls), report.Warnings...)
	if in.Baseline != nil {
		baseline, err := buildWindow(in, *in.Baseline)
		if err != nil {
			return Report{}, fmt.Errorf("baseline: %w", err)
		}
		report.Baseline = &baseline
		report.Deltas = compare(baseline, report)
	}
	return report, nil
}

func (w Window) validate() error {
	if !w.Since.IsZero() && !w.Until.IsZero() && w.Since.After(w.Until) {
		return fmt.Errorf("usage report: window since %s is after until %s",
			w.Since.Format(time.RFC3339), w.Until.Format(time.RFC3339))
	}
	return nil
}

func sourceWarnings(stats LoadStats, suppressed *SuppressedCalls) []string {
	var warnings []string
	if !stats.Available {
		warning := stats.Warning
		if warning == "" {
			warning = "usage store unavailable"
		}
		warnings = append(warnings, warning)
	}
	if stats.MalformedLines > 0 {
		warnings = append(warnings, fmt.Sprintf("usage store: %d malformed lines skipped", stats.MalformedLines))
	}
	if !suppressed.Available {
		warning := "lifecycle counters unavailable"
		if suppressed.Warning != "" {
			warning += ": " + suppressed.Warning
		}
		warnings = append(warnings, warning)
	}
	return warnings
}

// suppressedOutcomes selects the counter rows that show suppressed duplicates
// and preflight rejections; an empty outcome list selects every non-COMPLETED outcome.
var suppressedOutcomes = []struct {
	operation string
	outcomes  []string
}{
	{"assess-blocked", []string{models.LifecycleNoChange}},
	{"validate-payload", []string{models.LifecycleInvalidInput}},
	{"submit-verdict", nil},
}

func projectSuppressedCalls(counters *models.LifecycleOutcomeMetrics) *SuppressedCalls {
	if counters == nil {
		return &SuppressedCalls{}
	}
	projection := &SuppressedCalls{Available: counters.Available, Warning: counters.Warning}
	if !counters.Available {
		return projection
	}
	projection.ObservedSince = counters.ObservedSince
	for _, selection := range suppressedOutcomes {
		row := counters.Counts[selection.operation]
		outcomes := selection.outcomes
		if outcomes == nil {
			for outcome := range row {
				if outcome != models.LifecycleCompleted {
					outcomes = append(outcomes, outcome)
				}
			}
			sort.Strings(outcomes)
		}
		for _, outcome := range outcomes {
			if count, ok := row[outcome]; ok {
				projection.Rows = append(projection.Rows,
					SuppressedCallRow{Operation: selection.operation, Outcome: outcome, Count: count})
			}
		}
	}
	return projection
}

type tokenCounts struct {
	fresh, cacheRead, output int
}

func (c *tokenCounts) add(r Record) {
	c.fresh += r.FreshInputTokens
	c.cacheRead += r.CacheReadTokens
	c.output += r.OutputTokens
}

func (c tokenCounts) total() int { return c.fresh + c.cacheRead + c.output }

type outcomeKey struct {
	outcome OutcomeClass
	role    string
}

type outcomeAcc struct {
	units   map[string]*tokenCounts
	tasks   map[string]struct{}
	records int
}

type tailKey struct {
	role     string
	category FailureCategory
}

type tailAcc struct {
	tokens, records int
	tasks           map[string]*PostTransitionTask
}

// windowReader retains source tasks for dependency evidence, caching task
// readings with the graph and history at as_of. Anomalies are also truncated.
type windowReader struct {
	asOf      time.Time
	tasks     map[string]*models.Task
	anomalies []models.Anomaly
	cache     map[string]taskReading
}

type taskReading struct {
	task       *models.Task
	outcome    OutcomeClass
	lastUseful Transition
	hasUseful  bool
}

func newWindowReader(state *models.State, asOf time.Time) *windowReader {
	reader := &windowReader{asOf: asOf, tasks: map[string]*models.Task{}, cache: map[string]taskReading{}}
	for i := range state.Tasks {
		task := state.Tasks[i]
		reader.tasks[task.ID] = &task
	}
	for _, a := range state.Anomalies {
		if within(a.Timestamp, asOf) {
			reader.anomalies = append(reader.anomalies, a)
		}
	}
	return reader
}

// read classifies a task once per window. A missing task reads as unattributed.
func (w *windowReader) read(taskID string) (taskReading, error) {
	if reading, ok := w.cache[taskID]; ok {
		return reading, nil
	}
	task := w.tasks[taskID]
	reading := taskReading{task: task}
	reading.outcome, _ = ClassifyOutcome(task, w.asOf)
	if task != nil {
		dependencies, err := dependenciesAt(task, w.asOf)
		if err != nil {
			return taskReading{}, err
		}
		view := *task
		view.DependsOn = dependencies
		view.History = slices.DeleteFunc(slices.Clone(task.History), func(h models.TaskHistoryEntry) bool {
			return !within(h.Time, w.asOf)
		})
		reading.task = &view
		var deps []*models.Task
		for _, id := range dependencies {
			if dep := w.tasks[id]; dep != nil {
				deps = append(deps, dep)
			}
		}
		reading.lastUseful, reading.hasUseful = LastUsefulTransition(task, deps, w.asOf)
	}
	w.cache[taskID] = reading
	return reading, nil
}

// dependenciesAt uses the pre-change snapshot of the first later rewrite.
// History is append-only; subsequent rewrites cannot change that snapshot.
// apply-dependency-repair checks expected_dependencies against DependsOn before
// committing, so this is evidence of the actual graph, not just a requested one.
func dependenciesAt(task *models.Task, asOf time.Time) ([]string, error) {
	for _, h := range task.History {
		if h.Event != models.TaskEventDependenciesRewritten || within(h.Time, asOf) {
			continue
		}
		// These audit shapes explicitly describe output-only changes.
		if h.Extra["rewrote_depends_on"] == false || h.Extra["rewrote_inherit_inputs"] == true {
			continue
		}
		switch prior := h.Extra["expected_dependencies"].(type) {
		case []string:
			return slices.Clone(prior), nil
		case []any: // JSON/YAML-decoded history extras.
			dependencies := make([]string, 0, len(prior))
			for _, item := range prior {
				id, ok := item.(string)
				if !ok {
					break
				}
				dependencies = append(dependencies, id)
			}
			if len(dependencies) == len(prior) {
				return dependencies, nil
			}
		}
		return nil, fmt.Errorf("usage report: historical dependencies unavailable for task %s at %s: dependencies_rewritten at %s lacks valid expected_dependencies",
			task.ID, asOf.Format(time.RFC3339), h.Time.Format(time.RFC3339))
	}
	return task.DependsOn, nil
}

func buildWindow(in Input, w Window) (Report, error) {
	if w.AsOf.IsZero() {
		w.AsOf = w.Until
	}
	report := Report{Window: w}
	if !in.LoadStats.Available {
		return report, nil
	}

	reader := newWindowReader(in.State, w.AsOf)
	provenance := ProvenanceCounts{
		DuplicatesCollapsed: in.LoadStats.DuplicatesCollapsed,
		MalformedLines:      in.LoadStats.MalformedLines,
		ConflictingRecords:  in.LoadStats.ConflictingRecords,
	}
	outcomes := map[outcomeKey]*outcomeAcc{}
	tails := map[tailKey]*tailAcc{}
	missingTasks := 0
	referenced := map[string]struct{}{}
	var overall tokenCounts

	for _, r := range in.Records {
		if !inWindow(r.StartedAt, w.Since, w.Until) {
			continue
		}
		authoritative := countProvenance(&provenance, r.Provenance)
		reading, err := reader.read(r.TaskID)
		if err != nil {
			return Report{}, err
		}
		if r.TaskID != "" && reading.task == nil {
			missingTasks++
		}
		if reading.task != nil {
			referenced[r.TaskID] = struct{}{}
			accumulateTail(tails, reader, reading, r, authoritative)
		}
		if !authoritative {
			continue
		}
		overall.add(r)
		key := outcomeKey{reading.outcome, r.Role}
		acc := outcomes[key]
		if acc == nil {
			acc = &outcomeAcc{units: map[string]*tokenCounts{}, tasks: map[string]struct{}{}}
			outcomes[key] = acc
		}
		unit := r.RecordID
		if reading.task != nil {
			unit = r.TaskID
			acc.tasks[r.TaskID] = struct{}{}
		}
		if acc.units[unit] == nil {
			acc.units[unit] = &tokenCounts{}
		}
		acc.units[unit].add(r)
		acc.records++
	}

	report.Provenance = &provenance
	report.Outcomes = outcomeRows(outcomes)
	report.OutcomeTotals = outcomeTotals(outcomes)
	report.PerCompletedTask = perCompletedTask(outcomes, overall)
	report.PostTransition = tailRows(tails)
	if missingTasks > 0 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("%d usage records name tasks absent from state; reported as unattributed", missingTasks))
	}
	var referencedTasks []models.Task
	for id := range referenced {
		referencedTasks = append(referencedTasks, *reader.cache[id].task)
	}
	if names := UnclassifiedEvents(referencedTasks); len(names) > 0 {
		report.Warnings = append(report.Warnings, "unclassified task events: "+strings.Join(names, ", "))
	}
	return report, nil
}

// countProvenance counts one record and reports whether it is authoritative.
// An unrecognized provenance value counts as unknown.
func countProvenance(counts *ProvenanceCounts, provenance Provenance) bool {
	switch provenance {
	case ProvenanceTerminalAuthoritative:
		counts.TerminalAuthoritative++
		return true
	case ProvenancePartial:
		counts.Partial++
	case ProvenanceConflicting:
		counts.Conflicting++
	default:
		counts.Unknown++
	}
	return false
}

// accumulateTail adds a record started after its task's last useful
// transition (or any record of a task without one) to its failure bucket.
func accumulateTail(tails map[tailKey]*tailAcc, reader *windowReader, reading taskReading, r Record, authoritative bool) {
	var since time.Time
	if reading.hasUseful {
		if !r.StartedAt.After(reading.lastUseful.Time) {
			return
		}
		since = reading.lastUseful.Time
	}
	key := tailKey{r.Role, ClassifyFailure(reading.task, reader.anomalies, since, authoritative)}
	acc := tails[key]
	if acc == nil {
		acc = &tailAcc{tasks: map[string]*PostTransitionTask{}}
		tails[key] = acc
	}
	detail := acc.tasks[r.TaskID]
	if detail == nil {
		detail = &PostTransitionTask{TaskID: r.TaskID}
		if reading.hasUseful {
			lastUseful := reading.lastUseful
			detail.LastUseful = &lastUseful
		}
		acc.tasks[r.TaskID] = detail
	}
	acc.records++
	detail.Records++
	if authoritative {
		var counts tokenCounts
		counts.add(r)
		acc.tokens += counts.total()
		detail.Tokens += counts.total()
	}
}

func outcomeRows(outcomes map[outcomeKey]*outcomeAcc) []OutcomeRow {
	var rows []OutcomeRow
	for key, acc := range outcomes {
		var fresh, cacheRead, output []int
		for _, unit := range acc.units {
			fresh = append(fresh, unit.fresh)
			cacheRead = append(cacheRead, unit.cacheRead)
			output = append(output, unit.output)
		}
		rows = append(rows, OutcomeRow{
			Outcome: key.outcome, Role: key.role, Tasks: len(acc.tasks), Records: acc.records,
			FreshTokens: distribution(fresh), CacheReadTokens: distribution(cacheRead), OutputTokens: distribution(output),
		})
	}
	slices.SortFunc(rows, func(a, b OutcomeRow) int {
		if c := outcomeRank(a.Outcome) - outcomeRank(b.Outcome); c != 0 {
			return c
		}
		return strings.Compare(a.Role, b.Role)
	})
	return rows
}

func outcomeTotals(outcomes map[outcomeKey]*outcomeAcc) []SummaryOutcome {
	var totals []SummaryOutcome
	for _, class := range OutcomeClasses {
		total := SummaryOutcome{Outcome: class}
		tasks := map[string]struct{}{}
		found := false
		for key, acc := range outcomes {
			if key.outcome != class {
				continue
			}
			found = true
			for id := range acc.tasks {
				tasks[id] = struct{}{}
			}
			for _, unit := range acc.units {
				total.FreshTokens += unit.fresh
				total.CacheReadTokens += unit.cacheRead
				total.OutputTokens += unit.output
			}
		}
		if found {
			total.Tasks = len(tasks)
			totals = append(totals, total)
		}
	}
	return totals
}

func perCompletedTask(outcomes map[outcomeKey]*outcomeAcc, overall tokenCounts) *PerCompletedTask {
	merged := map[string]struct{}{}
	mergedCacheRead := 0
	for key, acc := range outcomes {
		if key.outcome != OutcomeMerged {
			continue
		}
		for id := range acc.tasks {
			merged[id] = struct{}{}
		}
		for _, unit := range acc.units {
			mergedCacheRead += unit.cacheRead
		}
	}
	per := &PerCompletedTask{MergedTasks: len(merged)}
	if len(merged) > 0 {
		ratio := float64(mergedCacheRead) / float64(len(merged))
		per.CacheReadTokensPerMergedTask = &ratio
	}
	if input := overall.fresh + overall.cacheRead; input > 0 {
		percent := float64(overall.cacheRead) * 100 / float64(input)
		per.CacheHitPercent = &percent
	}
	return per
}

func tailRows(tails map[tailKey]*tailAcc) []PostTransitionRow {
	var rows []PostTransitionRow
	for key, acc := range tails {
		row := PostTransitionRow{Role: key.role, Category: key.category, Tokens: acc.tokens, Records: acc.records}
		for _, detail := range acc.tasks {
			row.Tasks = append(row.Tasks, *detail)
		}
		slices.SortFunc(row.Tasks, func(a, b PostTransitionTask) int {
			if a.Tokens != b.Tokens {
				return b.Tokens - a.Tokens
			}
			return strings.Compare(a.TaskID, b.TaskID)
		})
		if len(row.Tasks) > postTransitionDetailLimit {
			row.TasksOmitted = len(row.Tasks) - postTransitionDetailLimit
			row.Tasks = row.Tasks[:postTransitionDetailLimit]
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b PostTransitionRow) int {
		if c := strings.Compare(a.Role, b.Role); c != 0 {
			return c
		}
		return categoryRank(a.Category) - categoryRank(b.Category)
	})
	return rows
}

// distribution uses nearest-rank percentiles: the value at rank ceil(p*n).
func distribution(values []int) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	d := Distribution{Median: nearestRank(sorted, 0.5), P95: nearestRank(sorted, 0.95)}
	for _, v := range sorted {
		d.Total += v
	}
	return d
}

func nearestRank(sorted []int, p float64) int {
	rank := int(math.Ceil(p * float64(len(sorted))))
	return sorted[max(rank, 1)-1]
}

func outcomeRank(class OutcomeClass) int { return slices.Index(OutcomeClasses, class) }

func categoryRank(category FailureCategory) int { return slices.Index(FailureCategories, category) }

// compare emits one delta per metric for every outcome x role and role x
// failure category present in either report, in report order.
func compare(baseline, primary Report) []Delta {
	type outcomeMetrics struct{ baseline, primary tokenCounts }
	byOutcome := map[outcomeKey]*outcomeMetrics{}
	var outcomeKeys []outcomeKey
	collectOutcomes := func(rows []OutcomeRow, isPrimary bool) {
		for _, row := range rows {
			key := outcomeKey{row.Outcome, row.Role}
			m := byOutcome[key]
			if m == nil {
				m = &outcomeMetrics{}
				byOutcome[key] = m
				outcomeKeys = append(outcomeKeys, key)
			}
			counts := tokenCounts{row.FreshTokens.Total, row.CacheReadTokens.Total, row.OutputTokens.Total}
			if isPrimary {
				m.primary = counts
			} else {
				m.baseline = counts
			}
		}
	}
	collectOutcomes(baseline.Outcomes, false)
	collectOutcomes(primary.Outcomes, true)
	slices.SortFunc(outcomeKeys, func(a, b outcomeKey) int {
		if c := outcomeRank(a.outcome) - outcomeRank(b.outcome); c != 0 {
			return c
		}
		return strings.Compare(a.role, b.role)
	})

	var deltas []Delta
	for _, key := range outcomeKeys {
		m := byOutcome[key]
		for _, metric := range []struct {
			name              string
			baseline, primary int
		}{
			{MetricFreshTokens, m.baseline.fresh, m.primary.fresh},
			{MetricCacheReadTokens, m.baseline.cacheRead, m.primary.cacheRead},
			{MetricOutputTokens, m.baseline.output, m.primary.output},
		} {
			deltas = append(deltas, Delta{Kind: DeltaKindOutcome, Outcome: key.outcome, Role: key.role,
				Metric: metric.name, Baseline: metric.baseline, Primary: metric.primary, Change: metric.primary - metric.baseline})
		}
	}

	tailTokens := map[tailKey]*[2]int{}
	var tailKeys []tailKey
	for i, rows := range [][]PostTransitionRow{baseline.PostTransition, primary.PostTransition} {
		for _, row := range rows {
			key := tailKey{row.Role, row.Category}
			if tailTokens[key] == nil {
				tailTokens[key] = &[2]int{}
				tailKeys = append(tailKeys, key)
			}
			tailTokens[key][i] = row.Tokens
		}
	}
	slices.SortFunc(tailKeys, func(a, b tailKey) int {
		if c := strings.Compare(a.role, b.role); c != 0 {
			return c
		}
		return categoryRank(a.category) - categoryRank(b.category)
	})
	for _, key := range tailKeys {
		tokens := tailTokens[key]
		deltas = append(deltas, Delta{Kind: DeltaKindPostTransition, Role: key.role, Category: key.category,
			Metric: MetricTokens, Baseline: tokens[0], Primary: tokens[1], Change: tokens[1] - tokens[0]})
	}
	return deltas
}
