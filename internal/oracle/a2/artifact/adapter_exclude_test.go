//go:build linux

package artifact

import "github.com/liza-mas/liza/internal/worktreeexclude"

func writeRepoExclude(linked string) error {
	return worktreeexclude.EnsureRepoExclude(linked, "/fixture-runtime/", "/fixture-index.json")
}

func writePrivateExclude(linked string) error {
	return worktreeexclude.EnsurePrivateExclude(linked, "fixture-local/", "fixture.tmp")
}
