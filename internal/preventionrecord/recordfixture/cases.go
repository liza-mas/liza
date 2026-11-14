package recordfixture

import (
	"strings"
	"time"
)

// Form classifies a case's prevention record.
type Form string

// The record forms of the case table.
const (
	// FormWellFormed is a record that satisfies the document's key rules.
	FormWellFormed Form = "well-formed"
	// FormMalformed is a record that breaks them; Case.Note says how.
	FormMalformed Form = "malformed"
	// FormOmitted is a fix commit without a record.
	FormOmitted Form = "omitted"
	// FormNotApplicable is a commit that needs no record: not a fix, or a revert.
	FormNotApplicable Form = "not-applicable"
)

// Case is one commit of the sample-record repository. The Concepts,
// Disposition, Owner and Class fields are the expected record fields of the
// document's query output; they are independent of the message text so a
// reader of a query result compares it with the table, not with itself.
type Case struct {
	ID      string
	Tag     string
	Date    time.Time
	Subject string
	// Paragraphs follow the subject, separated by blank lines; the last one
	// is the final trailer block when the commit has a record. The token
	// {sha:<id>} stands for the SHA of an earlier case.
	Paragraphs []string
	// Path is the file the commit changes; Delete removes it instead.
	Path   string
	Delete bool
	Form   Form
	Note   string
	// Listed reports whether the document's queries list the commit.
	Listed      bool
	Concepts    []string
	Disposition string
	Owner       string
	Class       string
	// SHA is filled by Build.
	SHA string
}

// Line returns the expected query output line for a listed case.
func (c Case) Line() string {
	return strings.Join([]string{
		c.SHA,
		c.Date.Format(queryDateLayout),
		strings.Join(c.Concepts, " "),
		c.Disposition,
		c.Owner,
		c.Class,
		c.Subject,
	}, "\t")
}

// queryDateLayout is the committer-date form of the query output.
const queryDateLayout = "2006-01-02T15:04:05Z"

func day(month time.Month, d int) time.Time {
	return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC)
}

// block renders a final trailer block; empty values omit their key.
func block(cause string, concepts []string, disposition, owner, class string) string {
	var lines []string
	if cause != "" {
		lines = append(lines, "Prevention-Cause: "+cause)
	}
	for _, concept := range concepts {
		lines = append(lines, "Prevention-Concept: "+concept)
	}
	if disposition != "" {
		lines = append(lines, "Prevention-Disposition: "+disposition)
	}
	if owner != "" {
		lines = append(lines, "Prevention-Owner: "+owner)
	}
	if class != "" {
		lines = append(lines, "Prevention-Class: "+class)
	}
	return strings.Join(lines, "\n")
}

// fixCase builds a listed case with a prose paragraph and a record block.
func fixCase(id string, date time.Time, subject string, form Form, concepts []string, disposition, owner, class, recordBlock string) Case {
	return Case{
		ID:          id,
		Date:        date,
		Subject:     subject,
		Paragraphs:  []string{"Repair " + id + ".", recordBlock},
		Form:        form,
		Listed:      true,
		Concepts:    concepts,
		Disposition: disposition,
		Owner:       owner,
		Class:       class,
	}
}

// Cases returns the I3 case table in commit order. Dates are the committer
// and author dates; SHA is empty until Build fills it in its own copy.
func Cases() []Case {
	const (
		storePut = "internal/cache/store.go#Store.Put"
		evict    = "internal/cache/evict.go#Evict"
		handle   = "internal/api/[id].go#Handle"
		qDrain   = "internal/queue/drain.go#Drain"
		wDrain   = "internal/work/drain.go#Drain"
	)
	cases := []Case{
		{ID: "c01", Date: day(time.March, 1), Subject: "chore: initialize the sample", Form: FormNotApplicable},
		fixCase("c02", day(time.March, 2), "fix(store): refuse writes after close", FormWellFormed,
			[]string{"T0.2"}, "removed", "", "gap",
			block("a closed store accepted writes", []string{"T0.2"}, "removed", "", "gap")),
		fixCase("c03", day(time.March, 3), "fix: reject an empty key", FormWellFormed,
			[]string{"T0.21"}, "scheduled", "human", "unenforced",
			block("keys were never checked at the boundary", []string{"T0.21"}, "scheduled", "human", "unenforced")),
		fixCase("c04", day(time.March, 4), "fix(cache): bound the entry size", FormWellFormed,
			[]string{storePut}, "scheduled", "TASK-42", "not-consulted",
			block("no owner limited what the cache stored", []string{storePut}, "scheduled", "TASK-42", "not-consulted")),
		fixCase("c05", day(time.March, 5), "fix(cache): evict stale entries", FormWellFormed,
			[]string{evict}, "removed", "", "not-consulted",
			block("eviction trusted the caller's clock", []string{evict}, "removed", "", "not-consulted")),
		fixCase("c06", day(time.March, 6), "fix: read the config from the working directory", FormWellFormed,
			[]string{"./config.yaml"}, "removed", "", "none-needed",
			block("the path was resolved twice", []string{"./config.yaml"}, "removed", "", "none-needed")),
		fixCase("c07", day(time.March, 7), "fix(api): validate the identifier", FormWellFormed,
			[]string{"T0.2", handle}, "removed", "", "gap",
			block("the handler trusted a decoded identifier", []string{"T0.2", handle}, "removed", "", "gap")),
		fixCase("c08", day(time.March, 8), "fix: bound the retry count", FormWellFormed,
			[]string{"S1.1"}, "accepted", "", "gap",
			block("retries were unbounded by default", []string{"S1.1"}, "accepted", "", "gap")),
		{
			ID: "c09", Date: day(time.March, 9), Subject: "fix: trim trailing whitespace",
			Paragraphs: []string{"The fix is small and carries no record."},
			Form:       FormOmitted, Listed: true,
		},
		fixCase("c10", day(time.March, 10), "fix: flush on shutdown", FormMalformed,
			[]string{"internal/cache/store.go"}, "deferred", "", "gap",
			block("shutdown dropped buffered writes", []string{"internal/cache/store.go"}, "deferred", "", "gap")),
		fixCase("c11", day(time.March, 11), "fix: stop double counting", FormMalformed,
			[]string{"T0.21"}, "removed", "", "",
			block("counters were incremented in two places", []string{"T0.21"}, "removed", "", "")),
		fixCase("c12", day(time.March, 12), "fix(api)!: change the error shape", FormMalformed,
			[]string{"internal/api/[id].go"}, "removed", "human", "gap",
			block("errors leaked internal detail", []string{"internal/api/[id].go"}, "removed", "human", "gap")),
		{
			ID: "c13", Date: day(time.March, 13), Subject: "fix: drain the queue on exit",
			Paragraphs: []string{
				"Squashed from two commits.",
				block("the first attempt closed the queue early", []string{"T0.21"}, "removed", "", ""),
				block("the drain loop never returned", []string{qDrain}, "removed", "", "gap"),
			},
			Form: FormMalformed, Note: "record line outside the final block",
			Listed: true, Concepts: []string{qDrain}, Disposition: "removed", Class: "gap",
		},
		fixCase("c14", day(time.March, 14), "fix: drain the queue on exit and check keys", FormWellFormed,
			[]string{qDrain, "T0.21"}, "scheduled", "TASK-43", "unenforced",
			block("the drain loop never returned and keys were unchecked", []string{qDrain, "T0.21"}, "scheduled", "TASK-43", "unenforced")),
		{
			ID: "c15", Date: day(time.March, 15), Subject: `Revert "fix(cache): evict stale entries"`,
			Paragraphs: []string{
				"This reverts commit {sha:c05}.",
				block("eviction trusted the caller's clock", []string{evict}, "removed", "", "not-consulted"),
			},
			Path: "cases/c05.txt", Delete: true, Form: FormNotApplicable, Note: "revert of c05",
		},
		{
			ID: "c16", Date: day(time.March, 16), Subject: "docs: describe the example",
			Paragraphs: []string{
				"fix(api): example",
				block("the example was missing", []string{"T0.2"}, "removed", "", "gap"),
			},
			Form: FormNotApplicable, Note: "non-fix commit whose body looks like a fix subject",
		},
		fixCase("c17", day(time.March, 28), "fix: cap the retry delay", FormWellFormed,
			[]string{"S1.1"}, "removed", "", "unenforced",
			block("the delay doubled without a ceiling", []string{"S1.1"}, "removed", "", "unenforced")),
		fixCase("c18", day(time.March, 29), "fix(work): follow the drain to its new home", FormWellFormed,
			[]string{wDrain, qDrain}, "removed", "", "gap",
			block("the drain moved and its callers kept the old path", []string{wDrain, qDrain}, "removed", "", "gap")),
	}
	for i := range cases {
		cases[i].Tag = "case/" + cases[i].ID
		if cases[i].Path == "" {
			cases[i].Path = "cases/" + cases[i].ID + ".txt"
		}
	}
	return cases
}
