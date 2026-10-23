//go:build linux

package artifact

import (
	"os"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	syscall.Umask(0o022)
	os.Exit(m.Run())
}
