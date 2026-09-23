//go:build !windows

package pairingindex

import (
	"os"
	"syscall"
)

// sameFilesystem reports whether a rename from a into b stays atomic.
func sameFilesystem(a, b string) (bool, error) {
	infoA, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	statA, okA := infoA.Sys().(*syscall.Stat_t)
	statB, okB := infoB.Sys().(*syscall.Stat_t)
	if !okA || !okB {
		return true, nil
	}
	return statA.Dev == statB.Dev, nil
}
