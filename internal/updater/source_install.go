package updater

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func installFromSource(ctx context.Context, commit, target string, stderr io.Writer) error {
	tmpDir, err := os.MkdirTemp("", binaryName()+"-source-update-*")
	if err != nil {
		return fmt.Errorf("create source checkout: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, sourceCheckoutDirName())
	// Clone with depth 1 for main branch
	if err := runStreaming(ctx, stderr, "git", "clone", "--depth", "1", "--branch", "main", sourceCloneURL(), repoDir); err != nil {
		return fmt.Errorf("clone %s source: %w", nameLower(), err)
	}
	// Fetch the exact commit we need (may not be in the shallow clone)
	fetchErr := runStreaming(ctx, stderr, "git", "-C", repoDir, "fetch", "--depth", "1", "origin", commit)
	if fetchErr != nil {
		// Fallback: try a deeper fetch if shallow exact fetch failed
		fmt.Fprintf(stderr, "Shallow fetch failed for commit %s, attempting deeper fetch...\n", commit)
		if err := runStreaming(ctx, stderr, "git", "-C", repoDir, "fetch", "origin", commit); err != nil {
			return fmt.Errorf("fetch commit %s (shallow and deep fetch both failed): %w (original shallow error: %v)", commit, err, fetchErr)
		}
	}
	// Checkout the fetched commit
	if err := runStreaming(ctx, stderr, "git", "-C", repoDir, "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("checkout commit %s: %w", commit, err)
	}
	installDir := filepath.Dir(target)
	makeArgs := sourceInstallMakeArgs(repoDir, installDir)
	if err := runStreaming(ctx, stderr, "make", makeArgs...); err != nil {
		return fmt.Errorf("build %s source: %w", nameLower(), err)
	}
	return nil
}

var runStreaming = func(ctx context.Context, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	return cmd.Run()
}
