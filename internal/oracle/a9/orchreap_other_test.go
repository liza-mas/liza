//go:build !linux

package a9

import "testing"

// Hosts without a child subreaper cannot adopt orphans (decision D45), so
// cleanup is best-effort: the process group of every supervisor is terminated.
// A provider the supervisor detached into a session of its own is not tracked.

func orchSubreaper(testing.TB)    {}
func orchSnapshot(*orchOwner)     {}
func orchExists(int, string) bool { return false }
func orchZombie(int, string) bool { return false }
func orchKillPID(int)             {}

func orchReap(_ testing.TB, o *orchOwner) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, leader := range o.leaders {
		orchKillGroup(leader)
	}
}
