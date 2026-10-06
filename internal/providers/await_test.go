package providers

import (
	"testing"
	"time"
)

func TestAwaitInterval(t *testing.T) {
	t.Parallel()
	for _, cli := range []string{"claude", "", "codex", "claude-acp", "codex-acp", "kimi", "cursor-acp", "unknown", "Claude"} {
		t.Run(cli, func(t *testing.T) {
			t.Parallel()
			want, limit := 100*time.Second, 120*time.Second
			if cli == "claude" {
				want, limit = 540*time.Second, 600*time.Second
			}
			if got := AwaitInterval(cli); got != want || got >= limit {
				t.Fatalf("AwaitInterval(%q) = %s; want %s below %s", cli, got, want, limit)
			}
		})
	}
}
