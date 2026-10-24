package install

import (
	"regexp"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

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
