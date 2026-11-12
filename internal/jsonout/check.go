package jsonout

import (
	"errors"
	"strings"
)

// CheckStatus is the state a check observed. Every status except CheckOK
// marks the command's outcome as degraded.
type CheckStatus string

const (
	CheckOK      CheckStatus = "ok"
	CheckMissing CheckStatus = "missing"
	CheckFailed  CheckStatus = "failed"
	CheckManual  CheckStatus = "manual"
)

// Severity tells a caller how much a non-ok check matters: error when the
// product cannot work at all, warn when it works in a reduced way.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
	SeverityInfo  Severity = "info"
)

// Check is one observed item of a command's outcome, emitted in
// result.checks of the --json envelope. The format is specified in
// specs/protocols/outcome-checks.md; callers parse it, so changing it is a
// breaking CLI change.
type Check struct {
	ID       string      `json:"id"`
	Status   CheckStatus `json:"status"`
	Severity Severity    `json:"severity"`
	Provider string      `json:"provider,omitempty"`
	Path     string      `json:"path,omitempty"`
	Message  string      `json:"message"`
	Fix      string      `json:"fix,omitempty"`
}

// CheckDefinition declares a kind of check and the severity its non-ok
// results carry.
type CheckDefinition struct {
	ID       string
	Severity Severity
}

// Check builds a result of this definition. It is the only place the
// severity rule is applied: an ok check is info, any other status takes the
// definition's severity.
func (d CheckDefinition) Check(status CheckStatus, provider, path, message, fix string) Check {
	severity := d.Severity
	if status == CheckOK {
		severity = SeverityInfo
	}
	return Check{ID: d.ID, Status: status, Severity: severity, Provider: provider, Path: path, Message: message, Fix: fix}
}

// ErrDegraded marks a command that completed but left at least one check
// not ok. The CLI maps it to the dedicated degraded exit code.
var ErrDegraded = errors.New("completed with degraded checks")

// Report collects the checks of one command run. It is the result of the
// --json envelope (result.checks). A nil *Report collects nothing, so code
// paths can report unconditionally.
type Report struct {
	Checks []Check `json:"checks"`
}

// NewReport returns an empty report whose checks serialize as [] rather than
// null.
func NewReport() *Report {
	return &Report{Checks: []Check{}}
}

// Add records a check; it is a no-op on a nil report.
func (r *Report) Add(check Check) {
	if r == nil {
		return
	}
	r.Checks = append(r.Checks, check)
}

// Degraded reports whether any check is not ok.
func (r *Report) Degraded() bool {
	return len(r.NotOK()) > 0
}

// NotOK returns the checks whose status is not ok, in report order.
func (r *Report) NotOK() []Check {
	if r == nil {
		return nil
	}
	var out []Check
	for _, check := range r.Checks {
		if check.Status != CheckOK {
			out = append(out, check)
		}
	}
	return out
}

// String renders a check on one line for human diagnostics.
func (c Check) String() string {
	var b strings.Builder
	b.WriteString(c.ID)
	if c.Provider != "" {
		b.WriteString(" " + c.Provider)
	}
	if c.Path != "" {
		b.WriteString(" " + c.Path)
	}
	b.WriteString(": " + c.Message)
	if c.Fix != "" {
		b.WriteString(" (" + c.Fix + ")")
	}
	return b.String()
}
