package referencecontract

import (
	"fmt"
	"strings"
)

// HeadingMatchError reports that an exact eligible ATX heading could not be
// selected uniquely.
type HeadingMatchError struct {
	Heading string
	Matches int
}

func (e *HeadingMatchError) Error() string {
	if e.Matches == 0 {
		return fmt.Sprintf("eligible ATX heading %q is missing", e.Heading)
	}
	return fmt.Sprintf("eligible ATX heading %q is ambiguous: %d matches", e.Heading, e.Matches)
}

// ExtractSection returns the exact source span beginning at the uniquely
// matching ATX heading and ending before the next eligible ATX heading of the
// same or higher level. Fenced and indented-code headings are ignored; setext
// headings neither match nor terminate a span.
func ExtractSection(markdown, exactHeading string) (string, error) {
	scan := scanMarkdown(markdown)
	matches := headingsMatching(scan.headings, 0, exactHeading)
	if len(matches) != 1 {
		return "", &HeadingMatchError{Heading: exactHeading, Matches: len(matches)}
	}

	match := matches[0]
	end := len(markdown)
	for _, heading := range scan.headings {
		if heading.line > match.line && heading.level <= match.level {
			end = heading.start
			break
		}
	}
	return markdown[match.start:end], nil
}

type markdownScan struct {
	lines    []sourceLine
	headings []atxHeading
}

type sourceLine struct {
	text  string
	start int
	end   int
}

type atxHeading struct {
	line  int
	start int
	level int
	text  string
}

type fence struct {
	char   byte
	length int
}

func scanMarkdown(markdown string) markdownScan {
	lines := splitSourceLines(markdown)
	headings := make([]atxHeading, 0)
	var openFence *fence

	for lineIndex, line := range lines {
		text := strings.TrimSuffix(line.text, "\r")
		if openFence != nil {
			if isClosingFence(text, *openFence) {
				openFence = nil
			}
			continue
		}
		if opened, ok := openingFence(text); ok {
			openFence = &opened
			continue
		}
		level, headingText, ok := parseATXHeading(text)
		if !ok {
			continue
		}
		headings = append(headings, atxHeading{
			line:  lineIndex,
			start: line.start,
			level: level,
			text:  headingText,
		})
	}

	return markdownScan{lines: lines, headings: headings}
}

func splitSourceLines(source string) []sourceLine {
	if source == "" {
		return nil
	}
	lines := make([]sourceLine, 0, strings.Count(source, "\n")+1)
	for start := 0; start < len(source); {
		relativeEnd := strings.IndexByte(source[start:], '\n')
		if relativeEnd < 0 {
			lines = append(lines, sourceLine{text: source[start:], start: start, end: len(source)})
			break
		}
		end := start + relativeEnd + 1
		lines = append(lines, sourceLine{text: source[start : end-1], start: start, end: end})
		start = end
	}
	return lines
}

func headingsMatching(headings []atxHeading, level int, exactText string) []atxHeading {
	matches := make([]atxHeading, 0, 1)
	for _, heading := range headings {
		if (level == 0 || heading.level == level) && heading.text == exactText {
			matches = append(matches, heading)
		}
	}
	return matches
}

func openingFence(line string) (fence, bool) {
	content, ok := afterFenceIndent(line)
	if !ok || len(content) < 3 || (content[0] != '`' && content[0] != '~') {
		return fence{}, false
	}
	char := content[0]
	run := delimiterRun(content, char)
	if run < 3 {
		return fence{}, false
	}
	return fence{char: char, length: run}, true
}

func isClosingFence(line string, open fence) bool {
	content, ok := afterFenceIndent(line)
	if !ok || len(content) < open.length || content[0] != open.char {
		return false
	}
	run := delimiterRun(content, open.char)
	return run >= open.length && strings.Trim(content[run:], " \t") == ""
}

func afterFenceIndent(line string) (string, bool) {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	if spaces > 3 || spaces < len(line) && line[spaces] == '\t' {
		return "", false
	}
	return line[spaces:], true
}

func delimiterRun(line string, char byte) int {
	run := 0
	for run < len(line) && line[run] == char {
		run++
	}
	return run
}

func parseATXHeading(line string) (int, string, bool) {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent < len(line) && line[indent] == '\t' {
		return 0, "", false
	}

	markerEnd := indent
	for markerEnd < len(line) && line[markerEnd] == '#' {
		markerEnd++
	}
	level := markerEnd - indent
	if level < 1 || level > 6 {
		return 0, "", false
	}
	if markerEnd == len(line) {
		return level, "", true
	}
	if line[markerEnd] != ' ' {
		return 0, "", false
	}

	text := strings.Trim(line[markerEnd+1:], " \t")
	if text == "" {
		return level, "", true
	}
	closingStart := len(text)
	for closingStart > 0 && text[closingStart-1] == '#' {
		closingStart--
	}
	if closingStart < len(text) && closingStart > 0 && (text[closingStart-1] == ' ' || text[closingStart-1] == '\t') {
		text = strings.TrimRight(text[:closingStart-1], " \t")
	}
	return level, text, true
}
