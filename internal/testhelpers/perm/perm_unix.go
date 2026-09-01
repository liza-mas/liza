//go:build !windows

package perm

import (
	"fmt"
	"os"
)

// denyWrites drops the write bits from the directory's mode.
func denyWrites(dir string) (func() error, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", dir, err)
	}
	previous := info.Mode().Perm()
	if err := os.Chmod(dir, previous&^0o222); err != nil {
		return nil, fmt.Errorf("deny writes on %s: %w", dir, err)
	}
	return func() error {
		if err := os.Chmod(dir, previous); err != nil {
			return fmt.Errorf("restore writes on %s: %w", dir, err)
		}
		return nil
	}, nil
}
