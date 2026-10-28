package a9

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA9Normaliser(t *testing.T) {
	t.Parallel()
	const fixed = "1234567890123456789012345678901234567890"
	cases := []struct{ name, input, want string }{
		{"zones", "2020-01-01T00:00:00Z 2020-01-01T02:00:00+02:00", "<T1:sZ> <T1:sO>"},
		{"lease", "2020-01-01T00:00:00.123Z 2020-01-01T00:30:00.123Z", "<T1:fZ> <T1+1800s:fZ>"},
		{"lowest", "2020-01-01T00:00:00Z 2020-01-01T00:00:02Z 2020-01-01T00:00:03Z", "<T1:sZ> <T1+2s:sZ> <T1+3s:sZ>"},
		{"fractions", "2020-01-01T00:00:00.123456789Z 2020-01-01T00:00:00.12345678Z", "<T1:fZ> <T2:fZ>"},
		{"equality", "generation: alpha\nrequest_id: alpha\npid: 123\nowner_pid: 123\n", "generation: <V1>\nrequest_id: <V1>\npid: <V2>\nowner_pid: <V2>\n"},
		{"json", `{"generation":"abc","request_id":"abc","pid":123,"lease_duration":1800}`, `{"generation":"<V1>","request_id":"<V1>","pid":"<V2>","lease_duration":1800}`},
		{"prose pids", "pid 123; matching pids [4 5 123]", "pid <V1>; matching pids [<V2> <V3> <V1>]"},
		{"sha", fixed + " 2234567890123456789012345678901234567890", fixed + " <V1>"},
		{"names", "coder-1-20261024-003906.txt records-2026-10-24.jsonl state.yaml.liveness-abcdef123456", "coder-1-<STAMP>.txt records-<DATE>.jsonl state.yaml.liveness-<V1>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newA9Normaliser([]string{fixed}).text(tc.input)
			if got != tc.want {
				t.Fatalf("normaliser %s: got %q; want %q", tc.name, got, tc.want)
			}
		})
	}
	n := oracle.NewNormaliser()
	if err := n.KeepSHA(fixed); err != nil {
		t.Fatal(err)
	}
	input := newA9Normaliser([]string{fixed}).text("sha: " + fixed + "\nlease: 2020-01-01T00:00:00.123Z\nexpires: 2020-01-01T00:30:00.123Z")
	if got := string(n.StateForm(nil, []byte(input))); !strings.Contains(got, fixed) || !strings.Contains(got, "<T1+1800s:fZ>") {
		t.Fatal("A9 then harness must retain fixed SHA and derived lease")
	}
	for _, kind := range []oracle.Kind{oracle.JSON, oracle.Human} {
		in := []byte(input)
		if kind == oracle.JSON {
			in = []byte(`{"sha":"` + fixed + `","other":"2234567890123456789012345678901234567890"}`)
		}
		out, err := n.Form(kind, []byte(newA9Normaliser([]string{fixed}).text(string(in))))
		if err != nil || !strings.Contains(string(out), fixed) || strings.Contains(string(out), "2234567890123456789012345678901234567890") {
			t.Fatal("A9/harness kind lost SHA isolation")
		}
	}
}
