package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/termutil"
)

// ErrProjectCleanupDeclined reports that the user declined destructive cleanup.
var ErrProjectCleanupDeclined = errors.New("project cleanup cancelled by user")

// CleanupParams configures project cleanup confirmation.
type CleanupParams struct {
	ProjectRoot string
	Stdin       io.Reader
	Stderr      io.Writer
	AutoConfirm bool
}

// CleanupResult reports whether any project targets were removed.
type CleanupResult struct {
	Cleaned bool
	Plan    ops.ProjectCleanupPlan
}

// CleanupProjectCommand plans, confirms, and executes project cleanup.
func CleanupProjectCommand(params CleanupParams) (*CleanupResult, error) {
	plan, err := ops.PlanProjectCleanup(params.ProjectRoot)
	if err != nil {
		return nil, err
	}
	result := &CleanupResult{Plan: plan}
	if plan.Empty() {
		return result, nil
	}
	if len(plan.LiveAgents) > 0 {
		return nil, fmt.Errorf("cannot clean project while agents are still running: %s", strings.Join(plan.LiveAgents, ", "))
	}

	stderr := params.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdin := cleanupBufferedReader(params.Stdin)

	fmt.Fprintln(stderr, "Existing workspace data will be permanently deleted:")
	for _, dir := range plan.Directories {
		fmt.Fprintf(stderr, "  directory: %s\n", dir)
	}
	for _, worktree := range plan.Worktrees {
		fmt.Fprintf(stderr, "  worktree:  %s\n", worktree.Path)
		fmt.Fprintf(stderr, "  branch:    %s\n", worktree.Branch)
	}
	fmt.Fprintln(stderr, "This discards runtime state, task worktree files, uncommitted changes, and the listed task branches.")
	fmt.Fprint(stderr, "Delete these targets? (y/n): ")
	if params.AutoConfirm {
		fmt.Fprintln(stderr, "yes")
	} else {
		response, readErr := termutil.ReadSingleKey(stdin)
		fmt.Fprintln(stderr)
		if readErr != nil {
			return nil, fmt.Errorf("read cleanup confirmation: %w; rerun with --yes or leave the workspace unchanged", readErr)
		}
		if response != confirmAnswerY {
			return nil, ErrProjectCleanupDeclined
		}
	}

	if err := ops.ExecuteProjectCleanup(plan); err != nil {
		return nil, err
	}
	result.Cleaned = true
	return result, nil
}

func cleanupBufferedReader(input io.Reader) *bufio.Reader {
	if input == nil {
		input = os.Stdin
	}
	if reader, ok := input.(*bufio.Reader); ok {
		return reader
	}
	return bufio.NewReader(input)
}
