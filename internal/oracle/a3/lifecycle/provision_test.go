package lifecycle

import "testing"

func TestOracleProvision(t *testing.T) {
	linuxOnly(t)
	// TC9: default-brand runtime input, with synthetic bytes only. Each mode
	// gets its own envelope and empty HOME; no operator key is read by the test.
	files := []setupFile{{name: "tok.env", data: []byte("TOK=oracle-value\n")}}
	runCases(t, "provision",
		lifecycleCase{name: "success", fixture: "executing-runtime-input", args: []string{"provision", "--record", "--task", "task-1", "--input", "tok", "--file", "{{TMPDIR}}/tok.env"}, setupFiles: files, mutating: true},
		lifecycleCase{name: "refusal-undeclared-input", fixture: "executing-runtime-input", args: []string{"provision", "--record", "--task", "task-1", "--input", "nope", "--file", "{{TMPDIR}}/tok.env"}, setupFiles: files, mutating: true})
}
