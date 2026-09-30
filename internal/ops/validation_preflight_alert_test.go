package ops

import (
	"os"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

func preflightAlerts(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(paths.New(root).AlertsLogPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, ValidationPreflightFailedCategory) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestValidationPreflightAlertsOnlyOnNewFailedObservation(t *testing.T) {
	t.Parallel()
	// GIVEN a claimable task whose one command declares two required variables
	f := newAssignmentPreflightFixture(t, models.TaskStatusRejected)
	if err := f.bb.Modify(func(s *models.State) error {
		s.FindTask("task-1").ValidationPrerequisites[0].Env = []string{"VALIDATION_FIRST", "VALIDATION_SECOND"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	attempt := func(env ...string) {
		t.Helper()
		session := &ValidationSession{Environment: append([]string{"PATH=/usr/bin:/bin"}, env...), Execution: "local", ForceCheck: true}
		_, _ = PrepareValidationPreflight(f.root, "task-1", "coder-1", "", session)
	}
	steps := []struct {
		name       string
		env        []string
		wantAlerts int
		wantInLast []string
	}{
		{name: "first failure alerts", wantAlerts: 1,
			wantInLast: []string{"task-1", "coder-1", "environment_missing", "command=0 check=0 variable=VALIDATION_FIRST", "repair-agent-pool --dry-run"}},
		{name: "unchanged retry is the same condition", wantAlerts: 1},
		{name: "a different failing check is a new condition", env: []string{"VALIDATION_FIRST=secret-first-value"}, wantAlerts: 2,
			wantInLast: []string{"command=0 check=1 variable=VALIDATION_SECOND"}},
		{name: "passing does not alert", env: []string{"VALIDATION_FIRST=secret-first-value", "VALIDATION_SECOND=secret-second-value"}, wantAlerts: 2},
		{name: "failing again after passing alerts", env: []string{"VALIDATION_FIRST=secret-first-value"}, wantAlerts: 3},
	}
	for _, step := range steps {
		// WHEN the agent preflights in that environment
		attempt(step.env...)

		// THEN only new failed observations are alerted, with sanitized fields only
		got := preflightAlerts(t, f.root)
		if len(got) != step.wantAlerts {
			t.Fatalf("%s: %d alerts, want %d: %q", step.name, len(got), step.wantAlerts, got)
		}
		last := got[len(got)-1]
		for _, want := range step.wantInLast {
			if !strings.Contains(last, want) {
				t.Fatalf("%s: alert %q lacks %q", step.name, last, want)
			}
		}
		for _, leaked := range []string{"secret-first-value", "secret-second-value", "canonical check"} {
			if strings.Contains(last, leaked) {
				t.Fatalf("%s: alert %q leaks %q", step.name, last, leaked)
			}
		}
	}
}
