package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleValidatePayload(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"list", "accepted", "rejected"} {
		for _, jsonMode := range []bool{true, false} {
			mode, ext := "human", "txt"
			if jsonMode {
				mode, ext = "json", "json"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				p := newProject(t, "payload")
				args := []string{"validate-payload"}
				if name == "list" {
					args = append(args, "--list")
				} else {
					args = append(args, "set-task-output", "--payload", filepath.Join(p.root, name+".json"))
				}
				if jsonMode {
					args = append(args, "--json")
				}
				got := p.run(t, false, args...)
				code := 0
				if name == "rejected" {
					code = 1
				}
				requireExit(t, got, code)
				if name == "list" {
					if !strings.Contains(got.Stdout, "set-task-output") {
						t.Fatal("schema list omitted set-task-output")
					}
				} else {
					outcome := "COMPLETED"
					if name == "rejected" {
						outcome = "INVALID_INPUT"
					}
					if !strings.Contains(got.Stdout+got.Stderr, outcome) {
						t.Fatalf("schema did not report %s", outcome)
					}
				}
				p.check(t, mode+"/payload-"+name+"."+ext, got)
			})
		}
	}
}
