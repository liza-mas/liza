package statevalidate

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// Persisted digest shapes and acceptance-receipt bounds.
const (
	gitSHA1HexLen            = 40
	sha256HexLen             = 64
	sha256Bytes              = 32
	maxAcceptanceMappings    = 256
	maxAcceptanceCommands    = 64
	maxAcceptanceOutputBytes = 1024 * 1024
)

// Admission performs repository checks. State validation only checks persisted
// shape, leaving missing receipts repairable through update-review-commit.
func validateAcceptanceState(v *violations, task *models.Task) {
	validateArchivedFields(v, task)
	source := task.AcceptanceSource
	if source == nil {
		if task.AcceptanceReceipt != nil || len(task.Archived) > 0 {
			v.add(fmt.Errorf("task %s acceptance_receipt requires acceptance_source", task.ID))
		}
		return
	}
	if source.Ref == "" {
		v.add(fmt.Errorf("task %s acceptance_source requires ref and parent_task (missing ref)", task.ID))
	}
	if source.ParentTask == "" {
		v.add(fmt.Errorf("task %s acceptance_source requires ref and parent_task (missing parent_task)", task.ID))
	}
	for _, id := range []struct{ field, value string }{
		{"commit", source.Commit},
		{"blob", source.Blob},
		{"parent_review_commit", source.ParentReviewCommit},
	} {
		if _, err := hex.DecodeString(id.value); err != nil || len(id.value) != gitSHA1HexLen || strings.ToLower(id.value) != id.value {
			v.add(fmt.Errorf("task %s acceptance_source requires immutable lowercase object IDs (%s)", task.ID, id.field))
		}
	}
	receipt := task.AcceptanceReceipt
	if receipt == nil {
		return
	}
	for _, check := range []struct {
		name   string
		broken bool
	}{
		{"version", receipt.Version != 1},
		{"review_commit", task.ReviewCommit == nil || receipt.ReviewCommit != *task.ReviewCommit},
		{"source", receipt.Source != *source},
	} {
		if check.broken {
			v.add(fmt.Errorf("task %s acceptance_receipt does not match its source and review_commit (%s)", task.ID, check.name))
		}
	}
	for _, check := range []struct {
		name   string
		broken bool
	}{
		{"manifest_path", receipt.ManifestPath == ""},
		{"mappings", len(receipt.Mappings) == 0 || len(receipt.Mappings) > maxAcceptanceMappings},
		{"commands", len(receipt.Commands)+len(receipt.Deferred) > maxAcceptanceCommands},
	} {
		if check.broken {
			v.add(fmt.Errorf("task %s acceptance_receipt has invalid manifest or result bounds (%s)", task.ID, check.name))
		}
	}
	if _, err := hex.DecodeString(receipt.ManifestBlob); err != nil || len(receipt.ManifestBlob) != gitSHA1HexLen {
		v.add(fmt.Errorf("task %s acceptance_receipt requires manifest blob identity", task.ID))
	}
	totalOutput := 0
	// A receipt is written once and never edited, so a command's index is a
	// stable owner: the same defect on two commands stays two violations.
	for i, command := range receipt.Commands {
		totalOutput += len(command.Output)
		v.within(fmt.Sprintf("task %s acceptance_receipt command %d", task.ID, i), func(v *violations) {
			for _, check := range []struct {
				name   string
				broken bool
			}{
				{"exit code", command.ExitCode != 0},
				{"started_at", command.StartedAt.IsZero()},
				{"finished_at", command.FinishedAt.Before(command.StartedAt)},
			} {
				if check.broken {
					v.add(fmt.Errorf("task %s acceptance_receipt contains unsuccessful execution (command %d %s)", task.ID, i, check.name))
				}
			}
			if _, err := hex.DecodeString(command.CommandSHA256); err != nil || len(command.CommandSHA256) != sha256HexLen {
				v.add(fmt.Errorf("task %s acceptance_receipt requires canonical command identity (command %d)", task.ID, i))
			}
		})
	}
	if totalOutput > maxAcceptanceOutputBytes {
		v.add(fmt.Errorf("task %s acceptance_receipt output exceeds 1 MiB", task.ID))
	}
	for i, deferral := range receipt.Deferred {
		ordered := i == 0 || deferral.Index > receipt.Deferred[i-1].Index
		if _, err := hex.DecodeString(deferral.CommandSHA256); err != nil || len(deferral.CommandSHA256) != sha256HexLen ||
			deferral.Index < 0 || !ordered || deferral.TimeoutSeconds < 1 || !deferral.Authority.IsValid() {
			v.add(fmt.Errorf("task %s acceptance_receipt has an invalid deferred command (deferred %d)", task.ID, i))
		}
	}
}

// validateArchivedFields checks the refs of fields moved to archive objects.
// Like receipts, it checks shape only and opens no files: one ref per field,
// only on terminal tasks, never alongside the live value it replaces.
func validateArchivedFields(v *violations, task *models.Task) {
	if len(task.Archived) == 0 {
		return
	}
	if !task.Status.IsTerminal() {
		v.addID(fmt.Sprintf("task %s has archived fields but is not terminal", task.ID),
			fmt.Errorf("task %s in status %s has archived fields; only terminal tasks may", task.ID, task.Status))
	}
	seen := map[string]bool{}
	for _, ref := range task.Archived {
		if ref.Field != models.ArchivedFieldAcceptanceReceipt {
			v.add(fmt.Errorf("task %s archived field %q is not archivable", task.ID, ref.Field))
		}
		if seen[ref.Field] {
			v.add(fmt.Errorf("task %s has more than one archived %s", task.ID, ref.Field))
		}
		seen[ref.Field] = true
		if _, err := hex.DecodeString(ref.SHA256); err != nil || len(ref.SHA256) != sha256HexLen || strings.ToLower(ref.SHA256) != ref.SHA256 {
			v.add(fmt.Errorf("task %s archived %s requires a lowercase SHA-256 digest", task.ID, ref.Field))
		}
		if ref.ArchivedAt.IsZero() {
			v.add(fmt.Errorf("task %s archived %s requires archived_at", task.ID, ref.Field))
		}
	}
	if seen[models.ArchivedFieldAcceptanceReceipt] && task.AcceptanceReceipt != nil {
		v.add(fmt.Errorf("task %s has both a live and an archived acceptance_receipt", task.ID))
	}
}
