package providers

import "time"

const (
	// claudeAwaitInterval leaves headroom below Bash's explicit 600-second timeout.
	claudeAwaitInterval = 540 * time.Second
	// defaultAwaitInterval is the conservative cap for tools without a verified host limit.
	defaultAwaitInterval = 100 * time.Second
)

// AwaitInterval uses tool identity: sharing a backend or contract does not
// imply a shared foreground limit. Unknown tools keep the conservative cap;
// extend this policy only with verified host limits and matching prompt guidance.
func AwaitInterval(cliName string) time.Duration {
	if cliName == "claude" {
		return claudeAwaitInterval
	}
	return defaultAwaitInterval
}
