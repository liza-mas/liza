package agent

import (
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// A quota block lifts at the provider's announced reset plus quotaResetGrace,
// but never sooner than quotaMinBlock after detection, so a provider repeating
// an already-past reset cannot drive a tight respawn loop. Without an announced
// reset the block lifts after quotaFallbackBlock. The first provider turn after
// a lift is the probe: if the provider is still exhausted it re-raises the
// signal with the newly announced reset.
const (
	quotaResetGrace    = time.Minute
	quotaMinBlock      = 5 * time.Minute
	quotaFallbackBlock = 30 * time.Minute
	// A date-less clock time further than this in the past means the next day.
	quotaClockPastTolerance = time.Hour
)

var (
	// Claude: "You've hit your session limit · resets 4am (Europe/Paris)".
	claudeResetText = regexp.MustCompile(`resets (\d{1,2}(?::\d{2})?)\s*(am|pm) \(([^)]+)\)`)
	// Codex prints its reset in host-local time, without the date when same-day:
	// "try again at Sep 29th, 2026 2:57 AM" or "try again at 2:57 AM".
	codexResetText = regexp.MustCompile(`try again at (?:([A-Z][a-z]{2}) (\d{1,2})(?:st|nd|rd|th), (\d{4}) )?(\d{1,2}:\d{2}) (AM|PM)`)
)

// quotaResetTime returns the reset the provider announced alongside a quota
// failure, or the zero time when none can be read.
func quotaResetTime(output, message, provider string, now time.Time) time.Time {
	switch provider {
	case "claude":
		if resetsAt := claudeRejectedResetAt(output); !resetsAt.IsZero() {
			return resetsAt
		}
		m := claudeResetText.FindStringSubmatch(message)
		if m == nil {
			return time.Time{}
		}
		loc, err := time.LoadLocation(m[3])
		if err != nil {
			return time.Time{}
		}
		return nextClockTime(m[1]+m[2], "3pm", "3:04pm", loc, now)
	case "codex":
		m := codexResetText.FindStringSubmatch(message)
		if m == nil {
			return time.Time{}
		}
		if m[1] == "" {
			return nextClockTime(m[4]+" "+m[5], "3:04 PM", "", time.Local, now)
		}
		resetsAt, err := time.ParseInLocation("Jan 2 2006 3:04 PM", m[1]+" "+m[2]+" "+m[3]+" "+m[4]+" "+m[5], time.Local)
		if err != nil {
			return time.Time{}
		}
		return resetsAt.UTC()
	}
	return time.Time{}
}

// claudeRejectedResetAt reads the exact reset from the last rejected
// rate_limit_event in Claude's stream-json output.
func claudeRejectedResetAt(output string) time.Time {
	var resetsAt time.Time
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Info struct {
				Status   string `json:"status"`
				ResetsAt int64  `json:"resetsAt"`
			} `json:"rate_limit_info"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Type != "rate_limit_event" {
			continue
		}
		if event.Info.Status == "rejected" && event.Info.ResetsAt > 0 {
			resetsAt = time.Unix(event.Info.ResetsAt, 0).UTC()
		}
	}
	return resetsAt
}

// nextClockTime resolves a date-less clock time in loc to its next occurrence,
// tolerating a slightly past time (clock skew at the reset second).
func nextClockTime(clock, layout, altLayout string, loc *time.Location, now time.Time) time.Time {
	parsed, err := time.Parse(layout, clock)
	if err != nil && altLayout != "" {
		parsed, err = time.Parse(altLayout, clock)
	}
	if err != nil {
		return time.Time{}
	}
	local := now.In(loc)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)
	if candidate.Before(now.Add(-quotaClockPastTolerance)) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC()
}

// quotaSignalExpiry is when a signal detected at detected stops blocking.
func quotaSignalExpiry(resetsAt, detected time.Time) time.Time {
	if resetsAt.IsZero() {
		return detected.Add(quotaFallbackBlock)
	}
	if floor := detected.Add(quotaMinBlock); resetsAt.Add(quotaResetGrace).Before(floor) {
		return floor
	}
	return resetsAt.Add(quotaResetGrace)
}

// readQuotaSignalExpiry returns when the signal file at path stops blocking.
// Older binaries wrote no expires field; a partially written file may have
// none either. Both fall back to detected, then to the file's mtime, plus the
// bounded fallback. ok is false when the file does not exist; any other read
// failure reports a block that has not expired (fail closed).
func readQuotaSignalExpiry(path string, now time.Time) (expires time.Time, ok bool) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return time.Time{}, false
	}
	if err != nil {
		return now.Add(quotaFallbackBlock), true
	}
	defer f.Close()
	data, readErr := io.ReadAll(f)
	info, statErr := f.Stat()
	if readErr != nil || statErr != nil {
		return now.Add(quotaFallbackBlock), true
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, found := strings.Cut(line, ": "); found {
			if _, seen := fields[key]; !seen {
				fields[key] = strings.TrimSpace(value)
			}
		}
	}
	if expires, err := time.Parse(time.RFC3339, fields["expires"]); err == nil {
		return expires, true
	}
	if detected, err := time.Parse(time.RFC3339, fields["detected"]); err == nil {
		return quotaSignalExpiry(time.Time{}, detected), true
	}
	return quotaSignalExpiry(time.Time{}, info.ModTime()), true
}
