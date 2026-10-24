package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	instantRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)
	logRE     = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)
	// TC6: these fields varied in the starting-commit probes. Other digests
	// remain digit-for-digit assertions, protected from harness folding.
	volatileRE  = regexp.MustCompile(`\b(?:transition_id|completed_transition_id|instance_id|key_id)"?\s*:\s*"?([0-9a-fA-F]{16,64})\b`)
	ledgerKeyRE = regexp.MustCompile(`^[0-9a-fA-F]{64}:$`)
	boundaryRE  = regexp.MustCompile(`expected_transition:\s*([0-9a-fA-F]{64})\b`)
)

type mask struct {
	start            time.Time
	commits          map[string]string
	paths            [][2]string
	volatile         map[string]bool
	instancePrefixes map[string]string
	ids              map[string]string
	pid              int
	pidRE            *regexp.Regexp
}

func newMask(start time.Time, commits map[string]string, paths map[string]string, pid int) *mask {
	// D38: only state PID fields and the CLI's labelled recovery messages
	// carry volatile PIDs. Equal configuration values must remain asserted.
	pidRE := regexp.MustCompile(`(?m)(^[ \t]*pid:[ \t]*|\bPID[ \t]+)` + strconv.Itoa(pid) + `\b`)
	m := &mask{start: start.Truncate(time.Second), commits: commits, volatile: map[string]bool{}, instancePrefixes: map[string]string{}, ids: map[string]string{}, pid: pid, pidRE: pidRE}
	for path, token := range paths {
		m.paths = append(m.paths, [2]string{path, token})
	}
	sort.Slice(m.paths, func(i, j int) bool { return len(m.paths[i][0]) > len(m.paths[j][0]) })
	return m
}

func (m *mask) discover(sources ...string) {
	for _, s := range sources {
		for _, match := range volatileRE.FindAllStringSubmatch(s, -1) {
			m.volatile[match[1]] = true
			if strings.HasPrefix(match[0], "instance_id") && len(match[1]) == 64 {
				m.discoverInstance(match[1])
			}
		}
		// The root state ledger carries instance identities as keys, even
		// when human output supplies only a prefix and no instance_id field.
		inLedger, indent := false, 0
		for _, line := range strings.Split(s, "\n") {
			if !strings.HasPrefix(line, " ") {
				inLedger, indent = line == "runtime_inputs:", 0
				continue
			}
			if !inLedger {
				continue
			}
			key := strings.TrimLeft(line, " ")
			spaces := len(line) - len(key)
			if indent == 0 {
				indent = spaces
			}
			if spaces == indent && ledgerKeyRE.MatchString(key) {
				m.discoverInstance(strings.TrimSuffix(key, ":"))
			}
		}
	}
}

// discoverBoundaries marks the receipt boundaries recorded in a final state as
// run-varying identities.
func (m *mask) discoverBoundaries(state string) {
	for _, match := range boundaryRE.FindAllStringSubmatch(state, -1) {
		m.volatile[match[1]] = true
	}
}

func (m *mask) discoverInstance(value string) {
	m.volatile[value] = true
	// Match the CLI's 16-character display to its full identity, sharing the
	// token regardless of which form first appears in the composed golden.
	m.instancePrefixes[value[:16]] = value
}

func (m *mask) identifier(value string) string {
	for name, sha := range m.commits {
		if value == sha {
			return "<commit:" + name + ">"
		}
	}
	if full := m.instancePrefixes[value]; full != "" {
		value = full
	}
	if m.volatile[value] {
		if m.ids[value] == "" {
			m.ids[value] = fmt.Sprintf("<id-%d>", len(m.ids)+1)
		}
		return m.ids[value]
	}
	var chunks []string
	for i := 0; i < len(value); i += 8 {
		end := min(i+8, len(value))
		chunks = append(chunks, value[i:end])
	}
	return "<hex:" + strings.Join(chunks, ".") + ">"
}

func (m *mask) text(s string) string {
	for _, pair := range m.paths {
		s = strings.ReplaceAll(s, pair[0], pair[1])
	}
	s = instantRE.ReplaceAllStringFunc(s, func(value string) string {
		instant, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return value
		}
		zone := "Z"
		if !strings.HasSuffix(value, "Z") {
			zone = value[len(value)-6:]
		}
		delta := instant.Sub(m.start)
		if delta >= -7*24*time.Hour && delta <= 7*24*time.Hour {
			minutes := int64(delta / time.Minute)
			return fmt.Sprintf("<now%+dm %s>", minutes, zone)
		}
		date := strings.TrimSuffix(value, zone)
		date = strings.NewReplacer("-", "", ":", "").Replace(date)
		return "<at " + date + " " + zone + ">"
	})
	// Include 16-hex runtime-key IDs as well as the harness's 40/64-hex set.
	idsRE := regexp.MustCompile(`\b(?:[0-9a-fA-F]{64}|[0-9a-fA-F]{40}|[0-9a-fA-F]{16})\b`)
	s = idsRE.ReplaceAllStringFunc(s, func(value string) string {
		if len(value) == 16 && !m.volatile[value] && m.instancePrefixes[value] == "" {
			return value
		}
		return m.identifier(value)
	})
	return m.pidRE.ReplaceAllString(s, "${1}<live-pid>")
}

// Object keys are traversed in canonical order before assigning ID tokens.
func (m *mask) document(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := map[string]any{}
		for _, key := range keys {
			item := v[key]
			if pid, ok := item.(json.Number); key == "pid" && ok && pid.String() == strconv.Itoa(m.pid) {
				item = "<live-pid>"
			}
			out[m.text(key)] = m.document(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = m.document(item)
		}
		return out
	case string:
		return m.text(v)
	default:
		return value
	}
}

func stripLogs(s string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		if !logRE.MatchString(line) {
			out.WriteString(line)
		}
	}
	return out.String()
}

func outcomeLines(t *testing.T, project string) string {
	t.Helper()
	dir := filepath.Join(project, ".liza", "lifecycle-metrics")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal("read lifecycle outcomes:", err)
	}
	counts := map[string]map[string]int{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".lock.owner.json") {
			continue
		}
		data := readFile(t, filepath.Join(dir, name))
		var current struct {
			Counts map[string]map[string]int `json:"counts"`
		}
		if err := json.Unmarshal(data, &current); err != nil {
			t.Fatalf("outcome file %s: %v", name, err)
		}
		for op, outcomes := range current.Counts {
			if counts[op] == nil {
				counts[op] = map[string]int{}
			}
			for outcome, n := range outcomes {
				counts[op][outcome] += n
			}
		}
	}
	var lines []string
	for op, outcomes := range counts {
		for outcome, n := range outcomes {
			if n != 0 {
				lines = append(lines, fmt.Sprintf("%s %s %d\n", op, outcome, n))
			}
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "")
}
