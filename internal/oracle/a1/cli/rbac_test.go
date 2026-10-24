package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestOraclePipelineRBAC(t *testing.T) {
	t.Parallel()
	var shared observation
	for _, jsonMode := range []bool{true, false} {
		p := newProject(t)
		state := filepath.Join(p.root, ".liza", "state.yaml")
		before := readFile(t, state)
		args := []string{"submit-verdict", "task-1", "APPROVED", "--agent-id", "coder-1"}
		mode, ext := "human", "txt"
		if jsonMode {
			mode, ext = "json", "json"
			args = append(args, "--json")
		}
		got := p.run(t, true, args...)
		requireExit(t, got, 1)
		if !strings.Contains(got.Stdout+got.Stderr, `not allowed for role "coder"`) || !strings.Contains(got.Stdout+got.Stderr, "FORBIDDEN") {
			// JSON escapes quotes; decode its stdout rather than weakening the refusal check.
			if !jsonMode {
				t.Fatal("refusal did not reach role capability check")
			}
			decoded, err := decodeJSON(got.Stdout)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := canonicalJSON(decoded)
			if !strings.Contains(string(b), `not allowed for role \"coder\"`) || !strings.Contains(string(b), "FORBIDDEN") {
				t.Fatal("refusal did not reach role capability check")
			}
		}
		after := readFile(t, state)
		if !bytes.Equal(before, after) {
			t.Fatal("RBAC refusal changed state")
		}
		pair := observation{got.ExitCode, string(before), string(after)}
		if jsonMode {
			shared = pair
		} else if pair != shared {
			t.Fatal("JSON and human RBAC state pairs differ")
		}
		p.check(t, mode+"/pipeline-rbac."+ext, got)
	}
	(project{}).check(t, "state/pipeline-rbac.txt", shared)
}
