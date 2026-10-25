package control

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// resumeFixtureDir holds the resume-only states. They live beside, not in,
// fixtures/state so the nine-state validation of the earlier drivers is unchanged.
const resumeFixtureDir = "testdata/fixtures/resume"

type resumeCase struct {
	name, fixture string
	code          int
	archive       string // sprint archive written by the case, pinned as an artifact
}

// newResumeProject builds the disposable project of an earlier case, then swaps
// in the resume state so the existing runner and its normalisation stay unchanged.
func newResumeProject(t *testing.T, tc resumeCase) *project {
	t.Helper()
	p := newProject(t, "paused", false, false, false)
	fixture := filepath.Join(resumeFixtureDir, tc.fixture+".yaml")
	if _, err := os.Stat(fixture); err != nil {
		fixture = filepath.Join("testdata", "fixtures", "state", tc.fixture+".yaml")
	}
	p.before = []byte(strings.ReplaceAll(string(mustRead(t, fixture)), "<live-pid>", strconv.Itoa(os.Getpid())))
	mustWrite(t, filepath.Join(p.dir, ".liza", "state.yaml"), p.before, 0o644)
	return p
}

// TestOracleResumePaths pins the resume branches the A3 run-control split moves:
// mode and sprint decision, and the post-commit transition reporting.
func TestOracleResumePaths(t *testing.T) {
	linuxOnly(t)
	for _, tc := range []resumeCase{
		{name: "resume-from-breaker", fixture: "circuit-breaker-tripped"},
		{name: "resume-mid-checkpoint", fixture: "checkpoint-mid-sprint"},
		{name: "resume-checkpoint-all-terminal", fixture: "checkpoint-all-terminal"},
		{name: "resume-stopped-halt", fixture: "stopped-active-halt"},
		{name: "resume-refused-stopped", fixture: "stopped", code: 1},
		{name: "resume-transition-checkpoint", fixture: "transition-checkpoint"},
		{name: "resume-completed-advance", fixture: "completed-sprint", archive: "sprint-1.yaml"},
		{name: "resume-transition-failure", fixture: "completed-fan-in-stuck"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newResumeProject(t, tc)
			res := p.run(t, "resume")
			if res.code != tc.code {
				t.Fatalf("%s: exit %d, want %d; %s", tc.name, res.code, tc.code, p.normalise(t, "stderr", []byte(res.stderr)))
			}
			name := "resume-paths/" + tc.name
			p.human(t, name, res)
			after := append(p.state(t), p.outcomes(t)...)
			oracle.CheckState(t, name+".yaml", p.normalise(t, "state before", p.before), p.normalise(t, "state after", after))
			if tc.archive != "" {
				archive := mustRead(t, filepath.Join(p.dir, ".liza", "archive", tc.archive))
				oracle.Check(t, oracle.Artifact, name+".archive", p.normalise(t, "sprint archive", archive))
			}
		})
	}
}
