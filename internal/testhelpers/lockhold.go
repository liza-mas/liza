package testhelpers

import (
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
)

// HoldFileLock acquires the exclusive lock protecting path and holds it until
// release is called or the test ends. It returns only once the lock is held,
// so a caller cannot accidentally exercise an uncontended path. Cleanup
// releases the lock and fails the test if the holder does not exit within 5s.
func HoldFileLock(t *testing.T, path string) (release func()) {
	t.Helper()
	acquired := make(chan struct{})
	releaseCh := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- filelock.New(path).WithTimeout(5*time.Second).WithLockOperation("test-hold", func() error {
			close(acquired)
			<-releaseCh
			return nil
		})
	}()
	select {
	case <-acquired:
	case err := <-done:
		t.Fatalf("hold lock for %s: %v", path, err)
	}
	var once sync.Once
	release = func() { once.Do(func() { close(releaseCh) }) }
	t.Cleanup(func() {
		release()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("held lock for %s: %v", path, err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("lock holder for %s did not exit", path)
		}
	})
	return release
}
