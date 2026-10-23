package a6

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

var (
	instantRE  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z| ?[+-]\d{2}:?\d{2})`)
	shaRE      = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
	durationRE = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ns|µs|ms|s|m)(?:\d+(?:\.\d+)?s)?\b`)
	pidRE      = regexp.MustCompile(`\b(pid[=: ]+|PID[=: ]+)\d+\b`)
	logRE      = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)
)

func platformGate(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("A6 Unix platform gate: Windows does not honor TZ and runner locks differ")
	}
	zone, err := time.LoadLocation("Etc/GMT-2")
	if err != nil {
		t.Fatalf("A6 platform gate: load Etc/GMT-2: %v", err)
	}
	_, offset := time.Unix(0, 0).In(zone).Zone()
	if offset != 2*60*60 {
		t.Fatal("A6 platform gate: Etc/GMT-2 must have +02:00 offset")
	}
}

type identities struct {
	values map[string]string
	next   int
}

func (ids *identities) register(sha, name string) {
	if !shaRE.MatchString(sha) || len(sha) != 40 {
		return
	}
	if ids.values == nil {
		ids.values = make(map[string]string)
	}
	if _, exists := ids.values[sha]; !exists {
		ids.values[sha] = "<SHA:" + name + ">"
	}
}

func (ids *identities) text(s string) string {
	s = instantRE.ReplaceAllStringFunc(s, func(instant string) string {
		if strings.HasSuffix(instant, "Z") {
			return "<TIME:Z>"
		}
		i := strings.LastIndexAny(instant, "+-")
		return "<TIME:" + instant[i:] + ">"
	})
	s = shaRE.ReplaceAllStringFunc(s, func(sha string) string {
		if ids.values == nil {
			ids.values = make(map[string]string)
		}
		if token, ok := ids.values[sha]; ok {
			return token
		}
		ids.next++
		token := fmt.Sprintf("<SHA:observed-%d>", ids.next)
		ids.values[sha] = token
		return token
	})
	s = durationRE.ReplaceAllString(s, "<DURATION>")
	return pidRE.ReplaceAllString(s, "${1}<PID>")
}

// Only stdlib log records are free; arbitrary stderr remains observable.
func withoutLogs(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if !logRE.MatchString(line) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func TestA6DeterminismPreservesZonesAndIdentity(t *testing.T) {
	platformGate(t)
	ids := identities{}
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	got := oracle.NewNormaliser().Text(ids.text("2001-02-03T04:05:06Z 2001-02-03T06:05:06+02:00 " + a + " " + b + " " + a))
	want := "<TIME:Z> <TIME:+02:00> <SHA:observed-1> <SHA:observed-2> <SHA:observed-1>"
	if got != want {
		t.Fatalf("zone or SHA identity lost: got %q want %q", got, want)
	}
	if again := ids.text(got); again != got {
		t.Fatal("A6 normalization is not idempotent")
	}
	if got := withoutLogs("2001/02/03 04:05:06 free log\nError: retained\n"); got != "Error: retained\n" {
		t.Fatal("non-log error content discarded")
	}
}

func TestA6AdapterImportBoundary(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if !strings.HasSuffix(name, "_test.go") {
			t.Errorf("non-test Go file %s", name)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Split(path, "/")[0], ".") || path == "github.com/liza-mas/liza/internal/testhelpers/oracle" {
				continue
			}
			if !strings.HasPrefix(name, "adapter_") {
				t.Errorf("module dependency %s outside adapter: %s", path, name)
			}
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytesToSleep := []byte("time." + "Sleep("); strings.Contains(string(data), string(bytesToSleep)) {
			t.Errorf("sleep in oracle: %s", name)
		}
	}
}
