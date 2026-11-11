package ops

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestDeferredStateText covers headTailExcerpt at the state limit; this pins
// the limit parameter at another bound, the acceptance failure excerpt.
func TestHeadTailExcerpt(t *testing.T) {
	t.Parallel()
	limit := acceptanceFailureExcerptLimit
	for _, tc := range []struct {
		name, in   string
		head, tail string
	}{
		{name: "short text passes through", in: "exit status 2\nok"},
		{name: "text at the limit passes through", in: strings.Repeat("x", limit)},
		{name: "multibyte runes at both cuts", in: "HEAD" + strings.Repeat("é", limit) + "TAIL", head: "HEAD", tail: "TAIL"},
		{name: "odd-width runes at both cuts", in: "HEAD" + strings.Repeat("€", limit) + "TAIL", head: "HEAD", tail: "TAIL"},
	} {
		got := headTailExcerpt(tc.in, limit)
		if !utf8.ValidString(got) || len(got) > limit {
			t.Errorf("%s: got %d bytes, valid UTF-8 %v; want at most %d valid bytes", tc.name, len(got), utf8.ValidString(got), limit)
		}
		if tc.head == "" {
			if got != tc.in {
				t.Errorf("%s: text within the limit was changed", tc.name)
			}
			continue
		}
		if !strings.HasPrefix(got, tc.head) || !strings.HasSuffix(got, tc.tail) || !strings.Contains(got, "bytes omitted") {
			t.Errorf("%s: got %d bytes without head, tail and omission marker", tc.name, len(got))
		}
	}
}
