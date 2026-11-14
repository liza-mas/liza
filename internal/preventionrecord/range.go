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
// It runs one git log in dir.
func ReadRange(dir, integrationHead, head, base string) ([]Commit, error) {
	out, err := gitenv.Output(dir, "log", "-z", "--no-merges", "--cherry-pick", "--right-only", "--reverse",
		"--no-show-signature", "--format="+readFormat, integrationHead+"..."+head, "^"+base, "--")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// The class of a failure is read from git's own text.
			return nil, fmt.Errorf("git log: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("git log: %w", err)
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
	return commits, nil
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
