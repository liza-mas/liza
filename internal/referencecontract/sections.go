package referencecontract

import (
	"encoding/json"
	"fmt"
	"strings"
)

const sectionReferencesPrefix = "**Direct references:**"

// SectionReferenceIDs reads an explicit read set in a heading's own body,
// before its first subsection. Fences, indented code and blockquotes cannot
// declare a read set. Presence distinguishes [] from a legacy missing list.
func SectionReferenceIDs(markdown, heading string) ([]string, bool, error) {
	scan := scanMarkdown(markdown)
	matches := headingsMatching(scan.headings, 0, heading)
	if len(matches) != 1 {
		present := false
		for _, match := range matches {
			present = present || len(sectionDeclarationLines(scan, match)) > 0
		}
		return nil, present, &HeadingMatchError{Heading: heading, Matches: len(matches)}
	}
	var ids []string
	present := false
	for _, line := range sectionDeclarationLines(scan, matches[0]) {
		text := strings.TrimSpace(scan.lines[line].text)
		if present {
			return nil, true, fmt.Errorf("section %q: duplicate Direct references declaration", heading)
		}
		present = true
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(text, sectionReferencesPrefix))), &ids); err != nil || ids == nil {
			return nil, true, fmt.Errorf("section %q: Direct references must be a JSON array of strings", heading)
		}
		seen := make(map[string]bool)
		for _, id := range ids {
			if err := validateID(id, "section reference ID"); err != nil {
				return nil, true, fmt.Errorf("section %q: %w", heading, err)
			}
			if seen[id] {
				return nil, true, fmt.Errorf("section %q: duplicate reference ID %q", heading, id)
			}
			seen[id] = true
		}
	}
	return ids, present, nil
}

func sectionDeclarationLines(scan markdownScan, heading atxHeading) []int {
	end := len(scan.lines)
	for _, next := range scan.headings {
		if next.line > heading.line {
			end = next.line
			break
		}
	}
	var lines []int
	for line := heading.line + 1; line < end; line++ {
		if scan.eligible[line] && strings.HasPrefix(strings.TrimSpace(scan.lines[line].text), sectionReferencesPrefix) {
			lines = append(lines, line)
		}
	}
	return lines
}

// ValidateSectionReferences checks new authored read sets against their
// carrier's IDs. Merged legacy carriers are never retroactively rejected here.
func ValidateSectionReferences(markdown string, contract *Contract) error {
	known := make(map[string]bool)
	for _, ref := range contract.DirectReferences {
		known[ref.ID] = true
	}
	for _, heading := range scanMarkdown(markdown).headings {
		ids, present, err := SectionReferenceIDs(markdown, heading.text)
		if !present {
			continue
		}
		if err != nil {
			return err
		}
		for _, id := range ids {
			if !known[id] {
				return fmt.Errorf("section %q: undeclared direct reference ID %q", heading.text, id)
			}
		}
	}
	return nil
}

// SectionAtLine maps a 1-based citation to the innermost eligible ATX section
// and its inclusive 1-based bounds at the cited immutable revision.
func SectionAtLine(markdown string, line int) (heading string, startLine, endLine int, err error) {
	scan := scanMarkdown(markdown)
	if line < 1 || line > len(scan.lines) {
		return "", 0, 0, fmt.Errorf("line %d is outside the document", line)
	}
	var selected *atxHeading
	for i := range scan.headings {
		candidate := &scan.headings[i]
		if candidate.line >= line {
			break
		}
		selected = candidate
	}
	if selected == nil {
		return "", 0, 0, fmt.Errorf("line %d precedes the first eligible heading", line)
	}
	if matches := headingsMatching(scan.headings, 0, selected.text); len(matches) != 1 {
		return "", 0, 0, &HeadingMatchError{Heading: selected.text, Matches: len(matches)}
	}
	endLine = len(scan.lines)
	for _, next := range scan.headings {
		if next.line > selected.line && next.level <= selected.level {
			endLine = next.line
			break
		}
	}
	return selected.text, selected.line + 1, endLine, nil
}
