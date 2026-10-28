package a9

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	a9TimeRE  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)
	a9StampRE = regexp.MustCompile(`\b\d{8}-\d{6}\b`)
	a9DateRE  = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	// Host-process correlation suffix of agent-collision and capacity errors:
	// it depends on which supervisors are visible on the host (D43).
	a9CorrelationRE = regexp.MustCompile(`correlation unavailable|observer-visible matching pids? (?:\[[0-9 ]*\]|[0-9]+)`)
	// Process IDs quoted in message text ("pid 12", "pids [3 4]").
	a9PidProseRE = regexp.MustCompile(`(?i)\b(pids?\s+)(\[[0-9 ]*\]|[0-9]+)`)
	a9DigitsRE   = regexp.MustCompile(`[0-9]+`)
	a9VolatileRE = regexp.MustCompile(`(?i)(?:"?\b(?:generation|agent_generation|generation_digest|transition_id|expected_transition|request_id|payload_digest|digest|record_id|supervisor_run_id|run_id|pid|owner_pid)"?\s*:\s*)(?:"[^"\n]*"|'[^'\n]*'|[^\s,}\n]+)|\b(?:[0-9a-f]{64}|[0-9a-f]{40})\b|\b[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\b|liveness-[0-9a-f]+`)
)

// a9Normaliser is fresh for each golden document, including each before/after
// pair. It runs before oracle.Normaliser, which owns path and kind formatting.
type a9Normaliser struct {
	kept       map[string]bool
	instants   []time.Time
	timeTokens []string
	values     map[string]string
}

func newA9Normaliser(shas []string) *a9Normaliser {
	n := &a9Normaliser{kept: map[string]bool{}, values: map[string]string{}}
	for _, sha := range shas {
		n.kept[sha] = true
	}
	return n
}

func (n *a9Normaliser) text(s string) string {
	s = a9TimeRE.ReplaceAllStringFunc(s, n.timestamp)
	s = a9CorrelationRE.ReplaceAllString(s, "<CORRELATION>")
	s = a9StampRE.ReplaceAllString(s, "<STAMP>")
	s = a9DateRE.ReplaceAllString(s, "<DATE>")
	s = a9PidProseRE.ReplaceAllStringFunc(s, func(match string) string {
		parts := a9PidProseRE.FindStringSubmatch(match)
		return parts[1] + a9DigitsRE.ReplaceAllStringFunc(parts[2], n.value)
	})
	return a9VolatileRE.ReplaceAllStringFunc(s, func(match string) string {
		if key, value, ok := strings.Cut(match, ":"); ok {
			prefix := key + ":" + value[:len(value)-len(strings.TrimLeft(value, " \t"))]
			value = strings.TrimSpace(value)
			if value == "null" || value == "''" || value == `""` {
				return match
			}
			quoted := strings.HasPrefix(value, `"`) || strings.HasSuffix(key, `"`)
			token := n.value(strings.Trim(value, `"'`))
			if quoted {
				token = `"` + token + `"`
			}
			return prefix + token
		}
		if strings.HasPrefix(match, "liveness-") {
			return "liveness-" + n.value(strings.TrimPrefix(match, "liveness-"))
		}
		return n.value(match)
	})
}

func (n *a9Normaliser) value(s string) string {
	if n.kept[s] {
		return s
	}
	if token, ok := n.values[s]; ok {
		return token
	}
	token := fmt.Sprintf("<V%d>", len(n.values)+1)
	n.values[s] = token
	return token
}

func (n *a9Normaliser) timestamp(s string) string {
	class := "s"
	if strings.Contains(s, ".") {
		class = "f"
	}
	zone := "N"
	canonical := strings.Replace(s, " ", "T", 1)
	switch {
	case strings.HasSuffix(s, "Z"):
		zone = "Z"
	case strings.ContainsAny(s[19:], "+-"):
		zone = "O"
		if canonical[len(canonical)-3] != ':' {
			canonical = canonical[:len(canonical)-2] + ":" + canonical[len(canonical)-2:]
		}
	default:
		canonical += "Z"
	}
	instant, err := time.Parse(time.RFC3339Nano, canonical)
	if err != nil {
		return s
	}
	for i, earlier := range n.instants {
		if instant.Equal(earlier) {
			return fmt.Sprintf("<%s:%s%s>", n.timeTokens[i], class, zone)
		}
	}
	var token string
	for i, earlier := range n.instants {
		delta := instant.Sub(earlier)
		if delta > 0 && delta%time.Second == 0 {
			token = fmt.Sprintf("T%d+%ds", i+1, int64(delta/time.Second))
			break
		}
	}
	n.instants = append(n.instants, instant)
	if token == "" {
		token = fmt.Sprintf("T%d", len(n.instants))
	}
	n.timeTokens = append(n.timeTokens, token)
	return fmt.Sprintf("<%s:%s%s>", token, class, zone)
}
