//go:build !windows

package ops

// retrySharingViolation runs op once: POSIX opens never collide with a
// concurrent rename or unlink of the same path.
func retrySharingViolation(op func() error) error {
	return op()
}

// retryReplaceCollision runs op once: POSIX renames replace open files.
func retryReplaceCollision(op func() error) error {
	return op()
}
