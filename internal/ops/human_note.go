package ops

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
)

// AddHumanNoteResult acknowledges persisted operator input without echoing it.
type AddHumanNoteResult struct {
	Target    string    `json:"target"`
	Timestamp time.Time `json:"timestamp"`
	Bytes     int       `json:"bytes"`
	Warnings  []string  `json:"warnings,omitempty"`
}

// AddHumanNote appends operator input under the state lock. The CLI owns the
// operator-session boundary; a note grants no approval or lifecycle authority.
// Activity-log failure after persistence is a warning, not a retryable failure.
func AddHumanNote(projectRoot, target, message string) (*AddHumanNoteResult, error) {
	if strings.TrimSpace(target) == "" {
		return nil, &PreconditionError{Reason: "note target is required (task ID or all)"}
	}
	if strings.TrimSpace(message) == "" {
		return nil, &PreconditionError{Reason: "note must not be empty"}
	}
	if !utf8.ValidString(message) {
		return nil, &PreconditionError{Reason: "note must be valid UTF-8"}
	}
	if len(message) > statehygiene.MaxStateTextBytes {
		return nil, &PreconditionError{Reason: fmt.Sprintf("note exceeds %d-byte limit", statehygiene.MaxStateTextBytes)}
	}
	note := models.HumanNote{
		Message: message,
		For:     target,
		Extra: map[string]any{
			"source":    "operator_cli",
			"operation": "add-human-note",
		},
	}
	if err := statehygiene.ValidateState(&models.State{HumanNotes: []models.HumanNote{note}}); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("invalid note: %v", err)}
	}
	lp := paths.New(projectRoot)
	if err := db.For(lp.StatePath()).Modify(func(state *models.State) error {
		if target != "all" && state.FindTask(target) == nil {
			return &errors.NotFoundError{Entity: "task", ID: target}
		}
		note.Timestamp = time.Now().UTC()
		state.HumanNotes = append(state.HumanNotes, note)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("add human note: %w", err)
	}
	result := &AddHumanNoteResult{Target: target, Timestamp: note.Timestamp, Bytes: len(message)}
	entry := log.Entry{
		Timestamp: note.Timestamp,
		Agent:     "operator",
		Action:    "human_note_added",
		Detail:    fmt.Sprintf("Operator input recorded (%d bytes); no approval or task transition", len(message)),
	}
	if target != "all" {
		entry.Task = &target
	}
	if err := log.New(lp.LogPath()).Append(entry); err != nil {
		result.Warnings = append(result.Warnings, "Note persisted; activity log write failed. Do not retry the note.")
	}
	return result, nil
}
