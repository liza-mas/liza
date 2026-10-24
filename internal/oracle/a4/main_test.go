package a4

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

var helperOps = map[string]func(root string) (string, error){}

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "a4-helper" {
		skip, fail := platformGate(runtime.GOOS, time.LoadLocation)
		if skip != "" || fail != "" {
			fmt.Fprintln(os.Stderr, skip+fail)
			os.Exit(2)
		}
		if len(os.Args) != 4 || helperOps[os.Args[2]] == nil {
			fmt.Fprintln(os.Stderr, "unknown A4 helper")
			os.Exit(2)
		}
		summary, err := helperOps[os.Args[2]](os.Args[3])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(summary)
		os.Exit(0)
	}
	oracle.RunMain(m)
}

func platformGate(goos string, load func(string) (*time.Location, error)) (skip, fail string) {
	if goos == "windows" {
		return "D3 platform gate: Windows does not honour TZ", ""
	}
	if _, err := load("Etc/GMT-2"); err != nil {
		return "", "D3 platform gate: cannot load Etc/GMT-2"
	}
	return "", ""
}

func gate(t testing.TB) {
	t.Helper()
	skip, fail := platformGate(runtime.GOOS, time.LoadLocation)
	if skip != "" {
		t.Skip(skip)
	}
	if fail != "" {
		t.Fatal(fail)
	}
}
