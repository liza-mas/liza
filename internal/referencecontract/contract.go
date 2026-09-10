// Package referencecontract parses and resolves the reference-first Markdown
// contract used by semantic artifacts.
package referencecontract

import (
	"encoding/json"
	"fmt"
	pathpkg "path"
	"strings"
	"unicode"
)

const sourceReferencesHeading = "Source References"

// Repository is the narrow immutable Git object interface needed to resolve
// reference contracts. Implementations must not read from the working tree.
type Repository interface {
	ResolveCommit(ref string) (string, error)
	ReadBlob(revision, path string) (string, error)
	BlobOID(revision, path string) (string, error)
}

// Contract is the parsed strict Source References section. A nil Contract from
// Parse means the document is a marker-free legacy artifact.
type Contract struct {
	SourceRevision     string
	DirectReferences   []DirectReference
	ObligationCoverage []ObligationCoverage
}

// DirectReference identifies an exact Markdown heading at its optional
// per-reference revision. An empty Revision uses Contract.SourceRevision.
type DirectReference struct {
	ID       string
	Path     string
	Heading  string
	Revision string
}

// EffectiveRevision returns the adjacent override when present, otherwise the
// contract's default source revision.
func (r DirectReference) EffectiveRevision(sourceRevision string) string {
	if r.Revision != "" {
		return r.Revision
	}
	return sourceRevision
}

// ObligationCoverage maps one semantic obligation to one or more declared
// direct reference IDs.
type ObligationCoverage struct {
	ID           string
	ReferenceIDs []string
}

// Parse parses a strict Source References section. It returns (nil, nil) when
// no eligible marker exists, preserving the legacy-artifact path. Once an
// eligible marker is present, all malformed input fails closed.
func Parse(markdown string) (*Contract, error) {
	scan := scanMarkdown(markdown)
	markers := headingsMatching(scan.headings, 2, sourceReferencesHeading)
	if len(markers) == 0 {
		return nil, nil
	}
	if len(markers) != 1 {
		return nil, fmt.Errorf("source references: expected exactly one eligible level-2 marker, got %d", len(markers))
	}

	marker := markers[0]
	sectionEndLine := len(scan.lines)
	for _, heading := range scan.headings {
		if heading.line > marker.line && heading.level <= marker.level {
			sectionEndLine = heading.line
			break
		}
	}

	headingAtLine := make(map[int]atxHeading)
	for _, heading := range scan.headings {
		headingAtLine[heading.line] = heading
	}

	contract := &Contract{}
	referenceIDs := make(map[string]struct{})
	obligationIDs := make(map[string]struct{})
	state := parseSourceRevision

	for lineIndex := marker.line + 1; lineIndex < sectionEndLine; lineIndex++ {
		line := scan.lines[lineIndex].text
		if strings.TrimSpace(line) == "" {
			continue
		}

		heading, isHeading := headingAtLine[lineIndex]
		switch state {
		case parseSourceRevision:
			if isHeading {
				return nil, parseLineError(lineIndex, "expected Source revision declaration before heading %q", heading.text)
			}
			revision, err := parseSourceRevisionLine(line)
			if err != nil {
				return nil, parseLineError(lineIndex, "%v", err)
			}
			contract.SourceRevision = revision
			state = parseDirectHeading

		case parseDirectHeading:
			if !isHeading || heading.level != 3 || heading.text != "Direct References" {
				return nil, parseLineError(lineIndex, "expected level-3 Direct References heading")
			}
			state = parseDirectEntries

		case parseDirectEntries:
			if isHeading {
				if heading.level != 3 || heading.text != "Obligation Coverage" {
					return nil, parseLineError(lineIndex, "expected level-3 Obligation Coverage heading")
				}
				if len(contract.DirectReferences) == 0 {
					return nil, parseLineError(lineIndex, "Direct References list must not be empty")
				}
				state = parseCoverageEntries
				continue
			}

			reference, err := parseDirectReferenceLine(line)
			if err != nil {
				return nil, parseLineError(lineIndex, "%v", err)
			}
			if _, duplicate := referenceIDs[reference.ID]; duplicate {
				return nil, parseLineError(lineIndex, "duplicate reference ID %q", reference.ID)
			}
			referenceIDs[reference.ID] = struct{}{}
			contract.DirectReferences = append(contract.DirectReferences, reference)

		case parseCoverageEntries:
			if isHeading {
				return nil, parseLineError(lineIndex, "unexpected heading %q in Obligation Coverage", heading.text)
			}
			coverage, err := parseCoverageLine(line)
			if err != nil {
				return nil, parseLineError(lineIndex, "%v", err)
			}
			if _, duplicate := obligationIDs[coverage.ID]; duplicate {
				return nil, parseLineError(lineIndex, "duplicate obligation ID %q", coverage.ID)
			}
			for _, referenceID := range coverage.ReferenceIDs {
				if _, declared := referenceIDs[referenceID]; !declared {
					return nil, parseLineError(lineIndex, "obligation %q names undeclared reference ID %q", coverage.ID, referenceID)
				}
			}
			obligationIDs[coverage.ID] = struct{}{}
			contract.ObligationCoverage = append(contract.ObligationCoverage, coverage)
		}
	}

	switch state {
	case parseSourceRevision:
		return nil, fmt.Errorf("source references: missing Source revision declaration")
	case parseDirectHeading:
		return nil, fmt.Errorf("source references: missing Direct References heading")
	case parseDirectEntries:
		return nil, fmt.Errorf("source references: missing Obligation Coverage heading")
	case parseCoverageEntries:
		if len(contract.ObligationCoverage) == 0 {
			return nil, fmt.Errorf("source references: Obligation Coverage list must not be empty")
		}
	}

	return contract, nil
}

type parseState uint8

const (
	parseSourceRevision parseState = iota
	parseDirectHeading
	parseDirectEntries
	parseCoverageEntries
)

func parseSourceRevisionLine(line string) (string, error) {
	cursor := lineCursor{input: line}
	if !cursor.consume("Source revision: ") {
		return "", fmt.Errorf("expected source revision declaration")
	}
	revision, err := cursor.jsonString()
	if err != nil {
		return "", fmt.Errorf("invalid Source revision: %w", err)
	}
	if !cursor.done() {
		return "", fmt.Errorf("unexpected content after Source revision")
	}
	if !validOID(revision) {
		return "", fmt.Errorf("source revision must be a 40-character lowercase hexadecimal object ID")
	}
	return revision, nil
}

func parseDirectReferenceLine(line string) (DirectReference, error) {
	cursor := lineCursor{input: line}
	if !cursor.consume("- ") {
		return DirectReference{}, fmt.Errorf("invalid Direct References entry")
	}
	id, err := cursor.jsonString()
	if err != nil {
		return DirectReference{}, fmt.Errorf("invalid reference ID: %w", err)
	}
	if err := validateID(id, "reference ID"); err != nil {
		return DirectReference{}, err
	}
	if !cursor.consume(": ") {
		return DirectReference{}, fmt.Errorf("expected ': ' after reference ID")
	}
	target, err := cursor.jsonString()
	if err != nil {
		return DirectReference{}, fmt.Errorf("invalid reference target: %w", err)
	}

	reference := DirectReference{ID: id}
	reference.Path, reference.Heading, err = parseTarget(target)
	if err != nil {
		return DirectReference{}, err
	}

	if cursor.done() {
		return reference, nil
	}
	if !cursor.consume(" @ ") {
		return DirectReference{}, fmt.Errorf("unexpected content after reference target")
	}
	reference.Revision, err = cursor.jsonString()
	if err != nil {
		return DirectReference{}, fmt.Errorf("invalid reference revision override: %w", err)
	}
	if !cursor.done() {
		return DirectReference{}, fmt.Errorf("unexpected content after reference revision override")
	}
	if !validOID(reference.Revision) {
		return DirectReference{}, fmt.Errorf("reference revision override must be a 40-character lowercase hexadecimal object ID")
	}
	return reference, nil
}

func parseCoverageLine(line string) (ObligationCoverage, error) {
	cursor := lineCursor{input: line}
	if !cursor.consume("- ") {
		return ObligationCoverage{}, fmt.Errorf("invalid Obligation Coverage entry")
	}
	id, err := cursor.jsonString()
	if err != nil {
		return ObligationCoverage{}, fmt.Errorf("invalid obligation ID: %w", err)
	}
	if err := validateID(id, "obligation ID"); err != nil {
		return ObligationCoverage{}, err
	}
	if !cursor.consume(" -> ") {
		return ObligationCoverage{}, fmt.Errorf("expected ' -> ' after obligation ID")
	}

	coverage := ObligationCoverage{ID: id}
	for {
		referenceID, parseErr := cursor.jsonString()
		if parseErr != nil {
			return ObligationCoverage{}, fmt.Errorf("invalid covered reference ID: %w", parseErr)
		}
		if err := validateID(referenceID, "covered reference ID"); err != nil {
			return ObligationCoverage{}, err
		}
		coverage.ReferenceIDs = append(coverage.ReferenceIDs, referenceID)
		if cursor.done() {
			break
		}
		if !cursor.consume(", ") {
			return ObligationCoverage{}, fmt.Errorf("expected ', ' between covered reference IDs")
		}
	}
	return coverage, nil
}

func parseTarget(target string) (string, string, error) {
	path, heading, found := strings.Cut(target, "#")
	if !found || path == "" || heading == "" {
		return "", "", fmt.Errorf("reference target must contain a non-empty path and exact heading separated by '#'")
	}
	if err := validateMarkdownPath(path); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(heading) != heading || strings.ContainsAny(heading, "\r\n") {
		return "", "", fmt.Errorf("reference heading must be a non-empty trimmed single-line string")
	}
	return path, heading, nil
}

func validateMarkdownPath(value string) error {
	if strings.HasPrefix(value, "/") || pathpkg.IsAbs(value) {
		return fmt.Errorf("reference path must be repository-relative")
	}
	if len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && value[2] == '/' {
		return fmt.Errorf("reference path must be repository-relative")
	}
	if strings.ContainsAny(value, "\\#") {
		return fmt.Errorf("reference path must use slash separators and contain no fragment delimiter")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("reference path must not contain control characters")
		}
	}
	if pathpkg.Clean(value) != value || value == "." {
		return fmt.Errorf("reference path must be clean")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("reference path must not contain empty, '.' or '..' segments")
		}
	}
	if pathpkg.Ext(value) != ".md" {
		return fmt.Errorf("reference path must name a Markdown (.md) file")
	}
	return nil
}

func validateID(value, kind string) error {
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be a non-empty single-line string", kind)
	}
	return nil
}

func validOID(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func parseLineError(zeroBasedLine int, format string, args ...any) error {
	return fmt.Errorf("source references line %d: %s", zeroBasedLine+1, fmt.Sprintf(format, args...))
}

type lineCursor struct {
	input string
	pos   int
}

func (c *lineCursor) consume(literal string) bool {
	if !strings.HasPrefix(c.input[c.pos:], literal) {
		return false
	}
	c.pos += len(literal)
	return true
}

func (c *lineCursor) jsonString() (string, error) {
	if c.pos >= len(c.input) || c.input[c.pos] != '"' {
		return "", fmt.Errorf("expected JSON string")
	}
	decoder := json.NewDecoder(strings.NewReader(c.input[c.pos:]))
	var value string
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	c.pos += int(decoder.InputOffset())
	return value, nil
}

func (c *lineCursor) done() bool {
	return c.pos == len(c.input)
}
