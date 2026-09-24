package models

import "time"

const (
	// LeaseExpiryGracePeriod is the grace window after lease expiry before alerting/warning.
	LeaseExpiryGracePeriod = 120 * time.Second

	// VerdictHandoffGrace bounds how long after a verdict the doer may still
	// hold WAITING on the task: its await-verdict call observes the verdict and
	// releases current_task in a separate write.
	VerdictHandoffGrace = 2 * time.Minute
)

// InVerdictHandoff reports whether the task's current boundary was set by a
// verdict recorded within VerdictHandoffGrace before now: its last history
// entry is an approval or rejection timestamped in [now-G, now]. A missing,
// zero, older or future timestamp, or any later history entry, grants nothing.
func InVerdictHandoff(task *Task, now time.Time) bool {
	if len(task.History) == 0 {
		return false
	}
	last := task.History[len(task.History)-1]
	if last.Event != TaskEventApproved && last.Event != TaskEventRejected {
		return false
	}
	if last.Time.IsZero() || last.Time.After(now) {
		return false
	}
	return now.Sub(last.Time) <= VerdictHandoffGrace
}

// LatestHistoryTime returns the time of the task's most recent history entry
// with the given event, or the zero time when there is none.
func LatestHistoryTime(task *Task, event TaskEventName) time.Time {
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == event {
			return task.History[i].Time
		}
	}
	return time.Time{}
}
