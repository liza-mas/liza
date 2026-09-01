package paths

import (
	"path/filepath"
	"strings"
)

// NormalizeSpecRef strips worktree path prefixes from spec_ref values.
// Agents working in git worktrees see files at `.worktrees/<task-id>/specs/...`,
// but spec_ref should always be relative to the project root (e.g. `specs/...`).
//
// Pattern matched: anything up to and including `.worktrees/<segment>/` is removed.
// If no worktree prefix is found, the input is returned as-is.
//
// Safety: if the remainder after stripping is empty, absolute, starts with "..",
// or is a fragment-only ref (e.g. "#anchor"), the input is returned unchanged so
// downstream validators can reject it with the original context intact.
func NormalizeSpecRef(specRef string) string {
	if specRef == "" {
		return specRef
	}

	const marker = ".worktrees/"
	_, after, found := strings.Cut(specRef, marker)
	if !found {
		return specRef
	}

	// Find the end of the task-id segment after ".worktrees/"
	_, remainder, hasSlash := strings.Cut(after, "/")
	if !hasSlash {
		// ".worktrees/task-id" with no trailing path — nothing to normalize to
		return specRef
	}

	// Validate remainder is a safe repo-relative path. If not, return original
	// unchanged so downstream validators reject it with full context.
	if remainder == "" {
		return specRef
	}
	if IsAbsAnyPlatform(remainder) {
		return specRef
	}
	if strings.HasPrefix(remainder, "#") {
		return specRef
	}
	if cleaned := filepath.Clean(remainder); strings.HasPrefix(cleaned, "..") {
		return specRef
	}

	return remainder
}

// SplitRefFile returns the file path portion of a ref, stripping any #fragment.
func SplitRefFile(ref string) string {
	if file, _, ok := strings.Cut(ref, "#"); ok {
		return file
	}
	return ref
}

// SplitRefFragment returns the fragment portion of a ref (without the #), or "" if none.
func SplitRefFragment(ref string) string {
	if _, frag, ok := strings.Cut(ref, "#"); ok {
		return frag
	}
	return ""
}

// IsAbsAnyPlatform returns true if the path is absolute on any platform.
// filepath.IsAbs is platform-dependent, so we also check for Unix single-leading
// slash paths, Windows rooted-backslash paths, and drive-letter prefixes (e.g.
// "C:\", "D:/") to catch cross-platform inputs. A spec_ref remainder like
// "/etc/passwd" is absolute on Unix and must be treated as absolute here even
// when the host is Windows, otherwise NormalizeSpecRef would launder it into a
// repo-relative path.
func IsAbsAnyPlatform(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	// Each leading separator is rooted on at least one supported platform.
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		return true
	}
	// Windows drive letter: letter + colon (e.g. "C:\", "C:/", "C:file")
	if len(path) >= 2 && path[1] == ':' {
		c := path[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}
