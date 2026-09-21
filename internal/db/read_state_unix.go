//go:build !windows

package db

import "os"

func readStateFile(statePath string) ([]byte, error) {
	return os.ReadFile(statePath)
}
