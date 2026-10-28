package control

import "testing"

func TestOracleAdmit(t *testing.T) {
	runCases(t, "admit", []controlCase{
		{name: "no-limit-exit-0", fixture: "base", args: []string{"admit", "--", "oracle-stub", "0"}, stub: true, unchanged: true},
		{name: "no-limit-exit-3", fixture: "base", args: []string{"admit", "--", "oracle-stub", "3"}, code: 3, stub: true, unchanged: true},
		{name: "limit-free-slot", fixture: "stub-limit", args: []string{"admit", "--", "oracle-stub", "0"}, stub: true, unchanged: true},
		{name: "refused-max-wait", fixture: "base", args: []string{"admit", "--max-wait", "99999", "--", "oracle-stub", "0"}, code: 1, stub: true, unchanged: true},
	})
}
