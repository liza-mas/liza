package present

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// D28 (2a44da6f9) exempts only the map-derived status agent collection from
// ordered comparison. Preserve all row contents and every other collection.
func sortStatusAgentsJSON(value any) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	if result, ok := object["result"].(map[string]any); ok {
		object = result
	}
	agents, ok := object["agents"].([]any)
	if !ok {
		return
	}
	id := func(v any) string {
		row, _ := v.(map[string]any)
		name, _ := row["id"].(string)
		return name
	}
	sort.SliceStable(agents, func(i, j int) bool { return id(agents[i]) < id(agents[j]) })
}

var statusAgentTable = regexp.MustCompile(`(?m)(^=== AGENTS ===\n[^\n]*\n)((?:[^\n]+\n)+)`)
var statusAgentYAML = regexp.MustCompile(`(?m)^agents:\n(?:[ \t]+[^\n]*\n)+`)

func sortStatusAgentsHuman(text string) string {
	text = statusAgentTable.ReplaceAllStringFunc(text, func(section string) string {
		parts := strings.SplitN(section, "\n", 3)
		return parts[0] + "\n" + parts[1] + "\n" + oracle.SortLines(parts[2])
	})
	return statusAgentYAML.ReplaceAllStringFunc(text, func(section string) string {
		var rows []string
		for _, line := range strings.SplitAfter(strings.TrimPrefix(section, "agents:\n"), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "- id: ") {
				rows = append(rows, line)
			} else if len(rows) > 0 {
				rows[len(rows)-1] += line
			}
		}
		sort.Strings(rows)
		return "agents:\n" + strings.Join(rows, "")
	})
}

func TestA7StatusOrderNormalization(t *testing.T) {
	skipUnlessLinux(t)
	input := "=== TASKS ===\nz\na\n\n=== AGENTS ===\nID  Task\ncoder-1  task-c\narchitect-1  \n\n=== OTHER ===\nz\na\n"
	want := "=== TASKS ===\nz\na\n\n=== AGENTS ===\nID  Task\narchitect-1  \ncoder-1  task-c\n\n=== OTHER ===\nz\na\n"
	if got := sortStatusAgentsHuman(input); got != want {
		t.Fatalf("D28 changed content or other order: %q", got)
	}
	input = "agents:\n    - id: coder-1\n      pid: 5000001\n    - id: architect-1\n      pid: 5000003\nother:\n    - z\n    - a\n"
	want = "agents:\n    - id: architect-1\n      pid: 5000003\n    - id: coder-1\n      pid: 5000001\nother:\n    - z\n    - a\n"
	if got := sortStatusAgentsHuman(input); got != want {
		t.Fatalf("D28 YAML changed content or other order: %q", got)
	}
	first := map[string]any{"id": "coder-1", "pid": 5000001}
	second := map[string]any{"id": "architect-1", "pid": 5000003}
	value := map[string]any{"agents": []any{first, second}, "tasks": []any{"z", "a"}}
	sortStatusAgentsJSON(value)
	agents := value["agents"].([]any)
	if agents[0].(map[string]any)["pid"] != 5000003 || agents[1].(map[string]any)["pid"] != 5000001 || value["tasks"].([]any)[0] != "z" {
		t.Fatal("D28 JSON changed content or other order")
	}
}

func TestOracleStatus(t *testing.T) {
	skipUnlessLinux(t)
	checkInvocations(t, "status", []invocation{
		{"default.txt", oracle.Human, []string{"status"}},
		{"json.json", oracle.JSON, []string{"status", "--json"}},
		{"format-json.json", oracle.JSON, []string{"status", "--format", "json"}},
		{"format-yaml.txt", oracle.Human, []string{"status", "--format", "yaml"}},
		{"detailed.txt", oracle.Human, []string{"status", "--detailed"}},
		{"detailed-json.json", oracle.JSON, []string{"status", "--detailed", "--json"}},
	})
	t.Run("refusal-non-repository", func(t *testing.T) {
		e := newEnvironment(t)
		checkResult(t, e, oracle.JSON, "status/refusal-non-repository.json", runCLI(t, e, e.project, nil, "-C", e.project, "status", "--json"))
	})
}
