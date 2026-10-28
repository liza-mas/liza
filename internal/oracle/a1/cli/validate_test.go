package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleValidate(t *testing.T) {
	t.Parallel()
	for _, valid := range []bool{true, false} {
		name, code := "valid", 0
		if !valid {
			name, code = "invalid", 1
		}
		for _, jsonMode := range []bool{true, false} {
			mode, ext := "human", "txt"
			if jsonMode {
				mode, ext = "json", "json"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				p := newProject(t)
				if !valid {
					if err := os.Remove(filepath.Join(p.root, "README.md")); err != nil {
						t.Fatal(err)
					}
				}
				state := filepath.Join(p.root, ".liza", "state.yaml")
				before := readFile(t, state)
				args := []string{"validate", "--skip-process-checks"}
				if jsonMode {
					args = append(args, "--json")
				}
				got := p.run(t, false, args...)
				requireExit(t, got, code)
				if !valid && !strings.Contains(got.Stdout+got.Stderr, "spec_ref file not found: README.md (task: task-1)") {
					t.Fatal("invalid validation did not reach missing-spec refusal")
				}
				if !bytes.Equal(before, readFile(t, state)) {
					t.Fatal("non-repair validate mutated state")
				}
				p.check(t, mode+"/validate-"+name+"."+ext, got)
			})
		}
	}
}
