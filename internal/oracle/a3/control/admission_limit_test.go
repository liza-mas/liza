package control

import "testing"

func TestOracleAdmissionLimit(t *testing.T) {
	// runCases asserts mode parity on fresh copies, including refusals/no-ops.
	runCases(t, "admission-limit", []controlCase{
		{name: "set-new", fixture: "base", args: []string{"admission-limit", "set", "db-suite", "--max-concurrent", "1", "--match=--oracle-heavy"}, json: true, state: true},
		{name: "replace", fixture: "admission-limit", args: []string{"admission-limit", "set", "db-suite", "--max-concurrent", "2", "--match=--oracle-heavy", "--replace", "--reason", "wider"}, json: true, state: true},
		{name: "remove", fixture: "admission-limit", args: []string{"admission-limit", "remove", "db-suite", "--reason", "done"}, json: true, state: true},
		{name: "remove-unknown", fixture: "base", args: []string{"admission-limit", "remove", "nope", "--reason", "done"}, json: true, state: true},
		{name: "refused-different", fixture: "admission-limit", args: []string{"admission-limit", "set", "db-suite", "--max-concurrent", "2", "--match=--oracle-heavy"}, code: 1, json: true, state: true},
	})
}
