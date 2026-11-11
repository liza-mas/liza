package ops

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/paths"
)

// outputExcerptMarker replaces the omitted middle of a bounded excerpt. The
// tail is kept because test runners and build tools report failures last.
const outputExcerptMarker = "\n[... %d bytes omitted ...]\n"

// headTailExcerpt returns text as valid UTF-8 of at most limit bytes: whole
// when it fits, otherwise its head and tail around outputExcerptMarker, cut on
// rune starts. Call it on masked text, so no cut can split a secret before
// masking sees it.
func headTailExcerpt(text string, limit int) string {
	text = strings.ToValidUTF8(text, "�")
	if len(text) <= limit {
		return text
	}
	// The marker's count never has more digits than len(text).
	keep := limit - len(fmt.Sprintf(outputExcerptMarker, len(text)))
	head, tail := keep/2, len(text)-(keep-keep/2)
	for head > 0 && !utf8.RuneStart(text[head]) {
		head--
	}
	for tail < len(text) && !utf8.RuneStart(text[tail]) {
		tail++
	}
	return text[:head] + fmt.Sprintf(outputExcerptMarker, tail-head) + text[tail:]
}

// projectToolResultsDir is the project's content-addressed tool-result store,
// which also keeps failed validation output that diagnostics reference.
func projectToolResultsDir(projectRoot string) string {
	return filepath.Join(projectRoot, paths.ProjectDirName(), "tool-results")
}
