package cli

import (
	"strings"
	"testing"
)

func TestOracleUsageReport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		flags []string
		code  int
	}{
		{"json/usage.json", []string{"--json"}, 0},
		{"human/usage-table.txt", []string{"--format", "table"}, 0},
		{"human/usage-json.txt", []string{"--format", "json"}, 0},
		{"human/usage-yaml.txt", []string{"--format", "yaml"}, 0},
		{"human/usage-value.txt", []string{"--format", "value"}, 0},
		{"json/usage-baseline.json", []string{"--json", "--baseline-since", "2026-09-29", "--baseline-until", "2026-09-30"}, 0},
		{"json/usage-inverted.json", []string{"--json", "--since", "2026-10-03"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newProject(t, "usage")
			args := []string{"usage", "report", "--since", "2026-10-01", "--until", "2026-10-02", "--as-of", "2026-10-02T00:00:00Z"}
			args = append(args, tc.flags...)
			got := p.run(t, false, args...)
			requireExit(t, got, tc.code)
			if strings.HasPrefix(tc.name, "human/") && (got.Stdout != "" || got.Stderr == "") {
				t.Fatal("human usage report must be on stderr only")
			}
			if strings.HasPrefix(tc.name, "json/") && got.Stdout == "" {
				t.Fatal("global JSON report must be on stdout")
			}
			p.check(t, tc.name, got)
		})
	}
}
