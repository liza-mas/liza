package historyfixture

import "time"

// Repository names. Each is the directory Build creates under its target.
const (
	Upstream   = "upstream"
	Downstream = "downstream"
)

// Population classes of a fix.
const (
	PopulationProduct = "product"
	PopulationProof   = "proof"
	PopulationInRun   = "in-run"
)

// Kind is how a case enters its repository.
type Kind int

// How a case enters its repository.
const (
	// KindCommit commits Files on Branch (main when empty).
	KindCommit Kind = iota
	// KindMerge merges Branch into main with --no-ff.
	KindMerge
	// KindRevert reverts the case named by Of.
	KindRevert
	// KindCherryPick cherry-picks the case named by Of after a fetch, without -x.
	KindCherryPick
)

const (
	sideBranch = "topic"
	// cloneAfter names the upstream case after which downstream is cloned.
	cloneAfter = "u02"
)

// File is a path and its full content after a case.
type File struct {
	Path    string
	Content string
}

// Case is one commit of the history and what it must exhibit.
type Case struct {
	ID   string
	Repo string
	Kind Kind
	// Type is the Conventional Commit type of the subject, or "merge" or
	// "revert" for git's own subjects.
	Type string
	// Date is the committer date; the author date equals it unless Kind is
	// KindCherryPick, which keeps the picked commit's author date.
	Date    time.Time
	Subject string
	Body    string
	Files   []File
	// Branch is the side branch a KindCommit case is committed on and the
	// branch a KindMerge case merges.
	Branch string
	// Of is the case a KindRevert reverts or a KindCherryPick picks.
	Of string
	// Population classifies a fix; empty for other commits.
	Population string
	// Expected is the finding the case must let an analysis reach.
	Expected string
	// SHA is filled by Build.
	SHA string
}

func at(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 12, 0, 0, 0, time.UTC)
}

const windowCheckV1 = `# Decide whether a moment falls inside a half-open window.


def in_window(start, end, moment):
    if moment < start:
        return False
    if moment > end:
        return False
    return True


def window_length(start, end):
    # Length is end minus start, in seconds.
    return end - start


def overlaps(a_start, a_end, b_start, b_end):
    return a_start < b_end and b_start < a_end
`

// windowCheckV2 changes line 7 of windowCheckV1.
const windowCheckV2 = `# Decide whether a moment falls inside a half-open window.


def in_window(start, end, moment):
    if moment < start:
        return False
    if moment >= end:
        return False
    return True


def window_length(start, end):
    # Length is end minus start, in seconds.
    return end - start


def overlaps(a_start, a_end, b_start, b_end):
    return a_start < b_end and b_start < a_end
`

// windowCheckV3 rewords line 13 of windowCheckV2 and also changes the
// comparison on line 18.
const windowCheckV3 = `# Decide whether a moment falls inside a half-open window.


def in_window(start, end, moment):
    if moment < start:
        return False
    if moment >= end:
        return False
    return True


def window_length(start, end):
    # Length is the end minus the start, counted in seconds.
    return end - start


def overlaps(a_start, a_end, b_start, b_end):
    return a_start <= b_end and b_start <= a_end
`

const durationV1 = `# Parse a duration such as "90s", "15m" or "2h" into seconds.

UNITS = {"s": 1, "m": 60, "h": 3600}


def parse_duration(text):
    value = text[:-1]
    unit = text[-1]
    return int(value) * UNITS[unit]


def format_seconds(total):
    return str(total) + "s"
`

// durationV2 replaces lines 7 and 8 of durationV1.
const durationV2 = `# Parse a duration such as "90s", "15m" or "2h" into seconds.

UNITS = {"s": 1, "m": 60, "h": 3600}


def parse_duration(text):
    value = text.strip()[:-1]
    unit = text.strip()[-1].lower()
    return int(value) * UNITS[unit]


def format_seconds(total):
    return str(total) + "s"
`

// durationV3 replaces lines 7 and 8 of durationV2.
const durationV3 = `# Parse a duration such as "90s", "15m" or "2h" into seconds.

UNITS = {"s": 1, "m": 60, "h": 3600}


def parse_duration(text):
    cleaned = text.strip().lower()
    value, unit = cleaned[:-1], cleaned[-1:]
    return int(value) * UNITS[unit]


def format_seconds(total):
    return str(total) + "s"
`

const durationTestV1 = `from duration import parse_duration

CASES = [
    ("90s", 90),
    ("15m", 900),
    ("2h", 7200),
]


def test_parse_duration():
    for text, expected in CASES:
        assert parse_duration("90s") == 90
`

// durationTestV2 replaces line 12 of durationTestV1.
const durationTestV2 = `from duration import parse_duration

CASES = [
    ("90s", 90),
    ("15m", 900),
    ("2h", 7200),
]


def test_parse_duration():
    for text, expected in CASES:
        assert parse_duration(text) == expected
`

const overviewV1 = `# Overview

These helpers answer small questions about time: whether a moment is inside
a window, how long a window is and how to read a duration written as text.
`

const retryV1 = `# Delay before the next attempt, doubling each time up to a cap.


def next_delay(attempt, base, cap):
    delay = base * 2 ** attempt
    return delay
`

// retryV2 replaces line 6 of retryV1.
const retryV2 = `# Delay before the next attempt, doubling each time up to a cap.


def next_delay(attempt, base, cap):
    delay = base * 2 ** attempt
    return min(delay, cap)
`

const cacheKeyV1 = `# Build a cache key from a name and a version.


def cache_key(name, version):
    return name + "@" + version
`

const reportV1 = `# Render a count as a short report line.


def report_line(label, count):
    return label + ": " + count
`

// reportV2 replaces line 5 of reportV1.
const reportV2 = `# Render a count as a short report line.


def report_line(label, count):
    return label + ": " + str(count)
`

const (
	windowPath   = "src/window_check.py"
	durationPath = "src/duration.py"
	testPath     = "tests/test_duration.py"
	retryPath    = "src/retry.py"
	reportPath   = "src/report.py"
)

// Cases returns the case table in build order: upstream cases first, then
// downstream cases. It is the reference for every property the layout in the
// Scope 5 architecture requires; SHA is empty until Build fills it.
func Cases() []Case {
	cases := upstreamCases()
	cases = append(cases, upstreamTailCases()...)
	return append(cases, downstreamCases()...)
}

// upstreamCases are the upstream cases up to the docs commit, on main.
func upstreamCases() []Case {
	return []Case{
		{
			ID: "u01", Repo: Upstream, Type: "feat", Date: at(time.January, 5),
			Subject:  "feat: add a window membership check",
			Files:    []File{{windowPath, windowCheckV1}},
			Expected: "old code for the u06 and u07 fixes",
		},
		{
			ID: "u02", Repo: Upstream, Type: "feat", Date: at(time.February, 10),
			Subject:  "feat: add a duration parser with a table test",
			Files:    []File{{durationPath, durationV1}, {testPath, durationTestV1}},
			Expected: "introducing change",
		},
		{
			ID: "u03", Repo: Upstream, Type: "fix", Date: at(time.February, 12),
			Subject: "fix: accept padded and upper-case durations",
			Body:    "Text with surrounding spaces or an upper-case unit was rejected.",
			Files:   []File{{durationPath, durationV2}}, Population: PopulationProduct,
			Expected: "repair of the u02 change; young",
		},
		{
			ID: "u04", Repo: Upstream, Type: "fix", Date: at(time.February, 14),
			Subject: "fix: clean the text once before splitting it",
			Body:    "The previous repair stripped the text twice and indexed an empty remainder.",
			Files:   []File{{durationPath, durationV3}}, Population: PopulationProduct,
			Expected: "repair of a repair (chain u02, u03, u04); young",
		},
		{
			ID: "u05", Repo: Upstream, Type: "fix", Date: at(time.February, 16),
			Subject: "fix: make the table test use its table",
			Body:    "The loop asserted one fixed input and never read its cases.",
			Files:   []File{{testPath, durationTestV2}}, Population: PopulationProof,
			Expected: "proof population; young",
		},
		{
			ID: "u06", Repo: Upstream, Type: "fix", Date: at(time.March, 1),
			Subject: "fix: treat the window end as exclusive",
			Body: "The check accepted a moment equal to the end, so adjacent windows both\n" +
				"claimed the boundary instant.\n" +
				"\n" +
				"Prevention-Cause: window bounds had no stated inclusive or exclusive rule\n" +
				"Prevention-Concept: src/window_check.py#in_window\n" +
				"Prevention-Disposition: scheduled\n" +
				"Prevention-Owner: human\n" +
				"Prevention-Class: gap",
			Files: []File{{windowPath, windowCheckV2}}, Population: PopulationProduct,
			Expected: "product; old; record read as starting point",
		},
		{
			ID: "u07", Repo: Upstream, Type: "fix", Date: at(time.March, 3),
			Subject: "fix: reword the length comment",
			Body:    "Comment-only change: no behaviour differs.",
			Files:   []File{{windowPath, windowCheckV3}}, Population: PopulationProduct,
			Expected: "misleading message caught by its diff; old",
		},
		{
			ID: "u08", Repo: Upstream, Type: "docs", Date: at(time.March, 5),
			Subject:  "docs: describe the helpers",
			Files:    []File{{"docs/overview.md", overviewV1}},
			Expected: "not gathered",
		},
	}
}

// upstreamTailCases are the upstream side branch, its merge, and the revert.
func upstreamTailCases() []Case {
	return []Case{
		{
			ID: "b01", Repo: Upstream, Type: "feat", Date: at(time.March, 6), Branch: sideBranch,
			Subject: "feat: add a retry delay helper",
			Files:   []File{{retryPath, retryV1}},
		},
		{
			ID: "b02", Repo: Upstream, Type: "fix", Date: at(time.March, 7), Branch: sideBranch,
			Subject: "fix: cap the retry delay",
			Body:    "The delay doubled without bound.",
			Files:   []File{{retryPath, retryV2}}, Population: PopulationInRun,
			Expected: "in-run correction; young",
		},
		{
			ID: "u09", Repo: Upstream, Type: "merge", Kind: KindMerge, Date: at(time.March, 8), Branch: sideBranch,
			Subject:  "Merge branch '" + sideBranch + "'",
			Expected: "not gathered",
		},
		{
			ID: "u10", Repo: Upstream, Type: "feat", Date: at(time.March, 9),
			Subject: "feat: add a cache key helper",
			Files:   []File{{"src/cache_key.py", cacheKeyV1}},
		},
		{
			ID: "u11", Repo: Upstream, Type: "revert", Kind: KindRevert, Of: "u10", Date: at(time.March, 10),
			Subject:  `Revert "feat: add a cache key helper"`,
			Expected: "revert gathered",
		},
	}
}

// downstreamCases are the downstream cases, built after the clone.
func downstreamCases() []Case {
	return []Case{
		{
			ID: "d01", Repo: Downstream, Type: "feat", Date: at(time.February, 13),
			Subject: "feat: add a report line renderer",
			Files:   []File{{reportPath, reportV1}},
		},
		{
			ID: "d02", Repo: Downstream, Type: "fix", Kind: KindCherryPick, Of: "u03", Date: at(time.February, 13),
			Subject:    "fix: accept padded and upper-case durations",
			Body:       "Text with surrounding spaces or an upper-case unit was rejected.",
			Population: PopulationProduct,
			Expected:   "duplicate of u03, counted once, attributed upstream",
		},
		{
			ID: "d03", Repo: Downstream, Type: "fix", Date: at(time.February, 20),
			Subject: "fix: report line",
			Files:   []File{{reportPath, reportV2}}, Population: PopulationProduct,
			Expected: "thin message, confidence lowered, diff read; young; downstream's own draft",
		},
	}
}
