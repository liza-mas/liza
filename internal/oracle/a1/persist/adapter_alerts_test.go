package persist

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
)

func appendAlert(path string, input alertInput) error {
	at, err := fixedTime(input.At)
	if err != nil {
		return err
	}
	level := alerts.AlertLevelWarning
	if input.Level == "critical" {
		level = alerts.AlertLevelCritical
	}
	alert := alerts.Alert{Timestamp: at, Level: level, Category: input.Category, Message: input.Message}
	if input.KeyKind != "" {
		episode, err := fixedTime(input.Episode)
		if err != nil {
			return err
		}
		switch input.KeyKind {
		case "blocked":
			alert.OnceKey = alerts.BlockedEpisodeKey(input.Task, episode, input.Message)
		case "human":
			alert.OnceKey = alerts.AwaitingHumanOccurrenceKey(input.Task, input.Index, episode, input.Ask)
		default:
			return fmt.Errorf("unknown fixture key kind %q", input.KeyKind)
		}
	}
	return alerts.Write(path, alert)
}

func parseAlert(line string) alertRecord {
	a, ok := alerts.ParseLine(line)
	if !ok {
		return alertRecord{Accepted: false}
	}
	level := string(a.Level)
	if a.Level == alerts.AlertLevelWarning {
		level = "warning"
	}
	if a.Level == alerts.AlertLevelCritical {
		level = "critical"
	}
	return alertRecord{true, a.Timestamp.Format(time.RFC3339Nano), level, a.Category, a.Message}
}
