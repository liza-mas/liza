package jsonout

import (
	"errors"
	"testing"
)

// The --json codes agents branch on. The literals are written out so that
// renaming a constant cannot rename a code.
func TestA7ErrorCodesKeepTheirSpellings(t *testing.T) {
	for code, want := range map[errorCode]string{
		codeNotFound: "not_found", codeRaceCondition: "race_condition", codeValidation: "validation",
		codePermissionDenied: "permission_denied", codeStaleCaller: "stale_caller", codeProjectRoot: "project_root",
		codePipelineConfig: "pipeline_config", codeStateSchema: "state_schema", codeWorktreeContext: "worktree_context",
		codeUnblockConflict: "unblock_rebase_conflict", codeLockTimeout: "lock_timeout", codeLockStale: "lock_stale",
		codeFilesystem: "filesystem", codeInternal: "internal",
	} {
		if string(code) != want {
			t.Errorf("code constant spells %q, want %q", code, want)
		}
	}
}

// An untyped message names a lock timeout only when it carries "lock" and
// either timeout phrase.
func TestA7LockTimeoutFragmentsKeepTheirSpellings(t *testing.T) {
	for msg, want := range map[string]string{
		"could not take the lock: timeout": "lock_timeout", "lock wait timed out": "lock_timeout",
		"timeout": "internal", "lock held": "internal",
	} {
		if got, _ := ClassifyError(errors.New(msg)); got != want {
			t.Errorf("ClassifyError(%q) code = %q, want %q", msg, got, want)
		}
	}
}
