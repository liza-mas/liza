package models

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxValidationNoteBytes = 4096
const MaxValidationNotesBytes = 16384

// PlanValidationNote selects one future output child without changing its contract.
type PlanValidationNote struct {
	OutputIndex int    `yaml:"output_index" json:"output_index"`
	Message     string `yaml:"message" json:"message"`
}

// ValidationNote retains the source of advisory guidance on a generated task.
type ValidationNote struct {
	ParentTask  string `yaml:"parent_task" json:"parent_task"`
	OutputIndex int    `yaml:"output_index" json:"output_index"`
	Message     string `yaml:"message" json:"message"`
}

// ValidatePlanValidationNotes checks bounded messages and unique output selectors.
func ValidatePlanValidationNotes(notes []PlanValidationNote, outputs int) error {
	seen := make(map[int]bool)
	total := 0
	for i, note := range notes {
		if note.OutputIndex < 0 || note.OutputIndex >= outputs {
			return fmt.Errorf("notes[%d].output_index must name an existing output", i)
		}
		if seen[note.OutputIndex] {
			return fmt.Errorf("notes[%d] duplicates output_index %d", i, note.OutputIndex)
		}
		seen[note.OutputIndex] = true
		if !utf8.ValidString(note.Message) || strings.TrimSpace(note.Message) == "" || len(note.Message) > MaxValidationNoteBytes {
			return fmt.Errorf("notes[%d].message must be nonempty UTF-8 of at most %d bytes", i, MaxValidationNoteBytes)
		}
		total += len(note.Message)
	}
	if total > MaxValidationNotesBytes {
		return fmt.Errorf("notes exceed %d-byte total message limit", MaxValidationNotesBytes)
	}
	return nil
}

// PlanCheckVerdict is the orchestrator's disposition of a merged plan before
// its children exist.
type PlanCheckVerdict string

const (
	// PlanCheckPassed admits the plan's reviewed hand-off transitions.
	PlanCheckPassed PlanCheckVerdict = "passed"
	// PlanCheckHeld parks the plan until a human performs Ask and an operator
	// clears the hold. Nothing but that clear releases it.
	PlanCheckHeld PlanCheckVerdict = "held"
	// PlanCheckReplaced retires an unused hand-off by an operator declaration.
	PlanCheckReplaced PlanCheckVerdict = "replaced"
)

// PlanCheck records the orchestrator's plan-executability disposition of a
// merged planning task. The task ID is the plan's identity: a merged plan's
// output is fixed. An operator can retire its unused hand-off by a separately
// merged correction without changing its delivered MERGED status.
type PlanCheck struct {
	Verdict    PlanCheckVerdict     `yaml:"verdict" json:"verdict"`
	Ask        string               `yaml:"ask,omitempty" json:"ask,omitempty"`
	ReplacedBy string               `yaml:"replaced_by,omitempty" json:"replaced_by,omitempty"`
	By         string               `yaml:"by" json:"by"`
	At         time.Time            `yaml:"at" json:"at"`
	Notes      []PlanValidationNote `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// PlanHandoffRetired reports an explicit retirement, distinct from replan.
func (t *Task) PlanHandoffRetired() bool {
	return t.PlanCheckVerdictOf() == PlanCheckReplaced
}

// PlanCheckVerdictOf returns the task's disposition, or "" when it has none.
func (t *Task) PlanCheckVerdictOf() PlanCheckVerdict {
	if t == nil || t.PlanCheck == nil {
		return ""
	}
	return t.PlanCheck.Verdict
}

// ValidationNotesForOutput copies guidance without sharing the plan's slice.
func (t *Task) ValidationNotesForOutput(index int) []ValidationNote {
	if t.PlanCheckVerdictOf() != PlanCheckPassed {
		return nil
	}
	var notes []ValidationNote
	for _, note := range t.PlanCheck.Notes {
		if note.OutputIndex == index {
			notes = append(notes, ValidationNote{ParentTask: t.ID, OutputIndex: index, Message: note.Message})
		}
	}
	return notes
}
