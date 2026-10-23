package oracle

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func TestMain(m *testing.M) { RunMain(m) }

// recorder is a testing.TB whose failures are recorded instead of reported.
type recorder struct {
	testing.TB
	msg    string
	failed bool
}

func (r *recorder) Helper() {}

func (r *recorder) Fatal(args ...any) {
	r.failed = true
	r.msg = fmt.Sprint(args...)
	runtime.Goexit()
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.failed = true
	r.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// expectFailure runs f against a recorder and returns the failure message; it
// fails t when f did not fail.
func expectFailure(t *testing.T, f func(tb testing.TB)) string {
	t.Helper()
	r := &recorder{TB: t}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		f(r)
	}()
	wg.Wait()
	if !r.failed {
		t.Fatal("expected the call to fail, it did not")
	}
	return r.msg
}
