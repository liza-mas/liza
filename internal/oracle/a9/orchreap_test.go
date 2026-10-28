package a9

import "testing"

// Decisions D45/D48: cancelling a supervisor leaves a provider it started in a detached
// process group running after the project is removed. These tests therefore end
// by signalling, reaping and asserting the absence of every child of the test
// process. That assertion is only sound when no other scenario owns a child, so
// every orchestrator test is serial: Go runs a top-level test that does not call
// t.Parallel while all parallel top-level tests are still paused, and its
// subtests are serial too (the same reason as startNamed and AuthorityLost).
// Do not add t.Parallel to a test that calls orchGuard.
//
// Linux makes this test process the reaper of orphans through a child subreaper
// (orchreap_linux_test.go), so a descendant is found whatever its leader did and
// whenever it exited. Other hosts have no subreaper: each supervisor's process
// group is terminated best-effort (orchStart) and a provider detached into a
// session of its own is not tracked. The helper is task-owned and small by D45;
// consolidating it into the kit is recorded debt (payback: the first A9-area
// refactor task).

// orchGuard makes this test process the reaper of the test's orphaned descendants
// before its first supervisor starts, and registers their reaping for the end of
// the test. It registers first so its cleanup runs after the supervisor's own.
func orchGuard(t *testing.T) {
	t.Helper()
	orchSubreaper(t)
	t.Cleanup(func() { orchReap(t) })
}
