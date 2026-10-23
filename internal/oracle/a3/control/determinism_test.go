package control

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	logLineRE = regexp.MustCompile(`(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? [^\n]*(?:\n|$)`)
	instantRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})`)
	hexRE     = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{64}|[0-9a-f]{40})\b`)
)

// C1-C7 have no masked hex fields: any newly observed identifier requires a
// determinism-only classification rather than silently losing its assertion.
var maskedHexFields = []string{}

type determinism struct {
	start      time.Time
	pid        int
	fixtureSHA string
	paths      [][2]string
}

func (n determinism) text(s string) (string, error) {
	s = logLineRE.ReplaceAllString(s, "")
	paths := append([][2]string(nil), n.paths...)
	sort.SliceStable(paths, func(i, j int) bool { return len(paths[i][0]) > len(paths[j][0]) })
	for _, p := range paths {
		s = strings.ReplaceAll(s, p[0], p[1])
	}
	if n.fixtureSHA != "" {
		s = strings.ReplaceAll(s, n.fixtureSHA, "<commit:fixture>")
	}
	var instantErr error
	s = instantRE.ReplaceAllStringFunc(s, func(raw string) string {
		parts := instantRE.FindStringSubmatch(raw)
		zone := parts[2]
		parse := strings.Replace(raw, " ", "T", 1)
		if zone != "Z" && len(zone) == 5 {
			parse = parse[:len(parse)-2] + ":" + parse[len(parse)-2:]
		}
		at, err := time.Parse(time.RFC3339Nano, parse)
		if err != nil {
			instantErr = fmt.Errorf("invalid instant in determinism input")
			return raw
		}
		delta := at.Sub(n.start.UTC().Truncate(time.Second))
		if delta >= -7*24*time.Hour && delta <= 7*24*time.Hour {
			return fmt.Sprintf("<now%+dm>%s", int64(delta/time.Minute), zone)
		}
		clock := raw[:len(raw)-len(zone)]
		clock = strings.NewReplacer("-", ".", ":", ".", "T", " ").Replace(clock)
		return "<at " + clock + ">" + zone
	})
	if instantErr != nil {
		return "", instantErr
	}
	if n.pid > 0 {
		// Match only explicitly labelled PID scalars, including anomaly details.
		// A trailing boundary preserves longer numbers sharing the PID prefix.
		pid := strconv.Itoa(n.pid)
		re := regexp.MustCompile(`(\bpid(?:\"\s*:\s*|:\s*|=| ))` + pid + `\b`)
		s = re.ReplaceAllString(s, "${1}<live-pid>")
	}
	if len(maskedHexFields) != 0 {
		return "", fmt.Errorf("masked hex fields require an explicit field encoder")
	}
	if hexRE.MatchString(s) {
		return "", fmt.Errorf("undeclared 40/64-hex identifier")
	}
	return s, nil
}
