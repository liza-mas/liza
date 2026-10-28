package referencecontract

import (
	"fmt"
	"strings"
)

// referencesSection is the one eligible Source References section of a
// document: its marker, the first line past it, and the headings by line.
type referencesSection struct {
	scan          markdownScan
	marker        atxHeading
	endLine       int
	headingAtLine map[int]atxHeading
}

// locateSourceReferences finds the Source References marker. It returns
// (nil, nil) when no eligible marker exists and an error unless exactly one does.
func locateSourceReferences(scan markdownScan) (*referencesSection, error) {
	markers := headingsMatching(scan.headings, sourceReferencesHeadingLevel, sourceReferencesHeading)
	if len(markers) == 0 {
		return nil, nil
	}
	if len(markers) != 1 {
		return nil, fmt.Errorf("source references: expected exactly one eligible level-2 marker, got %d", len(markers))
	}

	marker := markers[0]
	endLine := len(scan.lines)
	for _, heading := range scan.headings {
		if heading.line > marker.line && heading.level <= marker.level {
			endLine = heading.line
			break
		}
	}

	headingAtLine := make(map[int]atxHeading)
	for _, heading := range scan.headings {
		headingAtLine[heading.line] = heading
	}
	return &referencesSection{scan: scan, marker: marker, endLine: endLine, headingAtLine: headingAtLine}, nil
}

type parseState uint8

const (
	parseSourceRevision parseState = iota
	parseDirectHeading
	parseDirectEntries
	parseCoverageEntries
)

// contractParser is the section's line state machine: source revision, Direct
// References heading, direct entries, Obligation Coverage entries.
type contractParser struct {
	contract      *Contract
	referenceIDs  map[string]struct{}
	obligationIDs map[string]struct{}
	state         parseState
}

func newContractParser() *contractParser {
	return &contractParser{
		contract:      &Contract{},
		referenceIDs:  make(map[string]struct{}),
		obligationIDs: make(map[string]struct{}),
		state:         parseSourceRevision,
	}
}

// consume applies one non-blank section line to the current state.
func (p *contractParser) consume(lineIndex int, line string, heading atxHeading, isHeading bool) error {
	switch p.state {
	case parseSourceRevision:
		return p.consumeSourceRevision(lineIndex, line, heading, isHeading)
	case parseDirectHeading:
		return p.consumeDirectHeading(lineIndex, heading, isHeading)
	case parseDirectEntries:
		return p.consumeDirectEntry(lineIndex, line, heading, isHeading)
	case parseCoverageEntries:
		return p.consumeCoverageEntry(lineIndex, line, heading, isHeading)
	}
	return nil
}

func (p *contractParser) consumeSourceRevision(lineIndex int, line string, heading atxHeading, isHeading bool) error {
	if isHeading {
		return parseLineError(lineIndex, "expected Source revision declaration before heading %q", heading.text)
	}
	revision, err := parseSourceRevisionLine(line)
	if err != nil {
		return parseLineError(lineIndex, "%v", err)
	}
	p.contract.SourceRevision = revision
	p.state = parseDirectHeading
	return nil
}

func (p *contractParser) consumeDirectHeading(lineIndex int, heading atxHeading, isHeading bool) error {
	if !isHeading || heading.level != contractHeadingLevel || heading.text != "Direct References" {
		return parseLineError(lineIndex, "expected level-3 Direct References heading")
	}
	p.state = parseDirectEntries
	return nil
}

func (p *contractParser) consumeDirectEntry(lineIndex int, line string, heading atxHeading, isHeading bool) error {
	if isHeading {
		if heading.level != contractHeadingLevel || heading.text != "Obligation Coverage" {
			return parseLineError(lineIndex, "expected level-3 Obligation Coverage heading")
		}
		if len(p.contract.DirectReferences) == 0 {
			return parseLineError(lineIndex, "Direct References list must not be empty")
		}
		p.state = parseCoverageEntries
		return nil
	}

	reference, err := parseDirectReferenceLine(line)
	if err != nil {
		return parseLineError(lineIndex, "%v", err)
	}
	if _, duplicate := p.referenceIDs[reference.ID]; duplicate {
		return parseLineError(lineIndex, "duplicate reference ID %q", reference.ID)
	}
	p.referenceIDs[reference.ID] = struct{}{}
	p.contract.DirectReferences = append(p.contract.DirectReferences, reference)
	return nil
}

func (p *contractParser) consumeCoverageEntry(lineIndex int, line string, heading atxHeading, isHeading bool) error {
	if isHeading {
		return parseLineError(lineIndex, "unexpected heading %q in Obligation Coverage", heading.text)
	}
	coverage, err := parseCoverageLine(line)
	if err != nil {
		return parseLineError(lineIndex, "%v", err)
	}
	if _, duplicate := p.obligationIDs[coverage.ID]; duplicate {
		return parseLineError(lineIndex, "duplicate obligation ID %q", coverage.ID)
	}
	for _, referenceID := range coverage.ReferenceIDs {
		if _, declared := p.referenceIDs[referenceID]; !declared {
			return parseLineError(lineIndex, "obligation %q names undeclared reference ID %q", coverage.ID, referenceID)
		}
	}
	p.obligationIDs[coverage.ID] = struct{}{}
	p.contract.ObligationCoverage = append(p.contract.ObligationCoverage, coverage)
	return nil
}

// finish checks the section ended in a complete state and returns the contract.
func (p *contractParser) finish() (*Contract, error) {
	switch p.state {
	case parseSourceRevision:
		return nil, fmt.Errorf("source references: missing Source revision declaration")
	case parseDirectHeading:
		return nil, fmt.Errorf("source references: missing Direct References heading")
	case parseDirectEntries:
		return nil, fmt.Errorf("source references: missing Obligation Coverage heading")
	case parseCoverageEntries:
		if len(p.contract.ObligationCoverage) == 0 {
			return nil, fmt.Errorf("source references: Obligation Coverage list must not be empty")
		}
	}
	return p.contract, nil
}

// parseSection feeds the section's non-blank lines to the state machine.
func parseSection(section *referencesSection) (*Contract, error) {
	parser := newContractParser()
	for lineIndex := section.marker.line + 1; lineIndex < section.endLine; lineIndex++ {
		line := section.scan.lines[lineIndex].text
		if strings.TrimSpace(line) == "" {
			continue
		}
		heading, isHeading := section.headingAtLine[lineIndex]
		if err := parser.consume(lineIndex, line, heading, isHeading); err != nil {
			return nil, err
		}
	}
	return parser.finish()
}
