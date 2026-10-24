package embedded

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func ensureTomlAssignment(content, section, key, value string) (string, bool) {
	lines := strings.Split(content, "\n")
	start, end := tomlAssignmentRange(lines, section)
	if start == -1 {
		block := fmt.Sprintf("[%s]\n%s = %s\n", section, key, value)
		return appendTomlBlock(content, block), true
	}

	lineStart, lineEnd := findTomlAssignment(lines, start, end, key)
	wanted := key + " = " + value
	if lineStart == -1 {
		updated := insertLines(lines, end, []string{wanted})
		return ensureTrailingNewline(strings.Join(updated, "\n")), true
	}
	if lineEnd > lineStart {
		return content, false
	}

	current := strings.TrimSpace(stripTomlLineComment(strings.Join(lines[lineStart:lineEnd+1], "\n")))
	if current == wanted {
		return content, false
	}

	lines[lineStart] = wanted
	if lineEnd > lineStart {
		lines = append(lines[:lineStart+1], lines[lineEnd+1:]...)
	}
	return ensureTrailingNewline(strings.Join(lines, "\n")), true
}

func ensureTomlAssignmentIfMissing(content, section, key, value string) (string, bool) {
	lines := strings.Split(content, "\n")
	start, end := tomlAssignmentRange(lines, section)
	if start == -1 {
		block := fmt.Sprintf("[%s]\n%s = %s\n", section, key, value)
		return appendTomlBlock(content, block), true
	}

	lineStart, _ := findTomlAssignment(lines, start, end, key)
	if lineStart != -1 {
		return content, false
	}

	updated := insertLines(lines, end, []string{key + " = " + value})
	return ensureTrailingNewline(strings.Join(updated, "\n")), true
}

func tomlAssignmentRange(lines []string, section string) (int, int) {
	if section != "" {
		sectionStart, sectionEnd := findTomlSection(lines, section)
		if sectionStart == -1 {
			return -1, -1
		}
		return sectionStart + 1, sectionEnd
	}

	for i, line := range lines {
		if _, ok := tomlHeaderName(line); ok {
			return 0, i
		}
	}
	return 0, len(lines)
}

func findTomlSection(lines []string, name string) (int, int) {
	start := -1
	for i, line := range lines {
		header, ok := tomlHeaderName(line)
		if !ok {
			continue
		}
		if start == -1 {
			if header == name {
				start = i
			}
			continue
		}
		return start, i
	}
	if start == -1 {
		return -1, -1
	}
	return start, len(lines)
}

func tomlHeaderName(line string) (string, bool) {
	trimmed := strings.TrimSpace(stripTomlLineComment(line))
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "[[") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")), true
}

func findTomlAssignment(lines []string, start, end int, key string) (int, int) {
	for i := start; i < end; i++ {
		trimmed := strings.TrimSpace(stripTomlLineComment(lines[i]))
		if !strings.HasPrefix(trimmed, key) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		arrayEnd := i
		if strings.Contains(rest, "[") && !strings.Contains(rest, "]") {
			for arrayEnd+1 < end {
				arrayEnd++
				if strings.Contains(stripTomlLineComment(lines[arrayEnd]), "]") {
					break
				}
			}
		}
		return i, arrayEnd
	}
	return -1, -1
}

func stripTomlLineComment(line string) string {
	inString := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if r == '#' && !inString {
			return line[:i]
		}
	}
	return line
}

var tomlStringRE = regexp.MustCompile(`"([^"\\]|\\.)*"`)

func extractTomlStrings(content string) []string {
	matches := tomlStringRE.FindAllString(content, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		value, err := strconv.Unquote(match)
		if err == nil {
			values = append(values, value)
		}
	}
	return values
}

func appendToInlineTomlArray(line string, roots []string) string {
	closeIndex := strings.LastIndex(line, "]")
	if closeIndex == -1 {
		return line
	}

	before := strings.TrimRight(line[:closeIndex], " \t")
	after := line[closeIndex:]
	separator := ""
	if !strings.HasSuffix(strings.TrimSpace(before), "[") {
		separator = ","
	}

	values := make([]string, 0, len(roots))
	for _, root := range roots {
		values = append(values, tomlStringValue(root))
	}
	return before + separator + " " + strings.Join(values, ", ") + after
}

func inferArrayValueIndent(lines []string) string {
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "]") {
			continue
		}
		return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	}
	return "  "
}

func insertLines(lines []string, index int, insert []string) []string {
	updated := make([]string, 0, len(lines)+len(insert))
	updated = append(updated, lines[:index]...)
	updated = append(updated, insert...)
	updated = append(updated, lines[index:]...)
	return updated
}

func appendTomlBlock(content, block string) string {
	if strings.TrimSpace(content) == "" {
		return ensureTrailingNewline(block)
	}
	return ensureTrailingNewline(content) + "\n" + ensureTrailingNewline(block)
}

func ensureTrailingNewline(content string) string {
	if strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}

func tomlStringValue(value string) string {
	return strconv.Quote(value)
}

func tomlAssignmentExists(content, section, key string) bool {
	lines := strings.Split(content, "\n")
	start, end := tomlAssignmentRange(lines, section)
	if start == -1 {
		return false
	}
	lineStart, _ := findTomlAssignment(lines, start, end, key)
	return lineStart != -1
}

func tomlAssignmentMatches(content, section, key, value string) bool {
	lines := strings.Split(content, "\n")
	start, end := tomlAssignmentRange(lines, section)
	if start == -1 {
		return false
	}
	lineStart, lineEnd := findTomlAssignment(lines, start, end, key)
	if lineStart == -1 || lineEnd > lineStart {
		return false
	}
	current := strings.TrimSpace(stripTomlLineComment(strings.Join(lines[lineStart:lineEnd+1], "\n")))
	return current == key+" = "+value
}
