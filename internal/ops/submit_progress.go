package ops

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// defaultSubmitHeartbeatInterval initialises submitHeartbeatInterval.
const defaultSubmitHeartbeatInterval = 5 * time.Minute

// submitHeartbeatInterval is how often a long submit repeats its progress
// line on stderr (D-188); tests shorten it.
var submitHeartbeatInterval = defaultSubmitHeartbeatInterval

// submitProgressOutput is read at each write, so the line follows whatever
// stderr is then. Never stdout, which carries the lifecycle result alone.
func submitProgressOutput() io.Writer { return os.Stderr }

// submitProgress tells a submit's caller what it is waiting for or running,
// on stderr (the logger is discarded under --json), once per state change and
// then every submitHeartbeatInterval. Silence was read as a stall and the
// submit interrupted (D-188). It never echoes command output. A nil
// *submitProgress reports nothing.
type submitProgress struct {
	taskID  string
	started time.Time
	stop    chan struct{}
	done    chan struct{}

	mu    sync.Mutex
	state string
	until time.Time
}

func startSubmitProgress(taskID string) *submitProgress {
	p := &submitProgress{taskID: taskID, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	go p.repeat(submitHeartbeatInterval)
	return p
}

func (p *submitProgress) repeat(interval time.Duration) {
	defer close(p.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.mu.Lock()
			p.writeLocked()
			p.mu.Unlock()
		}
	}
}

// set reports state, which ends at the latest at until (zero when unknown).
func (p *submitProgress) set(state string, until time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if state == p.state {
		return
	}
	p.state, p.until = state, until
	p.writeLocked()
}

func (p *submitProgress) writeLocked() {
	if p.state == "" {
		return
	}
	line := fmt.Sprintf("submit-for-review %s: %s; %s since the submit started", p.taskID, p.state, time.Since(p.started).Round(time.Second))
	if !p.until.IsZero() {
		line += ", at most until " + p.until.UTC().Format(time.RFC3339)
	}
	_, _ = fmt.Fprintln(submitProgressOutput(), line+". Still working: do not interrupt it or submit again.")
}

// finish stops the reporter and waits for it, so nothing is written once the
// submit has returned.
func (p *submitProgress) finish() {
	if p == nil {
		return
	}
	close(p.stop)
	<-p.done
}
