package ops

import (
	stderrors "errors"

	"github.com/liza-mas/liza/internal/models"
)

// Verdict constants for AwaitVerdictResult.Verdict.
const (
	VerdictApproved   = "APPROVED"
	VerdictRejected   = "REJECTED"
	VerdictNewAttempt = "NEW_ATTEMPT"
	VerdictTerminal   = "TERMINAL"
	VerdictTimeout    = "TIMEOUT"
	VerdictAborted    = "ABORTED"
)

// ErrBudgetExhausted is returned when the iteration/review-cycle budget
// would be exceeded on rejection — the agent should exit normally.
var ErrBudgetExhausted = stderrors.New("budget exhausted: iteration or review-cycle limit reached")

// AwaitVerdictResult holds the outcome of blocking on a review verdict.
type AwaitVerdictResult struct {
	Verdict       string            // One of the Verdict* constants
	Reason        string            // Rejection reason or terminal explanation
	ReviewerAgent string            // Agent ID that issued the verdict (empty if timeout/abort)
	TaskStatus    models.TaskStatus // Final observed task status
	Iteration     int               // Current iteration number (post-reclaim if rejected)
	Guidance      string            // Inline guidance for the agent on rejection
}
