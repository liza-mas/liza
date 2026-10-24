package testguard

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Oracle tests resolve the run's starting commit with git, which a
// depth-1 checkout cannot do ("invalid reference"). Every checkout that runs
// tests must therefore fetch full history (D53).

// historyExemptJobs lists the jobs allowed to keep a shallow checkout, per
// workflow file, with the reason. An exempt job must not start running tests.
var historyExemptJobs = map[string]map[string]string{
	"ci.yml": {
		"test-windows": "aggregator: only runs go run ./scripts/ci/testshard over shard artifacts",
	},
}

// exemptRunsTests matches run scripts that would execute tests or a make target.
var exemptRunsTests = regexp.MustCompile(`\bgo test\b|\bmake\b`)

type workflowFile struct {
	Jobs map[string]struct {
		Steps []struct {
			Uses string         `yaml:"uses"`
			Run  string         `yaml:"run"`
			With map[string]any `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// checkoutHistoryViolations returns one message per breach in the workflow
// document data, each naming file, job and step index.
func checkoutHistoryViolations(file string, data []byte, exempt map[string]string) ([]string, error) {
	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	var violations []string
	for _, job := range slices.Sorted(maps.Keys(exempt)) {
		if _, ok := wf.Jobs[job]; !ok {
			violations = append(violations, fmt.Sprintf("%s: exempt job %q does not exist; remove the stale exemption", file, job))
		}
	}
	for _, job := range slices.Sorted(maps.Keys(wf.Jobs)) {
		_, isExempt := exempt[job]
		for i, step := range wf.Jobs[job].Steps {
			if isExempt && exemptRunsTests.MatchString(step.Run) {
				violations = append(violations, fmt.Sprintf("%s: exempt job %q step %d runs tests (go test or make); fetch full history and drop the exemption", file, job, i))
			}
			if isExempt || !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			depth, ok := step.With["fetch-depth"]
			if !ok || fmt.Sprint(depth) != "0" {
				violations = append(violations, fmt.Sprintf("%s: job %q step %d: actions/checkout must set with.fetch-depth: 0", file, job, i))
			}
		}
	}
	return violations, nil
}

func TestCIWorkflowCheckoutsFetchFullHistory(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	workflows := filepath.Join(filepath.Dir(thisFile), "..", "..", ".github", "workflows")

	for _, file := range []string{"ci.yml", "ci-core.yml"} {
		data, err := os.ReadFile(filepath.Join(workflows, file))
		if err != nil {
			t.Fatal(err)
		}
		violations, err := checkoutHistoryViolations(file, data, historyExemptJobs[file])
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range violations {
			t.Error(v)
		}
	}
}

func TestCheckoutHistoryViolationsDetection(t *testing.T) {
	const checkout = "      - uses: actions/checkout@abc # v4\n"
	workflow := func(body string) []byte {
		return []byte("jobs:\n" + body)
	}
	exempt := map[string]string{"agg": "aggregator"}

	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{
			name: "full history passes",
			yaml: "  a:\n    steps:\n" + checkout + "        with:\n          fetch-depth: 0\n",
		},
		{
			name: "missing with names job and step",
			yaml: "  a:\n    steps:\n      - run: echo\n" + checkout,
			want: []string{`ci.yml: job "a" step 1: actions/checkout must set with.fetch-depth: 0`},
		},
		{
			name: "nonzero depth fails",
			yaml: "  a:\n    steps:\n" + checkout + "        with:\n          fetch-depth: 1\n",
			want: []string{`ci.yml: job "a" step 0: actions/checkout must set with.fetch-depth: 0`},
		},
		{
			name: "exempt job keeps shallow checkout",
			yaml: "  agg:\n    steps:\n" + checkout + "      - run: go run ./scripts/ci/testshard\n",
		},
		{
			name: "exempt job running go test fails",
			yaml: "  agg:\n    steps:\n" + checkout + "      - run: go test ./...\n",
			want: []string{`ci.yml: exempt job "agg" step 1 runs tests`},
		},
		{
			name: "exempt job running make fails",
			yaml: "  agg:\n    steps:\n" + checkout + "      - run: make lint\n",
			want: []string{`ci.yml: exempt job "agg" step 1 runs tests`},
		},
		{
			name: "stale exemption fails",
			yaml: "  other:\n    steps:\n      - run: echo\n",
			want: []string{`ci.yml: exempt job "agg" does not exist`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := tt.yaml
			if !strings.Contains(doc, "  agg:") && tt.name != "stale exemption fails" {
				doc += "  agg:\n    steps:\n      - run: echo\n"
			}
			got, err := checkoutHistoryViolations("ci.yml", workflow(doc), exempt)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d violations %q, want %d", len(got), got, len(tt.want))
			}
			for i, w := range tt.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("violation %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}
