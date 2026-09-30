//go:build unix

package runtimeinputs

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// A FIFO in place of an envelope is refused without the blocking open that
// would hang until a writer appears.
func TestMaterializeRefusesAFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w03.env")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	key := &Key{ID: "k", secret: []byte("0123456789abcdef0123456789abcdef")}
	declaration := models.RuntimeInput{ID: "w03", Env: []string{"W03_FIXTURE"}}
	done := make(chan error, 1)
	go func() {
		_, err := Materialize(key, declaration, path, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Materialize blocked opening a FIFO")
	}
}
