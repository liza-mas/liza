package ops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// casPublicationFixture returns a repository whose integration branch is
// checked out and a task commit not yet on it. With diverged set, integration
// also moved past the task's base, so publication needs a true merge; without
// it, publication is a fast-forward.
func casPublicationFixture(t *testing.T, diverged bool) (repo, taskCommit string) {
	t.Helper()
	repo = t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	testhelpers.MustGit(t, repo, "checkout", "integration")
	testhelpers.MustGit(t, repo, "checkout", "-b", "task/cas-publication")
	if err := os.WriteFile(filepath.Join(repo, "cas-task.txt"), []byte("task candidate\n"), 0644); err != nil {
		t.Fatalf("Failed to write task file: %v", err)
	}
	testhelpers.MustGit(t, repo, "add", "cas-task.txt")
	testhelpers.MustGit(t, repo, "commit", "-m", "Task candidate")
	taskCommit = testhelpers.MustGit(t, repo, "rev-parse", "HEAD")
	testhelpers.MustGit(t, repo, "checkout", "integration")
	if diverged {
		if err := os.WriteFile(filepath.Join(repo, "cas-integration.txt"), []byte("integration candidate\n"), 0644); err != nil {
			t.Fatalf("Failed to write integration file: %v", err)
		}
		testhelpers.MustGit(t, repo, "add", "cas-integration.txt")
		testhelpers.MustGit(t, repo, "commit", "-m", "Integration candidate")
	}
	return repo, taskCommit
}

// advanceIntegrationOnEveryAttempt installs mergeCASRetryTestHook so that each
// attempt's update-ref loses its compare-and-swap: right after the attempt read
// the integration HEAD, another merge lands on top of it. It returns the
// attempt numbers seen.
func advanceIntegrationOnEveryAttempt(t *testing.T, repo string) *[]int {
	t.Helper()
	var attempts []int
	previous := mergeCASRetryTestHook
	mergeCASRetryTestHook = func(attempt int, integrationRef, preMergeHEAD string) error {
		attempts = append(attempts, attempt)
		tree := testhelpers.MustGit(t, repo, "rev-parse", preMergeHEAD+"^{tree}")
		competing := testhelpers.MustGit(t, repo, "commit-tree", tree, "-p", preMergeHEAD, "-m", "competing merge "+strconv.Itoa(attempt))
		testhelpers.MustGit(t, repo, "update-ref", integrationRef, competing, preMergeHEAD)
		return nil
	}
	t.Cleanup(func() { mergeCASRetryTestHook = previous })
	return &attempts
}

// TestPerformCASMergeExhaustsBoundedRetries pins the retry bound of both
// publication branches: when every attempt loses its compare-and-swap the merge
// stops after maxMergeRetries attempts, publishes nothing of its own and reports
// the contention. The first attempt of the fast-forward case loses on the
// fast-forward branch, the later ones on the true-merge branch.
func TestPerformCASMergeExhaustsBoundedRetries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		diverged  bool
		withGuard bool
	}{
		{name: "fast-forward then true merge with guard", diverged: false, withGuard: true},
		{name: "true merge with guard", diverged: true, withGuard: true},
		{name: "true merge without guard", diverged: true, withGuard: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, taskCommit := casPublicationFixture(t, tc.diverged)
			attempts := advanceIntegrationOnEveryAttempt(t, repo)

			var guarded []string
			var guard func(string) error
			if tc.withGuard {
				guard = func(candidateTreeish string) error {
					guarded = append(guarded, candidateTreeish)
					return nil
				}
			}

			outcome, err := performCASMerge(git.New(repo), "refs/heads/integration", taskCommit, "cas-exhausted", guard)
			if err == nil || !strings.Contains(err.Error(), "merge CAS failed after 3 attempts") || !strings.Contains(err.Error(), "high contention on refs/heads/integration") {
				t.Fatalf("performCASMerge() error = %v, want CAS exhaustion after 3 attempts", err)
			}
			if outcome != nil {
				t.Fatalf("outcome = %#v, want nil", outcome)
			}
			if got := *attempts; len(got) != maxMergeRetries || got[0] != 0 || got[1] != 1 || got[2] != 2 {
				t.Fatalf("CAS attempts = %v, want exactly 0,1,2", got)
			}
			if tc.withGuard && len(guarded) != maxMergeRetries {
				t.Fatalf("guard calls = %d, want one per attempt (%d)", len(guarded), maxMergeRetries)
			}
			head := testhelpers.MustGit(t, repo, "rev-parse", "integration")
			for _, candidate := range append([]string{taskCommit}, guarded...) {
				if head == candidate {
					t.Fatalf("integration points at unpublished candidate %s", candidate)
				}
			}
			if err := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", taskCommit, head).Run(); err == nil {
				t.Fatal("task commit reached integration although every compare-and-swap lost")
			}
		})
	}
}

// TestPublishGuardedRef covers the one guarded publication step both CAS
// branches share: the guard sees the candidate before update-ref, a guard
// failure retries only when the HEAD it read has moved, a lost compare-and-swap
// retries, and any other update failure carries the branch's own prefix.
func TestPublishGuardedRef(t *testing.T) {
	const ref = "refs/heads/integration"
	setup := func(t *testing.T) (repo, base, candidate string) {
		t.Helper()
		repo, candidate = casPublicationFixture(t, false)
		return repo, testhelpers.MustGit(t, repo, "rev-parse", "integration"), candidate
	}
	moveIntegration := func(t *testing.T, repo, base string) string {
		t.Helper()
		tree := testhelpers.MustGit(t, repo, "rev-parse", base+"^{tree}")
		moved := testhelpers.MustGit(t, repo, "commit-tree", tree, "-p", base, "-m", "moved")
		testhelpers.MustGit(t, repo, "update-ref", ref, moved, base)
		return moved
	}

	t.Run("accepted candidate is published after the guard saw it", func(t *testing.T) {
		repo, base, candidate := setup(t)
		var seen []string
		retry, err := publishGuardedRef(git.New(repo), ref, candidate, base, func(c string) error {
			seen = append(seen, c)
			if head := testhelpers.MustGit(t, repo, "rev-parse", "integration"); head != base {
				t.Fatalf("ref advanced before the guard ran: %s", head)
			}
			return nil
		}, "failed to publish")
		if retry || err != nil {
			t.Fatalf("publishGuardedRef() = (%v, %v), want published", retry, err)
		}
		if len(seen) != 1 || seen[0] != candidate {
			t.Fatalf("guard saw %v, want [%s]", seen, candidate)
		}
		if head := testhelpers.MustGit(t, repo, "rev-parse", "integration"); head != candidate {
			t.Fatalf("integration = %s, want %s", head, candidate)
		}
	})

	t.Run("nil guard publishes", func(t *testing.T) {
		repo, base, candidate := setup(t)
		retry, err := publishGuardedRef(git.New(repo), ref, candidate, base, nil, "failed to publish")
		if retry || err != nil {
			t.Fatalf("publishGuardedRef() = (%v, %v), want published", retry, err)
		}
	})

	t.Run("guard refusal on an unmoved head is returned and nothing is published", func(t *testing.T) {
		repo, base, candidate := setup(t)
		guardErr := errors.New("artifact guard rejected candidate")
		retry, err := publishGuardedRef(git.New(repo), ref, candidate, base, func(string) error { return guardErr }, "failed to publish")
		if retry || err != guardErr {
			t.Fatalf("publishGuardedRef() = (%v, %v), want (false, %v)", retry, err, guardErr)
		}
		if head := testhelpers.MustGit(t, repo, "rev-parse", "integration"); head != base {
			t.Fatalf("integration = %s, want unchanged %s", head, base)
		}
	})

	t.Run("guard failure on a moved head retries", func(t *testing.T) {
		repo, base, candidate := setup(t)
		moved := moveIntegration(t, repo, base)
		retry, err := publishGuardedRef(git.New(repo), ref, candidate, base, func(string) error { return errors.New("stale view") }, "failed to publish")
		if !retry || err != nil {
			t.Fatalf("publishGuardedRef() = (%v, %v), want retry", retry, err)
		}
		if head := testhelpers.MustGit(t, repo, "rev-parse", "integration"); head != moved {
			t.Fatalf("integration = %s, want %s", head, moved)
		}
	})

	t.Run("lost compare-and-swap retries and leaves the other merge in place", func(t *testing.T) {
		repo, base, candidate := setup(t)
		moved := moveIntegration(t, repo, base)
		retry, err := publishGuardedRef(git.New(repo), ref, candidate, base, nil, "failed to publish")
		if !retry || err != nil {
			t.Fatalf("publishGuardedRef() = (%v, %v), want retry", retry, err)
		}
		if head := testhelpers.MustGit(t, repo, "rev-parse", "integration"); head != moved {
			t.Fatalf("integration = %s, want %s", head, moved)
		}
	})

	t.Run("other update failure carries the branch prefix", func(t *testing.T) {
		repo, base, _ := setup(t)
		retry, err := publishGuardedRef(git.New(repo), ref, strings.Repeat("1", 40), base, nil, "failed to update integration branch")
		if retry || err == nil || !strings.HasPrefix(err.Error(), "failed to update integration branch: ") {
			t.Fatalf("publishGuardedRef() = (%v, %v), want prefixed update failure", retry, err)
		}
	})
}

// TestMergePublicationNamedValues pins the bytes the named publication values
// replaced: lock labels, ref prefix, merge commit message, short-SHA width and
// the retry bound are observable in lock errors, logs and Git history.
func TestMergePublicationNamedValues(t *testing.T) {
	t.Parallel()
	if integrationLockForward != "forward " || integrationLockRollback != "rollback " || integrationLockRestore != "restore " {
		t.Fatalf("lock labels = %q %q %q", integrationLockForward, integrationLockRollback, integrationLockRestore)
	}
	if integrationBranchRefPrefix != "refs/heads/" {
		t.Fatalf("integrationBranchRefPrefix = %q", integrationBranchRefPrefix)
	}
	if got := fmt.Sprintf(mergeCommitMessageFormat, "task-7", "task-7"); got != "Merge task-7 (task/task-7)" {
		t.Fatalf("merge commit message = %q", got)
	}
	if got := shortSHA("0123456789abcdef"); got != "0123456" || shortSHAWidth != 7 {
		t.Fatalf("shortSHA = %q (width %d), want 7 characters", got, shortSHAWidth)
	}
	if maxMergeRetries != 3 {
		t.Fatalf("maxMergeRetries = %d, want 3", maxMergeRetries)
	}
}
