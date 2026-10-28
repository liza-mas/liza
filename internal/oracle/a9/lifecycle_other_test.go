//go:build !linux

package a9

import "testing"

// D53: the I-02 capacity refusal asserts procfs liveness wording that only Linux
// produces; lifecycle_linux_test.go holds the scenario and states the reason. The
// I-01 collision and I-05 authority-lost tests exist only in that file.
func oracleA9CapacityRefused(t *testing.T) {
	t.Skip("D53: I-02 capacity refusal freezes Linux procfs liveness wording")
}
