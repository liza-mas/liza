package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
)

// ActivityKind names the producer of an activity-feed entry.
type ActivityKind string

const (
	ActivityKindLog     ActivityKind = "log"     // log.yaml entry
	ActivityKindAlert   ActivityKind = "alert"   // watch alert or TUI-side failure notice
	ActivityKindAnomaly ActivityKind = "anomaly" // blackboard anomaly
)

// Actions of the failure notices the TUI raises itself, as opposed to watch
// alert categories (alerts.Category).
const (
	activityActionStateReadError = "state_read_error"
	activityActionWriteError     = "write_error"
	activityActionWatcherError   = "watcher_error"
)

// Alert levels as ActivityEntry.Level and AlertMsg.Level carry them.
const (
	levelWarning  = string(alerts.AlertLevelWarning)
	levelCritical = string(alerts.AlertLevelCritical)
)

// maxActivityEntries caps the activity feed; older entries drop first.
const maxActivityEntries = 200

// appendActivity appends an entry to the activity slice, capping it at maxActivityEntries.
func appendActivity(activities []ActivityEntry, entry ActivityEntry) []ActivityEntry {
	activities = append(activities, entry)
	if len(activities) > maxActivityEntries {
		activities = activities[len(activities)-maxActivityEntries:]
	}
	return activities
}

// warningActivity is a failure notice the TUI raises itself.
func warningActivity(action, detail string) ActivityEntry {
	return ActivityEntry{
		Timestamp: time.Now(),
		Source:    ActivityKindAlert,
		Action:    action,
		Level:     levelWarning,
		Detail:    detail,
	}
}

// alertActivity is the feed entry of a watch alert.
func alertActivity(a AlertMsg) ActivityEntry {
	return ActivityEntry{
		Timestamp: a.Timestamp,
		Source:    ActivityKindAlert,
		Action:    a.Category,
		Detail:    a.Message,
		Level:     a.Level,
		AlertKey:  a.Key,
	}
}

// anomalyActivity is the feed entry of a blackboard anomaly.
func anomalyActivity(a models.Anomaly) ActivityEntry {
	return ActivityEntry{
		Timestamp: a.Timestamp,
		Source:    ActivityKindAnomaly,
		Agent:     a.Reporter,
		Action:    string(a.Type),
		Task:      a.Task,
		Detail:    formatAnomalyDetails(a.Details),
		Level:     levelWarning,
	}
}

// logActivity is the feed entry of a log.yaml entry.
func logActivity(e log.Entry) ActivityEntry {
	task := ""
	if e.Task != nil {
		task = *e.Task
	}
	return ActivityEntry{
		Timestamp: e.Timestamp,
		Source:    ActivityKindLog,
		Agent:     e.Agent,
		Action:    e.Action,
		Task:      task,
		Detail:    e.Detail,
	}
}

func resolveInactiveAlerts(activities []ActivityEntry, activeKeys map[string]bool) []ActivityEntry {
	if activeKeys == nil {
		return activities
	}
	for i := range activities {
		if activities[i].Source != ActivityKindAlert || activities[i].AlertKey == "" {
			continue
		}
		activities[i].Resolved = !activeKeys[activities[i].AlertKey]
	}
	return activities
}

func dropResolvedTransientAlerts(activities []ActivityEntry) []ActivityEntry {
	kept := activities[:0]
	for _, activity := range activities {
		if activity.Source == ActivityKindAlert && activity.Resolved && isTransientAlertCategory(activity.Action) {
			continue
		}
		kept = append(kept, activity)
	}
	return kept
}

func isTransientAlertCategory(category string) bool {
	switch alerts.Category(category) {
	case alerts.CategoryLeaseExpired, alerts.CategoryReviewLeaseExpired:
		return true
	default:
		return false
	}
}

func shouldClearResolvedBanner(alertBanner *ActivityEntry, activeKeys map[string]bool) bool {
	if alertBanner == nil || alertBanner.AlertKey == "" || activeKeys == nil {
		return false
	}
	return !activeKeys[alertBanner.AlertKey]
}

// formatAnomalyDetails converts an anomaly's Details map to a compact display string.
// Keys are sorted alphabetically, formatted as key=value pairs separated by spaces.
func formatAnomalyDetails(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}

	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%v", k, details[k])
	}
	return strings.Join(parts, " ")
}
