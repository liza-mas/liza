package a9

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// reviewRecipe is the committed git recipe of the derived CODE_TO_REVIEW
// fixture: the task branch commit the starting-commit coder submitted.
type reviewRecipe struct {
	Branch               string            `json:"branch"`
	Worktree             string            `json:"worktree"`
	BaseRef              string            `json:"base_ref"`
	Files                map[string]string `json:"files"`
	Message              string            `json:"message"`
	ExpectedReviewCommit string            `json:"expected_review_commit"`
}

// reviewGit says what a scenario takes from the derived fixture: the name of its
// state file ("" keeps the scenario's own or the common state), and which parts
// of the task branch the recipe replays and keeps.
type reviewGit struct {
	state            string
	branch, worktree bool
}

// Derived state files: the state the starting-commit coder left, and the same
// state without the recorded worktree path, the only form the reviewer claim
// accepts when the worktree directory is absent (review_boundary.go).
const (
	reviewStateRecorded   = "state.yaml"
	reviewStateUnrecorded = "state-unrecorded-worktree.yaml"
)

// newReviewScenario prepares a scenario whose state is the derived
// CODE_TO_REVIEW fixture unless the scenario ships its own state.yaml, and,
// when asked, replays the derived recipe: the task branch commit with fixed
// dates and its worktree. The recipe SHA is asserted (AC-derived-3).
func newReviewScenario(t *testing.T, name string, keep reviewGit) *scenario {
	t.Helper()
	p := newScenario(t, name)
	if _, err := os.Stat(filepath.Join("testdata", "fixtures", name, "state.yaml")); os.IsNotExist(err) && keep.state != "" {
		p.write(t, ".liza/state.yaml", mustRead(t, filepath.Join("testdata", "fixtures", "derived-code-to-review", keep.state)))
		p.before = p.state(t)
	}
	if !keep.branch && !keep.worktree {
		return p
	}
	var recipe reviewRecipe
	if err := readJSON("testdata/fixtures/derived-code-to-review/recipe.json", &recipe); err != nil {
		t.Fatal("A9 setup: derived recipe unavailable")
	}
	worktree := filepath.Join(p.dir, recipe.Worktree)
	p.git(t, "worktree", "add", "-q", "-b", recipe.Branch, recipe.Worktree, recipe.BaseRef)
	for path, content := range recipe.Files {
		p.write(t, filepath.Join(recipe.Worktree, path), []byte(content))
		p.git(t, "-C", worktree, "add", "--", path)
	}
	p.git(t, "-C", worktree, "commit", "-q", "-m", recipe.Message)
	sha := strings.TrimSpace(p.git(t, "-C", worktree, "rev-parse", "HEAD"))
	if err := verifyRecipeSHA(gitRecipe{ExpectedSHA: recipe.ExpectedReviewCommit}, sha); err != nil {
		t.Fatal(err)
	}
	p.recipeSHAs = append(p.recipeSHAs, sha)
	if !keep.worktree {
		p.git(t, "worktree", "remove", "--force", recipe.Worktree)
	}
	if !keep.branch {
		p.git(t, "branch", "-D", recipe.Branch)
	}
	return p
}

var (
	reviewLeaseRE = regexp.MustCompile(`(?m)^\s*review_lease_expires: (\S+)$`)
	reviewClaimRE = regexp.MustCompile(`(?m)^\s*- time: (\S+)\n\s*event: claimed\n\s*agent: (\S+)$`)
)

// reviewLeaseError checks the raw at-launch state of a reviewer claim, before
// normalisation: the agent heartbeat is the claim instant, and the review
// lease, the claim receipt lease and the agent lease are each that instant plus
// the default 1800 s. The derived fixture also carries the doer's older claim
// receipt, which the check skips by reading the last two lease expiries.
func reviewLeaseError(raw []byte, reviewer string) error {
	beat := heartbeatRE.FindSubmatch(raw)
	if beat == nil {
		return fmt.Errorf("no agent heartbeat")
	}
	base, err := time.Parse(time.RFC3339Nano, string(beat[1]))
	if err != nil {
		return fmt.Errorf("heartbeat not a timestamp")
	}
	check := func(label string, value []byte) error {
		at, err := time.Parse(time.RFC3339Nano, string(value))
		if err != nil {
			return fmt.Errorf("%s not a timestamp", label)
		}
		if got := at.Sub(base); got != defaultLease {
			return fmt.Errorf("%s %s after claim, want %s", label, got, defaultLease)
		}
		return nil
	}
	review := reviewLeaseRE.FindSubmatch(raw)
	if review == nil {
		return fmt.Errorf("no review lease")
	}
	if err := check("review lease", review[1]); err != nil {
		return err
	}
	expiries := leaseExpiryRE.FindAllSubmatch(raw, -1)
	if len(expiries) < 2 {
		return fmt.Errorf("%d lease expiries, want receipt and agent", len(expiries))
	}
	if err := check("claim receipt lease", expiries[len(expiries)-2][1]); err != nil {
		return err
	}
	if err := check("agent lease", expiries[len(expiries)-1][1]); err != nil {
		return err
	}
	for _, claim := range reviewClaimRE.FindAllSubmatch(raw, -1) {
		if string(claim[2]) != reviewer {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, string(claim[1])); err != nil || !at.Equal(base) {
			return fmt.Errorf("claim instant differs from heartbeat")
		}
		return nil
	}
	return fmt.Errorf("no claim history entry by the reviewer")
}

func requireReviewLease(t testing.TB, raw []byte, reviewer string) {
	t.Helper()
	if err := reviewLeaseError(raw, reviewer); err != nil {
		t.Fatalf("A9 review lease: %v", err)
	}
}

// TestOracleA9ReviewLeaseAssertion proves the review lease check rejects
// altered durations and a missing base, so I-08 cannot pass on presence alone.
func TestOracleA9ReviewLeaseAssertion(t *testing.T) {
	t.Parallel()
	const tmpl = "tasks:\n  t:\n    review_lease_expires: %s\n    receipts:\n      lease_expires: 2026-01-01T00:00:00Z\n      lease_expires: \"%s\"\n    history:\n      - time: %s\n        event: claimed\n        agent: r-1\nagents:\n  r-1:\n    lease_expires: %s\n    heartbeat: 2026-01-01T01:00:00.5Z\n"
	build := func(review, receipt, claim, agent string) []byte {
		return fmt.Appendf(nil, tmpl, review, receipt, claim, agent)
	}
	const beat, end = "2026-01-01T01:00:00.5Z", "2026-01-01T01:30:00.5Z"
	cases := []struct {
		name string
		raw  []byte
		ok   bool
	}{
		{"default", build(end, end, beat, end), true},
		{"shortened review lease", build("2026-01-01T01:10:00.5Z", end, beat, end), false},
		{"shortened receipt lease", build(end, "2026-01-01T01:10:00.5Z", beat, end), false},
		{"shortened agent lease", build(end, end, beat, "2026-01-01T01:10:00.5Z"), false},
		{"claim before beat", build(end, end, "2026-01-01T01:00:00Z", end), false},
		{"no review lease", []byte("agents:\n  r-1:\n    heartbeat: 2026-01-01T01:00:00.5Z\n"), false},
	}
	for _, tc := range cases {
		if err := reviewLeaseError(tc.raw, "r-1"); (err == nil) != tc.ok {
			t.Fatalf("review lease check %s: error %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func reviewerArgs(extra ...string) []string {
	return append([]string{"agent", "code-reviewer", "--cli", "claude", "--agent-id", "code-reviewer-1"}, extra...)
}

func TestOracleA9ReviewerClaim(t *testing.T) {
	t.Parallel()
	p := newReviewScenario(t, "i08-claim", reviewGit{state: reviewStateRecorded, branch: true, worktree: true})
	r := p.run(t, reviewerArgs()...)
	requireCode(t, p, r, 0)
	launch := mustRead(t, filepath.Join(p.recordDir, "launch-1", "state.yaml"))
	requireReviewLease(t, launch, "code-reviewer-1")
	for _, value := range []string{"status: REVIEWING_CODE", "reviewing_by: code-reviewer-1", "operation: claim-reviewer-task", "role: code-reviewer", "status: REVIEWING", "current_task: task-1"} {
		if !bytes.Contains(launch, []byte(value)) {
			t.Fatalf("I-08 at-launch state missing %s", value)
		}
	}
	// I-09 refusal: an intact worktree is not recovered.
	if bytes.Contains(launch, []byte("event: worktree_recovered")) || bytes.Contains(launch, []byte("status: BLOCKED")) {
		t.Fatal("I-08 intact worktree must not be recovered or blocked")
	}
	p.check(t, r)
}

// TestOracleA9ReviewerClaimRefused keeps the task under another reviewer's
// unexpired review lease: the starting binary registers, idles and never claims
// or launches, and the test ends the run with liza stop.
func TestOracleA9ReviewerClaimRefused(t *testing.T) {
	t.Parallel()
	p := newReviewScenario(t, "i08-refused", reviewGit{state: reviewStateRecorded, branch: true, worktree: true})
	a := p.start(t, reviewerArgs()...)
	p.await(t, "idle registration", func() bool {
		s := p.state(t)
		return bytes.Contains(s, []byte("code-reviewer-1:")) && bytes.Contains(s, []byte("status: IDLE"))
	})
	// The claim step follows registration within the same second; a short dwell
	// lets a wrongly admitted claim surface before the absence is asserted.
	time.Sleep(time.Second)
	p.observeLaunch(t)
	state := p.state(t)
	if len(p.records(t)) != 0 || bytes.Contains(state, []byte("reviewing_by: code-reviewer-1")) || !bytes.Contains(state, []byte("reviewing_by: code-reviewer-2")) ||
		bytes.Contains(state, []byte("agent: code-reviewer-1\n")) {
		t.Fatal("I-08 foreign unexpired review lease must prevent claim and launch")
	}
	if r := p.run(t, "stop"); r.code != 0 {
		t.Fatal("I-08 stop failed")
	}
	r := p.finish(t, a, 10*time.Second)
	requireCode(t, p, r, 0)
	p.check(t, r)
}

// TestOracleA9ReviewerWorktree covers I-09 (worktree removed, branch present:
// recovered) and I-10 (both absent: task BLOCKED). The recoverable case is the
// I-10 refusal; the intact worktree of TestOracleA9ReviewerClaim is the I-09
// refusal.
func TestOracleA9ReviewerWorktree(t *testing.T) {
	t.Parallel()
	t.Run("i09-recovered", func(t *testing.T) {
		t.Parallel()
		p := newReviewScenario(t, "i09-recovered", reviewGit{state: reviewStateUnrecorded, branch: true})
		r := p.run(t, reviewerArgs()...)
		requireCode(t, p, r, 0)
		launch := mustRead(t, filepath.Join(p.recordDir, "launch-1", "state.yaml"))
		if !bytes.Contains(launch, []byte("event: worktree_recovered")) || !bytes.Contains(launch, []byte("status: REVIEWING_CODE")) || bytes.Contains(launch, []byte("status: BLOCKED")) {
			t.Fatal("I-09 must recover the worktree and keep the review claim")
		}
		if _, err := os.Stat(filepath.Join(p.dir, ".worktrees", "task-1", "review-feature.txt")); err != nil {
			t.Fatal("I-09 recovered worktree must hold the task branch commit")
		}
		p.check(t, r)
	})
	t.Run("i10-blocked", func(t *testing.T) {
		t.Parallel()
		p := newReviewScenario(t, "i10-blocked", reviewGit{state: reviewStateUnrecorded})
		a := p.start(t, reviewerArgs()...)
		p.await(t, "blocked task", func() bool { return bytes.Contains(p.state(t), []byte("status: BLOCKED")) })
		p.observeLaunch(t)
		state := p.state(t)
		if len(p.records(t)) != 0 || !bytes.Contains(state, []byte("blocked_reason: worktree and branch both missing")) ||
			bytes.Contains(state, []byte("reviewing_by:")) || bytes.Contains(state, []byte("event: worktree_recovered")) {
			t.Fatal("I-10 must block the task, clear the review claim and launch no provider")
		}
		if _, err := os.Stat(filepath.Join(p.dir, ".worktrees", "task-1")); !os.IsNotExist(err) {
			t.Fatal("I-10 must not create a worktree")
		}
		if r := p.run(t, "stop"); r.code != 0 {
			t.Fatal("I-10 stop failed")
		}
		r := p.finish(t, a, 10*time.Second)
		requireCode(t, p, r, 0)
		p.check(t, r)
	})
}

// reviewLiveness is the liveness record the heartbeat publishes.
type reviewLiveness struct {
	AgentID            string    `json:"agent_id"`
	Generation         string    `json:"generation"`
	Seq                int64     `json:"seq"`
	Heartbeat          time.Time `json:"heartbeat"`
	LeaseExpires       time.Time `json:"lease_expires"`
	ReviewTask         string    `json:"review_task"`
	ReviewLeaseExpires time.Time `json:"review_lease_expires"`
	ReviewSeq          int64     `json:"review_seq"`
}

var reviewSeqRE = regexp.MustCompile(`"(seq|review_seq)":\d+`)

// reviewHeartbeatRecord asserts the semantics of the liveness record the
// held session left and canonicalises its two sequence numbers in place.
// Both derive from the beat's wall clock (nanoseconds since the epoch), which no
// normaliser key covers; equal values stay equal after the rewrite, and their
// relation to the heartbeat instant is asserted here instead.
func reviewHeartbeatRecord(t *testing.T, p *scenario, launch []byte) {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(p.dir, ".liza", "state.yaml.liveness-*"))
	if len(paths) != 1 {
		t.Fatalf("A9 %s collect: %d liveness records, want 1", p.name, len(paths))
	}
	raw := mustRead(t, paths[0])
	var rec reviewLiveness
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("A9 %s collect: liveness record is not JSON", p.name)
	}
	if rec.AgentID != "code-reviewer-1" || rec.Generation == "" || rec.ReviewTask != "task-1" {
		t.Fatal("I-03 record must name the reviewer, its generation and the reviewed task")
	}
	if got := rec.LeaseExpires.Sub(rec.Heartbeat); got != defaultLease {
		t.Fatalf("I-03 agent lease %s after beat, want %s", got, defaultLease)
	}
	if !rec.ReviewLeaseExpires.Equal(rec.LeaseExpires) {
		t.Fatal("I-03 review lease must renew with the agent lease")
	}
	if rec.Seq != rec.Heartbeat.UnixNano() || rec.ReviewSeq != rec.Seq {
		t.Fatal("I-03 sequence numbers must derive from the beat instant")
	}
	claim, err := time.Parse(time.RFC3339Nano, string(heartbeatRE.FindSubmatch(launch)[1]))
	if err != nil {
		t.Fatal("A9 collect: launch heartbeat")
	}
	if elapsed := rec.Heartbeat.Sub(claim); elapsed < 45*time.Second || elapsed > 100*time.Second {
		t.Fatalf("I-03 first beat %s after the claim, want about one default interval", elapsed)
	}
	if err := os.WriteFile(paths[0], reviewSeqRE.ReplaceAll(raw, []byte(`"$1":1`)), 0o644); err != nil {
		t.Fatal("A9 collect: canonicalise liveness record")
	}
}

// TestOracleA9Heartbeat holds a reviewer session until the first default-interval
// beat publishes its liveness record (R23), then lets the stub stop the run.
func TestOracleA9Heartbeat(t *testing.T) {
	t.Parallel()
	p := newReviewScenario(t, "i03-success", reviewGit{state: reviewStateRecorded, branch: true, worktree: true})
	r := p.run(t, reviewerArgs()...)
	requireCode(t, p, r, 0)
	launch := mustRead(t, filepath.Join(p.recordDir, "launch-1", "state.yaml"))
	requireReviewLease(t, launch, "code-reviewer-1")
	reviewHeartbeatRecord(t, p, launch)
	p.check(t, r)
}

var reviewAgeRE = regexp.MustCompile(`"age":\s*"[^"]*"|\b\d+d \d+h \d+m ago\b`)

// reviewAgeless replaces the anomaly age, a duration from the record's timestamp
// to the wall clock that moves every minute, by a token; no other value is
// touched.
func reviewAgeless(s string) string {
	return reviewAgeRE.ReplaceAllStringFunc(s, func(match string) string {
		if strings.HasPrefix(match, `"age"`) {
			return `"age": "<AGE>"`
		}
		return "<AGE> ago"
	})
}

// reviewHuman is the human golden content of one invocation: exit code, then
// every stdout and stderr line that is not a structured log record.
func reviewHuman(r invocation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "exit_code: %d\n[stdout]\n", r.code)
	for i, stream := range []string{r.stdout, r.stderr} {
		for _, line := range strings.Split(stream, "\n") {
			if strings.HasPrefix(line, "time=") && strings.Contains(line, " level=") {
				continue
			}
			b.WriteString(line + "\n")
		}
		if i == 0 {
			b.WriteString("[stderr]\n")
		}
	}
	return b.String()
}

// reviewGoldenCheck runs the anomaly read surfaces (human, then --json) and
// checks both outputs and the unchanged fixture state against goldens.
func reviewGoldenCheck(t *testing.T, p *scenario, args ...string) {
	t.Helper()
	human := p.run(t, args...)
	asJSON := p.run(t, append(append([]string{}, args...), "--json")...)
	requireCode(t, p, human, 0)
	requireCode(t, p, asJSON, 0)
	shas := p.fixedSHAs(t)
	n := p.harness(t, shas)
	n.Check(t, oracle.Human, p.name+"/get-anomalies.txt", []byte(newA9Normaliser(shas).text(reviewAgeless(reviewHuman(human)))))
	n.Check(t, oracle.JSON, p.name+"/get-anomalies.json", []byte(newA9Normaliser(shas).text(reviewAgeless(asJSON.stdout))))
	norm := newA9Normaliser(shas)
	n.CheckState(t, p.name+"/state.yaml", []byte(norm.text(string(p.before))), []byte(norm.text(string(p.state(t)))))
}

// TestOracleA9PendingMergeStall pins the stall record's observable surface per
// decision D42 (specs/goals/20261023-architecture-consolidation-decisions.md):
// the producer, recordPendingMergeStall, is reached only after about 606 s of
// retries and wake rounds, so the success fixture holds exactly the record it
// writes (fields and values from strategy_reviewer.go at 96c9ad69a) and the
// starting CLI's anomaly read surfaces run over it. The trigger path stays
// guarded by the white-box TestReviewerPreWork_PendingMergeStallRecordsAnomaly.
func TestOracleA9PendingMergeStall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		records int
	}{{"i11-stall", 1}, {"i11-nostall", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newReviewScenario(t, tc.name, reviewGit{})
			if got := bytes.Count(p.before, []byte("type: pending_merge_stalled")); got != tc.records {
				t.Fatalf("I-11 fixture holds %d stall records, want %d", got, tc.records)
			}
			reviewGoldenCheck(t, p, "get", "anomalies")
			if !bytes.Equal(p.before, p.state(t)) {
				t.Fatal("I-11 read surfaces must not change the state")
			}
		})
	}
}

// TestOracleA9HeartbeatRefused is the I-03 refusal: an exported heartbeat that
// holds a stale generation of a registered agent refuses at its first default
// beat (the adapter, AC2). No liveness record is written and state.yaml keeps its
// bytes.
func TestOracleA9HeartbeatRefused(t *testing.T) {
	t.Parallel()
	p := newReviewScenario(t, "i03-refused", reviewGit{})
	got, err := heartbeatRefused(filepath.Join(p.dir, ".liza", "state.yaml"), "coder-1", 75*time.Second)
	if err != nil {
		t.Fatalf("A9 %s run: heartbeat adapter: %v", p.name, err)
	}
	records, _ := filepath.Glob(filepath.Join(p.dir, ".liza", "state.yaml.liveness-*"))
	unchanged := bytes.Equal(p.before, p.state(t))
	if !got.AuthorityError || !got.Returned || len(records) != 0 || !unchanged {
		t.Fatalf("I-03 stale generation must be refused before publishing: %+v, %d records, state unchanged %v", got, len(records), unchanged)
	}
	shas := p.fixedSHAs(t)
	n := p.harness(t, shas)
	norm := newA9Normaliser(shas)
	n.CheckState(t, p.name+"/state.yaml", []byte(norm.text(string(p.before))), []byte(norm.text(string(p.state(t)))))
	outcome := fmt.Sprintf("authority_error: %t\nreturned_before_deadline: %t\nliveness_records: %d\nstate_unchanged: %t\n", got.AuthorityError, got.Returned, len(records), unchanged)
	n.Check(t, oracle.Human, p.name+"/outcome.txt", []byte(outcome))
}
