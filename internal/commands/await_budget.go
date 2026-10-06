package commands

import (
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
)

const maxAwaitInterval = 100 * time.Second

func agentAwaitInterval(projectRoot, agentID string) time.Duration {
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadCached()
	if err != nil {
		return maxAwaitInterval
	}
	return providers.AwaitInterval(state.Agents[agentID].Provider)
}

func awaitWithBudget[T any](
	remaining time.Duration,
	maxInterval time.Duration,
	await func(time.Duration) (T, error),
	isTimeout func(T) bool,
	markPoll func(T),
) (T, int, error) {
	remaining = max(remaining, 0)
	maxInterval = max(maxInterval, 0)
	interval := min(remaining, maxInterval)

	result, err := await(interval)
	remainingAfter := max(remaining-interval, 0)
	timeoutSeconds := int(remainingAfter / time.Second)
	if err == nil && remainingAfter > 0 && isTimeout(result) {
		markPoll(result)
	}

	return result, timeoutSeconds, err
}
