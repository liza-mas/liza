package ops

import (
	"fmt"
	"math"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

// AwaitPollIntervalConfigKey controls periodic checks, not foreground slices.
const AwaitPollIntervalConfigKey = "config.await_poll_interval"

func awaitPollInterval(seconds int) (time.Duration, error) {
	if seconds < 0 || int64(seconds) > math.MaxInt64/int64(time.Second) {
		return 0, &PreconditionError{Reason: fmt.Sprintf(
			"%s must be zero (unset) or positive seconds fitting a time.Duration; set it with %s %s %d --replace --reason \"restore valid interval\"",
			AwaitPollIntervalConfigKey, brand.Command("config", "set"), AwaitPollIntervalConfigKey, models.DefaultAwaitPollInterval),
		}
	}
	if seconds == 0 {
		seconds = models.DefaultAwaitPollInterval
	}
	return time.Duration(seconds) * time.Second, nil
}

// normalizeAwaitPolling resolves one entry snapshot for both await loops.
// Explicit positive overrides retain short test intervals independently.
func normalizeAwaitPolling(config models.Config, abort, fallback time.Duration) (time.Duration, time.Duration, error) {
	interval, err := awaitPollInterval(config.AwaitPollInterval)
	if err != nil {
		return 0, 0, err
	}
	return normalizedAwaitInterval(abort, interval), normalizedAwaitInterval(fallback, interval), nil
}

func normalizedAwaitInterval(interval, fallback time.Duration) time.Duration {
	if interval <= 0 {
		return fallback
	}
	return interval
}
