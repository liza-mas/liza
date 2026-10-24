//go:build !linux

package a9

import "testing"

// Decision D45 on hosts without a child subreaper: descendants cannot be
// attributed or reaped, so cleanup is best effort. The supervisor is cancelled and
// waited for by the scenario helpers; a provider the supervisor detached into its
// own process group is not tracked. Goldens come from Linux (AC4).

func orchSubreaper(testing.TB)    {}
func orchChildren(string) []int   { return nil }
func orchReap(testing.TB, string) {}
func orchAlive(int) bool          { return false }
