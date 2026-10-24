package a6

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type observation struct {
	stdout, stderr []byte
	exit           int
}

func runProcess(t *testing.T, f *fixture, bin string, args []string, extra []string) observation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = f.root
	cmd.Env = f.envFor(t, extra...)
	var out, errout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	o := observation{stdout: out.Bytes(), stderr: errout.Bytes()}
	if ctx.Err() != nil {
		t.Fatalf("A6 bounded child timed out: %s", bin)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		o.exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("A6 child could not start: %v", err)
	}
	return o
}

func captureCLI(t *testing.T, f *fixture, extra []string, args ...string) observation {
	t.Helper()
	return runProcess(t, f, oracle.BuildCLI(t, "./cmd/liza"), args, extra)
}

type caseSpec struct {
	setup   func(*testing.T) *fixture
	args    []string
	kinds   []oracle.Kind
	exit    int
	extra   []string
	noDelta bool
	check   func(*testing.T, *fixture, observation)
}

// Each output mode receives a fresh project; the normalized state pair is
// compared across modes as well as against its single committed state golden.
func runCase(t *testing.T, c caseSpec) {
	t.Helper()
	platformGate(t)
	stem := strings.TrimPrefix(t.Name(), "TestOracle")
	has := func(kind oracle.Kind) bool {
		for _, k := range c.kinds {
			if k == kind {
				return true
			}
		}
		return false
	}
	var jsonState []byte
	for _, kind := range []oracle.Kind{oracle.JSON, oracle.Human} {
		if !has(kind) {
			continue
		}
		f := c.setup(t)
		before := f.state(t)
		args := append([]string{}, c.args...)
		if kind == oracle.JSON {
			args = append(args, "--json")
		}
		o := captureCLI(t, f, c.extra, args...)
		if f.release != nil {
			f.release()
		}
		if o.exit != c.exit {
			t.Fatalf("%s: exit %d want %d\nstdout: %s\nstderr: %s", stem, o.exit, c.exit, o.stdout, o.stderr)
		}
		if c.check != nil {
			c.check(t, f, o)
		}
		after := f.state(t)
		if c.noDelta && !bytes.Equal(before, after) {
			t.Fatalf("%s: precondition refusal changed state", stem)
		}
		if kind == oracle.JSON {
			document, err := oneJSONDocument(o.stdout)
			if err != nil {
				t.Fatalf("%s: stdout is not a JSON document: %v\n%s", stem, err, o.stdout)
			}
			wrapped, err := json.Marshal(map[string]any{"exit_code": o.exit, "stdout": document})
			if err != nil {
				t.Fatal(err)
			}
			f.norm.Check(t, kind, stem+".json", []byte(f.preprocess(string(wrapped))))
		} else {
			transcript := fmt.Sprintf("exit_code: %d\nstdout:\n%s\nstderr:\n%s", o.exit, withoutLogs(string(o.stdout)), withoutLogs(string(o.stderr)))
			f.norm.Check(t, kind, stem+".txt", []byte(f.preprocess(transcript)))
		}
		if has(oracle.State) {
			b, a := []byte(f.preprocess(string(before))), []byte(f.preprocess(string(after)))
			form := f.norm.StateForm(b, a)
			if kind == oracle.JSON {
				f.norm.CheckState(t, stem+".yaml", b, a)
				jsonState = form
			} else if !bytes.Equal(jsonState, form) {
				t.Fatalf("%s: fresh human-mode state differs from json-mode state", stem)
			}
		}
	}
}

func oneJSONDocument(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document, trailing any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("expected end of JSON document, got %v", err)
	}
	return document, nil
}

func TestA6JSONCaptureRejectsTrailingContent(t *testing.T) {
	for _, data := range []string{`{"ok":true} {"extra":true}`, `{"ok":true} garbage`} {
		if _, err := oneJSONDocument([]byte(data)); err == nil {
			t.Fatal("JSON capture accepted trailing content")
		}
	}
	document, err := oneJSONDocument([]byte("{\"number\":9007199254740993}\n"))
	if err != nil || document.(map[string]any)["number"] != json.Number("9007199254740993") {
		t.Fatal("single JSON document lost numeric precision")
	}
}
