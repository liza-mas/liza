// Package preventionrecord validates the prevention record a fix commit carries
// in its final trailer block, and reads the commits a submission would publish
// so the submit gate can refuse the ones whose record is missing or malformed.
//
// Git alone decides where the final trailer block is. The validator never
// re-implements that detection: it takes git's block verbatim and checks its
// raw lines, because git's parsed trailer form folds continuations and drops
// lines it does not parse, which would hide what the record format forbids. A
// record the validator accepts parses, under git's trailer parsing, to the same
// keys and values.
//
// The package reports data only: a closed vocabulary of problems, each naming
// the key it concerns. It never returns a record value.
package preventionrecord

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Problem names, from a closed vocabulary, one way a record departs from the
// record format.
type Problem string

// The problem vocabulary.
const (
	// RecordMissing: the commit carries no prevention line at all.
	RecordMissing Problem = "record_missing"
	// OutsideFinalBlock: a prevention line sits outside the final trailer block.
	OutsideFinalBlock Problem = "outside_final_block"
	// KeyUnknown: an entry key is not one of the record's five keys.
	KeyUnknown Problem = "key_unknown"
	// KeyMissing: a required key is absent.
	KeyMissing Problem = "key_missing"
	// KeyRepeated: a single-valued key appears more than once.
	KeyRepeated Problem = "key_repeated"
	// ValueInvalid: a value is empty, spans several lines or is not allowed.
	ValueInvalid Problem = "value_invalid"
	// OwnerUnexpected: an owner accompanies a disposition other than scheduled.
	OwnerUnexpected Problem = "owner_unexpected"
	// DispositionAccepted: an agent recorded accepted, which only a human may.
	DispositionAccepted Problem = "disposition_accepted"
)

// The record's keys, written exactly so.
const (
	KeyCause       = "Prevention-Cause"
	KeyConcept     = "Prevention-Concept"
	KeyDisposition = "Prevention-Disposition"
	KeyOwner       = "Prevention-Owner"
	KeyClass       = "Prevention-Class"
)

// Finding is one problem and the key it concerns; Key is empty for
// RecordMissing and OutsideFinalBlock.
type Finding struct {
	Problem Problem
	Key     string
}

var (
	fixSubject = regexp.MustCompile(`^fix(\([^)]*\))?!?: `)
	recordLine = regexp.MustCompile(`(?i)^prevention-[^\s:]*[ \t]*:`)
	invariant  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)
	symbol     = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)
)

// IsFixSubject reports whether a commit subject has Conventional Commit type
// fix: the population that must carry a record. Reverts and every other type
// are not.
func IsFixSubject(subject string) bool {
	return fixSubject.MatchString(subject)
}

type entry struct {
	key, value string
	multiline  bool
}

// Validate returns every problem of the record in a fix commit, in the order of
// the record format's rules; none means the commit passes. message is the raw
// commit message and block git's final trailer block, verbatim.
func Validate(message, block string) []Finding {
	entries := readBlock(block)
	messageLines := 0
	for line := range strings.SplitSeq(message, "\n") {
		if recordLine.MatchString(line) {
			messageLines++
		}
	}

	if len(entries) == 0 {
		if messageLines == 0 {
			return []Finding{{Problem: RecordMissing}}
		}
		return []Finding{{Problem: OutsideFinalBlock}}
	}

	var findings []Finding
	add := func(p Problem, key string) {
		if f := (Finding{Problem: p, Key: key}); !slices.Contains(findings, f) {
			findings = append(findings, f)
		}
	}

	if messageLines > len(entries) {
		add(OutsideFinalBlock, "")
	}

	counts := map[string]int{}
	known := map[string]bool{KeyCause: true, KeyConcept: true, KeyDisposition: true, KeyOwner: true, KeyClass: true}
	for _, e := range entries {
		if !known[e.key] {
			add(KeyUnknown, e.key)
			continue
		}
		counts[e.key]++
	}
	for _, key := range []string{KeyCause, KeyConcept, KeyDisposition, KeyClass} {
		if counts[key] == 0 {
			add(KeyMissing, key)
		}
	}
	for _, key := range []string{KeyCause, KeyDisposition, KeyOwner, KeyClass} {
		if counts[key] > 1 {
			add(KeyRepeated, key)
		}
	}

	var dispositions []string
	for _, e := range entries {
		if !known[e.key] {
			continue
		}
		if e.multiline || !validValue(e.key, e.value) {
			add(ValueInvalid, e.key)
			continue
		}
		if e.key == KeyDisposition {
			dispositions = append(dispositions, e.value)
		}
	}

	if len(dispositions) == 1 {
		switch disposition := dispositions[0]; {
		case disposition == "scheduled" && counts[KeyOwner] == 0:
			add(KeyMissing, KeyOwner)
		case disposition != "scheduled" && counts[KeyOwner] > 0:
			add(OwnerUnexpected, KeyOwner)
		}
		if dispositions[0] == "accepted" {
			add(DispositionAccepted, KeyDisposition)
		}
	}
	return findings
}

// readBlock returns the record lines of a trailer block as entries, flagging an
// entry with a known key that git would continue onto following lines.
func readBlock(block string) (entries []entry) {
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	for i, line := range lines {
		if !recordLine.MatchString(line) {
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		e := entry{key: strings.TrimRight(key, " \t"), value: strings.Trim(value, " \t")}
		if i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
			e.multiline = true
		}
		entries = append(entries, e)
	}
	return entries
}

func validValue(key, value string) bool {
	if value == "" {
		return false
	}
	switch key {
	case KeyCause:
		return true
	case KeyConcept:
		return validConcept(value)
	case KeyDisposition:
		return value == "removed" || value == "scheduled" || value == "accepted"
	case KeyClass:
		return value == "gap" || value == "unenforced" || value == "not-consulted" || value == "none-needed"
	case KeyOwner:
		return !hasSpace(value)
	}
	return false
}

// hasSpace reports whether a token holds any Unicode whitespace, not only blanks.
func hasSpace(value string) bool {
	return strings.IndexFunc(value, unicode.IsSpace) >= 0
}

// validConcept reports whether a value is one whitespace-free token that is an
// invariant ID, or a repository-relative code boundary: a path containing a
// slash, not rooted and without a parent segment, optionally followed by one
// hash and a symbol.
func validConcept(value string) bool {
	if hasSpace(value) {
		return false
	}
	if invariant.MatchString(value) {
		return true
	}
	path, sym, hasSymbol := strings.Cut(value, "#")
	if hasSymbol && !symbol.MatchString(sym) {
		return false
	}
	if !strings.Contains(path, "/") || strings.HasPrefix(path, "/") {
		return false
	}
	return !slices.Contains(strings.Split(path, "/"), "..")
}
