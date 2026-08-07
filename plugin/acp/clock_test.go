package acp

import (
	"testing"
	"time"
)

// testClock advances by a fixed step on every reading, so a test can assert on
// an exact duration instead of on "more than zero" — which is not a property
// the platform guarantees for work that takes microseconds.
type testClock struct {
	current time.Time
	step    time.Duration
}

func (c *testClock) now() time.Time {
	c.current = c.current.Add(c.step)
	return c.current
}

// stubClock installs a deterministic clock for the duration of the test.
func stubClock(t *testing.T, step time.Duration) *testClock {
	t.Helper()

	clock := &testClock{current: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: step}
	original := nowFunc
	nowFunc = clock.now
	t.Cleanup(func() { nowFunc = original })
	return clock
}
