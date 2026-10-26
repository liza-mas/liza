package jsonout

import "testing"

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
