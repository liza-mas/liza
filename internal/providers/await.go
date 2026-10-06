package providers

import "time"

// AwaitInterval uses tool identity: sharing a backend or contract does not
// imply a shared foreground limit. Unknown tools keep the conservative cap;
// extend this policy only with verified host limits and matching prompt guidance.
func AwaitInterval(cliName string) time.Duration {
	if cliName == "claude" {
		return 540 * time.Second // Headroom below Bash's explicit 600-second timeout.
	}
	return 100 * time.Second
}
