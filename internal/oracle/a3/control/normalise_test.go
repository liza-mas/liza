package control

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func linuxOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("A3 oracle requires Linux process liveness and POSIX fixture permissions (TC1)")
	}
}

func TestDeterminismLayer(t *testing.T) {
	linuxOnly(t)
	start := time.Date(2026, 10, 23, 19, 0, 0, 900000000, time.UTC)
	cases := []struct {
		name, input, want string
		pid               int
		badHex            bool
	}{
		{name: "log", input: "before\n2026/10/23 21:00:00.123 audit value\nafter\n", want: "before\nafter\n"},
		{name: "paths", input: "/case/project/a /case/home /case/tmp/x /case/bin", want: "<PROJECT>/a <HOME> <TMPDIR>/x <CASE>/bin"},
		{name: "fixture", input: strings.Repeat("a", 40), want: "<commit:fixture>"},
		{name: "utc", input: "2026-10-23T19:00:00Z", want: "<now+0m>Z"},
		{name: "local", input: "2026-10-23T21:00:00+02:00", want: "<now+0m>+02:00"},
		{name: "compact-zone", input: "2026-10-23T14:00:00-0500", want: "<now+0m>-0500"},
		{name: "fraction-quoted", input: `"2026-10-23 19:00:00.123Z"`, want: `"<now+0m>Z"`},
		{name: "lease-second-precision", input: "2026-10-23T19:30:00Z", want: "<now+30m>Z"},
		{name: "negative-minute", input: "2026-10-23T18:58:59Z", want: "<now-1m>Z"},
		{name: "negative-subminute", input: "2026-10-23T18:59:59Z", want: "<now+0m>Z"},
		{name: "past-boundary", input: "2026-10-16T19:00:00Z", want: "<now-10080m>Z"},
		{name: "future-boundary", input: "2026-10-30T19:00:00Z", want: "<now+10080m>Z"},
		{name: "past-outside", input: "2026-10-16T18:59:59Z", want: "<at 2026.10.16 18.59.59>Z"},
		{name: "future-outside", input: "2026-10-30T19:00:01Z", want: "<at 2026.10.30 19.00.01>Z"},
		{name: "past", input: "2020-01-01T00:30:00Z", want: "<at 2020.01.01 00.30.00>Z"},
		{name: "future", input: "2099-01-01T00:00:00.123+02:00", want: "<at 2099.01.01 00.00.00.123>+02:00"},
		{name: "zero", input: "0001-01-01T00:00:00Z", want: "<at 0001.01.01 00.00.00>Z"},
		{name: "undeclared-40-hex", input: strings.Repeat("b", 40), badHex: true},
		{name: "undeclared-64-hex", input: strings.Repeat("c", 64), badHex: true},
		{name: "pid-contexts", pid: 1234, input: "    pid: 1234\n        details:\n            pid: 1234\n(pid 1234) pid=1234 pid: 1234 {\"pid\": 1234}\npid: 12345", want: "    pid: <live-pid>\n        details:\n            pid: <live-pid>\n(pid <live-pid>) pid=<live-pid> pid: <live-pid> {\"pid\": <live-pid>}\npid: 12345"},
	}
	for _, pid := range []int{2020, 30, 1800, 5} {
		input := fmt.Sprintf("pid: %d\nlease_expires: 2020-01-01T00:30:00Z\nregistered_at: 2020-01-01T00:00:00Z\nlease_duration: 1800\nmax_instances: 5\n{\"max_concurrent\": 5}\nmanifest content: %d", pid, pid)
		want := fmt.Sprintf("pid: <live-pid>\nlease_expires: <at 2020.01.01 00.30.00>Z\nregistered_at: <at 2020.01.01 00.00.00>Z\nlease_duration: 1800\nmax_instances: 5\n{\"max_concurrent\": 5}\nmanifest content: %d", pid)
		cases = append(cases, struct {
			name, input, want string
			pid               int
			badHex            bool
		}{name: fmt.Sprintf("pid-collision-%d", pid), input: input, want: want, pid: pid})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := determinism{start: start, pid: tc.pid, fixtureSHA: strings.Repeat("a", 40), paths: [][2]string{{"/case/project", "<PROJECT>"}, {"/case/home", "<HOME>"}, {"/case/tmp", "<TMPDIR>"}, {"/case", "<CASE>"}}}
			got, err := n.text(tc.input)
			if tc.badHex {
				if err == nil {
					t.Fatal("undeclared hex accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("layer: got %q, want %q", got, tc.want)
			}
			if got = oracle.NewNormaliser().Text(got); got != tc.want {
				t.Fatalf("harness erased asserted token: got %q, want %q", got, tc.want)
			}
		})
	}
}
