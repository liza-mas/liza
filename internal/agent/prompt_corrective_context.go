package agent

import (
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/prompts"
)

const (
	correctiveNoteLimit  = 8
	correctiveTextRunes  = 4096
	correctiveLabelRunes = 128
)

// The human-note target and operator provenance values mirror the unexported
// ops constants (humanNoteForAll, humanNoteSourceOperatorCLI,
// addHumanNoteOperation): ops exports no owner for them, and exporting one is
// an edit of a closed area. The values must stay equal.
const (
	humanNoteTargetAll         = "all"
	humanNoteSourceOperatorCLI = "operator_cli"
	humanNoteOperationAddNote  = "add-human-note"
)

// buildTaskCorrectiveContext projects append-only recovery evidence without
// consuming notes or changing task authority. Overflow stays discoverable via CLI.
func buildTaskCorrectiveContext(task *models.Task, state *models.State) *prompts.TaskCorrectiveContext {
	context := &prompts.TaskCorrectiveContext{}
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventUnblocked || entry.Reason == nil || strings.TrimSpace(*entry.Reason) == "" {
			continue
		}
		agent := derefString(entry.Agent)
		reason := prompts.TruncateText(*entry.Reason, correctiveTextRunes)
		label := prompts.TruncateText(agent, correctiveLabelRunes)
		context.Unblock = &prompts.TaskUnblockResolution{
			Timestamp: entry.Time.UTC().Format(time.RFC3339), Agent: label, Reason: reason,
			Truncated: reason != *entry.Reason || label != agent,
		}
		break
	}
	for i := len(state.HumanNotes) - 1; i >= 0; i-- {
		note := state.HumanNotes[i]
		if (note.For != task.ID && note.For != humanNoteTargetAll) || strings.TrimSpace(note.Message) == "" {
			continue
		}
		if len(context.Notes) == correctiveNoteLimit {
			context.OmittedNotes++
			continue
		}
		source, _ := note.Extra["source"].(string)
		operation, _ := note.Extra["operation"].(string)
		view := prompts.TaskHumanNote{
			Timestamp:     note.Timestamp.UTC().Format(time.RFC3339),
			Target:        prompts.TruncateText(note.For, correctiveLabelRunes),
			Message:       prompts.TruncateText(note.Message, correctiveTextRunes),
			Source:        prompts.TruncateText(source, correctiveLabelRunes),
			Operation:     prompts.TruncateText(operation, correctiveLabelRunes),
			OperatorInput: source == humanNoteSourceOperatorCLI && operation == humanNoteOperationAddNote,
		}
		view.Truncated = view.Target != note.For || view.Message != note.Message || view.Source != source || view.Operation != operation
		context.Notes = append(context.Notes, view)
	}
	slices.Reverse(context.Notes)
	for _, exception := range ops.RecordedScopeExceptions(task.History) {
		context.ScopeExceptions = append(context.ScopeExceptions, prompts.TaskScopeException{
			Timestamp: exception.Time.UTC().Format(time.RFC3339), Agent: prompts.TruncateText(exception.Agent, correctiveLabelRunes),
			Files: exception.Files, Change: exception.Change, Unchanged: exception.Unchanged,
		})
	}
	if context.Unblock == nil && len(context.Notes) == 0 && len(context.ScopeExceptions) == 0 {
		return nil
	}
	return context
}
