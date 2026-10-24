//go:build !linux

package a9

import "time"

// launchAwaitsExit reports that launchAwaitExit cannot wait for the exit of a
// scenario process on this platform.
const launchAwaitsExit = false

// launchSubreaper does nothing: only Linux lets a process adopt its orphaned
// descendants (D45), so on other platforms a scenario process whose parent exits
// leaves the test's reach and its exit cannot be waited for.
func launchSubreaper() error { return nil }

// launchAwaitExit waits for nothing. D45 accepts best-effort termination here:
// launchReap kills every live member of the scenario (the processes of its
// groups included) at once, with no wait loop, and does not assert afterwards
// that they are gone, because their exit cannot be observed without polling.
func launchAwaitExit([]int, time.Duration) int { return 0 }
