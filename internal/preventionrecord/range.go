package preventionrecord

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
)

// Commit is one commit of a replay range as git reports it.
type Commit struct {
	SHA     string
	Subject string
	// Message is the raw commit message.
	Message string
	// Trailers is git's final trailer block, verbatim.
	Trailers string
}

// Offender is a fix commit whose record has problems.
type Offender struct {
	SHA      string
	Subject  string
	Findings []Finding
}

// Result is the outcome of checking a range.
type Result struct {
	// Checked counts the fix commits of the range.
	Checked   int
	Offenders []Offender
}

// readFormat prints, per commit and NUL-separated, the full SHA, the subject,
// the raw message and git's final trailer block as written. With -z the
// commits are NUL-separated too, so every token is one field.
const readFormat = "%H%x00%s%x00%B%x00%(trailers)"

// ReadRange returns, oldest first, the commits the rebase of head onto
// integrationHead would publish: those reachable from head and not from base,
// leaving out merges and commits whose change integrationHead already holds.
// It runs one git log in dir, and two more when integration has advanced past
// base (see dropHeldChanges).
func ReadRange(dir, integrationHead, head, base string) ([]Commit, error) {
	out, err := gitenv.Output(dir, "log", "-z", "--no-merges", "--cherry-pick", "--right-only", "--reverse",
		"--no-show-signature", "--format="+readFormat, integrationHead+"..."+head, "^"+base, "--")
	if err != nil {
		return nil, gitError("git log", err)
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return nil, nil
	}
	if len(fields)%4 != 0 {
		return nil, fmt.Errorf("git log: unexpected output of %d fields", len(fields))
	}
	commits := make([]Commit, 0, len(fields)/4)
	for i := 0; i < len(fields); i += 4 {
		commits = append(commits, Commit{SHA: fields[i], Subject: fields[i+1], Message: fields[i+2], Trailers: fields[i+3]})
	}
	return dropHeldChanges(dir, integrationHead, base, commits)
}

// dropHeldChanges leaves out the commits whose change a non-merge commit of
// base..integrationHead already holds, by exact patch identity. --cherry-pick
// only compares the range with integration commits head does not reach: once
// the author merges integration into the task branch it compares with
// nothing, while the rebase still drops the change as empty.
//
// Ceiling: it reads every diff in base..integrationHead, so a task far behind a
// busy integration pays for that history on each check.
func dropHeldChanges(dir, integrationHead, base string, commits []Commit) ([]Commit, error) {
	if len(commits) == 0 {
		return commits, nil
	}
	held, err := patchIDs(dir, "--no-merges", base+".."+integrationHead, "--")
	if err != nil || len(held) == 0 {
		return commits, err
	}
	shas := make([]string, 0, len(commits)+2)
	shas = append(shas, "--no-walk=unsorted")
	for _, c := range commits {
		shas = append(shas, c.SHA)
	}
	own, err := patchIDs(dir, append(shas, "--")...)
	if err != nil {
		return nil, err
	}
	heldIDs := make(map[string]bool, len(held))
	for _, id := range held {
		heldIDs[id] = true
	}
	kept := make([]Commit, 0, len(commits))
	for _, c := range commits {
		// A commit without a diff has no patch identity and is never held.
		if id, ok := own[c.SHA]; ok && heldIDs[id] {
			continue
		}
		kept = append(kept, c)
	}
	return kept, nil
}

// patchIDs maps each commit git log selects with args to its exact patch
// identity, as internal/git's PatchID computes it; a commit without a diff is
// absent.
func patchIDs(dir string, args ...string) (map[string]string, error) {
	diffs, err := gitenv.Output(dir, append([]string{"log", "-p", "--pretty=medium", "--no-decorate", "--no-color",
		"--full-index", "--no-ext-diff", "--no-textconv", "--no-show-signature"}, args...)...)
	if err != nil {
		return nil, gitError("git log -p", err)
	}
	ids := map[string]string{}
	if len(strings.TrimSpace(string(diffs))) == 0 {
		return ids, nil
	}
	out, err := gitenv.OutputWithStdin(dir, string(diffs), "patch-id", "--verbatim")
	if err != nil {
		return nil, gitError("git patch-id", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 {
			ids[fields[1]] = fields[0]
		}
	}
	return ids, nil
}

// gitError wraps a git failure; the class of a failure is read from git's own
// text, so its stderr is kept.
func gitError(op string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s: %w: %s", op, err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return fmt.Errorf("%s: %w", op, err)
}

// Check reads the range and validates the record of each of its fix commits.
func Check(dir, integrationHead, head, base string) (Result, error) {
	commits, err := ReadRange(dir, integrationHead, head, base)
	if err != nil {
		return Result{}, err
	}
	return CheckCommits(commits), nil
}

// CheckCommits validates the record of each fix commit; other commits pass
// unchecked, whatever trailers they carry.
func CheckCommits(commits []Commit) Result {
	var result Result
	for _, c := range commits {
		if !IsFixSubject(c.Subject) {
			continue
		}
		result.Checked++
		if findings := Validate(c.Message, c.Trailers); len(findings) > 0 {
			result.Offenders = append(result.Offenders, Offender{SHA: c.SHA, Subject: c.Subject, Findings: findings})
		}
	}
	return result
}
