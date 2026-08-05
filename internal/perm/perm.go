// Package perm denies and restores write access to a directory.
//
// Tests that exercise graceful write-failure paths need a directory the process
// genuinely cannot write to. On POSIX that is one chmod away. On Windows the
// POSIX bits os.Chmod accepts are mapped only to the read-only attribute, which
// applies to files and is ignored for directory entry creation, so a chmod
// there does not block anything and the test silently exercises the success
// path instead. Blocking writes on Windows requires an explicit deny entry in
// the directory's DACL, which is what the Windows implementation adds.
package perm

// DenyWrites blocks the current user from creating entries in dir, and returns
// a function restoring the previous state.
//
// Files already inside stay writable on both platforms, matching chmod 0555:
// callers that need a lock file or a marker to remain usable can pre-create it.
//
// Removing an existing entry is blocked on POSIX but not on Windows, where the
// right to delete a file is carried by the file itself and is not overridden by
// the parent directory. Do not build a test on removal failing.
//
// The returned restore function is safe to call once; call it from a defer so
// the directory can be cleaned up even when the test fails.
func DenyWrites(dir string) (restore func() error, err error) {
	return denyWrites(dir)
}
