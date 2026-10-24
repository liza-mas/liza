//go:build !linux

package a4

import "testing"

// D53: the D35 success scenario freezes validation that depends on a procfs
// process scan; dependencies_d35_linux_test.go holds the scenario and states the
// reason. The refused case in dependencies_test.go runs on every platform.
func runD35Success(t *testing.T) {
	t.Skip("D53: D35 success freezes the procfs-present validation outcome (process scan); Linux only")
}
