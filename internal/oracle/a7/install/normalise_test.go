package install

import (
	"regexp"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

var (
	boxDrawingLine = regexp.MustCompile(`(?m)^[\x{2500}-\x{257F}]+\r?$`)
	horizontalRun  = regexp.MustCompile(`[─═━]+`)
)

// D37: path length changes box width. After harness path normalization, collapse
// horizontal runs on box-drawing-only lines; preserve all other bytes.
func normaliseDecoration(text string) string {
	return boxDrawingLine.ReplaceAllStringFunc(text, func(line string) string {
		return horizontalRun.ReplaceAllString(line, "<BORDER>")
	})
}

func TestDecorationNormalisationPreservesContent(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"rounded", "╭────╮\n╰──╯\n", "╭<BORDER>╮\n╰<BORDER>╯\n"},
		{"double-heavy-mixed", "╔══╗\n┏━━┓\n╚─═━╝", "╔<BORDER>╗\n┏<BORDER>┓\n╚<BORDER>╝"},
		{"separate-runs", "┌──┐┌───┐\n", "┌<BORDER>┐┌<BORDER>┐\n"},
		{"vertical-edges", "│┃║\n╭╮╰╯\n", "│┃║\n╭╮╰╯\n"},
		{"text-and-order", "│ Label: café ─═━ │\nvalue  42\tα\n", "│ Label: café ─═━ │\nvalue  42\tα\n"},
		{"ascii-and-whitespace", "+---+\n ╭──╮\n╰──╯ \n\n", "+---+\n ╭──╮\n╰──╯ \n\n"},
		{"line-endings", "╭──╮\r\n│ text ─ │\r\n╰──╯", "╭<BORDER>╮\r\n│ text ─ │\r\n╰<BORDER>╯"},
		{"invalid-utf8", "╭──\xff╮\n", "╭──\xff╮\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normaliseDecoration(tc.input); got != tc.want {
				t.Fatalf("decoration content mismatch: got %q; want %q", got, tc.want)
			}
		})
	}
}

// AC4 masks are selected by identifier syntax, state key or human label. Stable
// values, timestamps and identifiers in installed artifacts never pass here.
var masks = []struct {
	pattern *regexp.Regexp
	token   string
}{
	{regexp.MustCompile(`\bgoal-[0-9]+\b`), "goal-<ID>"},
	{regexp.MustCompile(`(?m)^(\s*(?:age|time_in_status|elapsed|lease_remaining):\s*)([^\r\n]+)`), "<DURATION>"},
	{regexp.MustCompile(`(?m)^((?:Age|Time in status|Elapsed|Lease remaining):\s*)([^\r\n]+)`), "<DURATION>"},
}

func (f *fixture) normaliser(observations ...[]byte) *oracle.Normaliser {
	n := oracle.NewNormaliser()
	n.Dir(f.home, oracle.TokenHome)
	n.Dir(f.project, oracle.TokenProject)
	n.Dir(f.tmp, oracle.TokenTmp)
	n.Dir(f.root, "<ROOT>")
	for _, observation := range observations {
		for _, mask := range masks {
			for _, match := range mask.pattern.FindAllSubmatch(observation, -1) {
				value := match[0]
				if len(match) > 2 {
					value = match[2]
				}
				n.Literal(string(value), mask.token)
			}
		}
	}
	return n
}

// pathOnly deliberately preserves hashes, fixed timestamps, UUIDs and hex IDs.
func (f *fixture) pathOnly(text string) string {
	for _, pair := range [][2]string{{f.home, "<HOME>"}, {f.project, "<PROJECT>"}, {f.tmp, "<TMP>"}, {f.root, "<ROOT>"}} {
		text = oracle.ReplaceRoot(text, pair[0], pair[1])
	}
	return text
}
