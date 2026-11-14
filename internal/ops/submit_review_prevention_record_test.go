package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/preventionrecord"
	"github.com/liza-mas/liza/internal/preventionrecord/recordfixture"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// recordScenario is a task worktree whose commits are cases cherry-picked from
// the I3 fixture, never hand-written copies of their messages.
type recordScenario struct {
	root, taskID, agentID, wt, branch string
	head                              string
	g                                 *git.Git
	bb                                *db.Blackboard
}

type recordScenarioOptions struct {
	rolePair, status, agentRole string
	taskType                    models.TaskType
	waiveTDD                    bool
	integrationConfig           string
	cases                       []string
}

func (o recordScenarioOptions) withDefaults() recordScenarioOptions {
	if o.rolePair == "" {
		o.rolePair, o.status, o.agentRole, o.taskType = "coding-pair", string(models.TaskStatusImplementing), "coder", models.TaskTypeCoding
	}
	if o.integrationConfig == "" {
		o.integrationConfig = "integration"
	}
	return o
}

func cherryPickCase(t *testing.T, fixtureDir, wt, id string) {
	t.Helper()
	tag := "refs/tags/case/" + id
	testhelpers.MustGit(t, wt, "fetch", "--quiet", fixtureDir, tag+":"+tag)
	testhelpers.MustGit(t, wt, "-c", "user.name=Task Author", "-c", "user.email=author@example.invalid",
		"cherry-pick", "--allow-empty", "--keep-redundant-commits", "case/"+id)
}

func newRecordScenario(t *testing.T, fixtureDir string, opts recordScenarioOptions) *recordScenario {
	t.Helper()
	opts = opts.withDefaults()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.MustGit(t, root, "checkout", "integration")

	g := git.New(root)
	// The task ID names the worktree directory under a temp dir that already
	// carries the test name; git rebase stats an 83-char range relative to it,
	// so a long ID exceeds Windows MAX_PATH. Each scenario has its own repo.
	name := t.Name()
	taskID := "task-" + strings.ToLower(name[strings.LastIndex(name, "/")+1:])
	if len(taskID) > 32 {
		taskID = taskID[:32]
	}
	base, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wt := g.GetWorktreePath(taskID)
	for _, id := range opts.cases {
		cherryPickCase(t, fixtureDir, wt, id)
	}

	agentID := opts.agentRole + "-1"
	lease := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	extra := map[string]any{"intent": "test", "validation_plan": "test", "files_to_modify": []string{"x"}}
	if opts.waiveTDD {
		extra["tdd_not_required"] = "fixture commits carry no test files"
	}
	state := &models.State{
		Config: models.Config{IntegrationBranch: opts.integrationConfig, LeaseDuration: 1800},
		Tasks: []models.Task{{
			ID: taskID, Type: opts.taskType, Description: "prevention record gate", Status: models.TaskStatus(opts.status),
			RolePair: opts.rolePair, AssignedTo: &agentID, LeaseExpires: &lease, Worktree: &worktree, BaseCommit: &base,
			Iteration: 1, Created: time.Now().UTC(),
			History: []models.TaskHistoryEntry{{Time: time.Now().UTC(), Event: models.TaskEventPreExecutionCheckpoint, Agent: &agentID, Extra: extra}},
		}},
		Agents: map[string]models.Agent{agentID: {Role: opts.agentRole, Status: models.AgentStatusWorking, CurrentTask: &taskID}},
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	return &recordScenario{
		root: root, taskID: taskID, agentID: agentID, wt: wt, g: g, bb: bb,
		branch: paths.TaskBranchPrefix + taskID,
		head:   testhelpers.MustGit(t, wt, "rev-parse", "HEAD"),
	}
}

func (s *recordScenario) submit() (*SubmitForReviewResult, error) {
	return SubmitForReview(s.root, s.taskID, s.head, s.agentID)
}

func (s *recordScenario) task(t *testing.T) models.Task {
	t.Helper()
	state, err := s.bb.Read()
	testhelpers.AssertNoError(t, err)
	return *state.FindTask(s.taskID)
}

// requireUntouched checks that a refusal left the task as the claim made it.
func (s *recordScenario) requireUntouched(t *testing.T, before models.Task) {
	t.Helper()
	after := s.task(t)
	if after.Status != before.Status || after.AssignedTo == nil || *after.AssignedTo != s.agentID ||
		!after.LeaseExpires.Equal(*before.LeaseExpires) || len(after.History) != len(before.History) ||
		(after.Lifecycle != nil && after.Lifecycle.Preparation != nil) || after.ReviewCommit != nil {
		t.Errorf("task changed by a refusal: status %s -> %s, history %d -> %d, preparation %t, review commit %t",
			before.Status, after.Status, len(before.History), len(after.History),
			after.Lifecycle != nil && after.Lifecycle.Preparation != nil, after.ReviewCommit != nil)
	}
}

func asRecordRefusal(t *testing.T, err error) *PreconditionError {
	t.Helper()
	var refusal *PreconditionError
	if !stderrors.As(err, &refusal) {
		t.Fatalf("want a *PreconditionError, got %T: %v", err, err)
	}
	return refusal
}

func offendingCommits(t *testing.T, refusal *PreconditionError) []map[string]any {
	t.Helper()
	commits, ok := refusal.Details["offending_commits"].([]map[string]any)
	if !ok {
		t.Fatalf("offending_commits has type %T", refusal.Details["offending_commits"])
	}
	return commits
}

func problemsOf(t *testing.T, commit map[string]any) []map[string]string {
	t.Helper()
	problems, ok := commit["problems"].([]map[string]string)
	if !ok {
		t.Fatalf("problems has type %T", commit["problems"])
	}
	return problems
}

func TestSubmitForReview_PreventionRecord(t *testing.T) {
	fixture, err := recordfixture.Build(filepath.Join(t.TempDir(), "fixture"))
	if err != nil {
		t.Fatalf("build the fixture: %v", err)
	}
	coding := func(cases ...string) recordScenarioOptions {
		return recordScenarioOptions{waiveTDD: true, cases: cases}
	}

	// refused asserts that the record check refuses the cases, with the problem
	// names it must report, leaving the task untouched, and returns the
	// scenario, the submission's error and the refusal it wraps.
	refused := func(t *testing.T, cases []string, want ...string) (*recordScenario, *PreconditionError, error) {
		t.Helper()
		s := newRecordScenario(t, fixture.Dir, coding(cases...))
		before := s.task(t)
		_, err := s.submit()
		if err == nil {
			t.Fatal("want a refusal, got nil")
		}
		refusal := asRecordRefusal(t, err)
		for _, name := range want {
			if !strings.Contains(refusal.Reason, name) {
				t.Errorf("reason does not name %q", name)
			}
		}
		s.requireUntouched(t, before)
		return s, refusal, err
	}

	t.Run("E1_omitted_record_is_a_correctable_refusal", func(t *testing.T) {
		t.Parallel()
		s, refusal, err := refused(t, []string{"c09"}, "record_missing")
		var lifecycle *LifecycleError
		if !stderrors.As(err, &lifecycle) {
			t.Fatalf("want a *LifecycleError")
		}
		if o := lifecycle.Outcome; o.Outcome != models.LifecycleInvalidInput || o.SafeAction != string(LifecycleSafeActionCorrectInput) || o.Effects != string(LifecycleEffectsNone) {
			t.Errorf("outcome %q, safe action %q, effects %q", o.Outcome, o.SafeAction, o.Effects)
		}
		if len(offendingCommits(t, refusal)) != 1 {
			t.Fatalf("offending commits: %v", refusal.Details["offending_commits"])
		}
		c09, _ := fixture.Case("c09")
		published := offendingCommits(t, refusal)[0]["sha"].(string)
		for _, want := range []string{published[:12], c09.Subject, "~/" + paths.GlobalDirName() + "/skills/shared/references/prevention-record.md",
			"git -C " + s.wt + " log -1 --format=%B <sha>", "git -C " + s.wt + " rebase --onto HEAD <sha> " + s.branch} {
			if !strings.Contains(refusal.Reason, want) {
				t.Errorf("reason does not contain %q", want)
			}
		}
		if strings.Contains(refusal.Reason, published[:13]) {
			t.Error("reason names more than the 12-character SHA prefix")
		}
	})

	t.Run("E2_record_outside_the_final_block_is_refused", func(t *testing.T) {
		t.Parallel()
		_, refusal, _ := refused(t, []string{"c13"}, "outside_final_block")
		if published := offendingCommits(t, refusal)[0]["sha"].(string); !strings.Contains(refusal.Reason, published[:12]) {
			t.Errorf("reason does not name the commit %s", published[:12])
		}
		if got := problemsOf(t, offendingCommits(t, refusal)[0]); len(got) != 1 || got[0]["problem"] != "outside_final_block" {
			t.Errorf("problems = %v", got)
		}
	})

	t.Run("E3_accepted_names_scheduled_and_human", func(t *testing.T) {
		t.Parallel()
		refused(t, []string{"c08"}, "disposition_accepted", "scheduled", "human")
	})

	t.Run("E4_malformed_record_names_the_key", func(t *testing.T) {
		t.Parallel()
		_, refusal, _ := refused(t, []string{"c11"}, "key_missing (Prevention-Class)")
		want := []map[string]string{{"problem": "key_missing", "key": "Prevention-Class"}}
		if got := problemsOf(t, offendingCommits(t, refusal)[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("problems = %v, want %v", got, want)
		}
	})

	t.Run("E5_the_repair_is_executable", func(t *testing.T) {
		t.Parallel()
		s, refusal, _ := refused(t, []string{"c09", "c16"}, "record_missing")
		offender := offendingCommits(t, refusal)[0]["sha"].(string)

		// The reason's own commands, with the shell redirect replaced by a file
		// the test writes.
		var commands []string
		for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(refusal.Reason, -1) {
			commands = append(commands, m[1])
		}
		file := filepath.Join(t.TempDir(), "message")
		message := testhelpers.MustGit(t, s.wt, "log", "-1", "--format=%B", offender)
		record := "Prevention-Cause: the trim ran on one path only\nPrevention-Concept: internal/trim/trim.go#Trim\nPrevention-Disposition: removed\nPrevention-Class: gap\n"
		if err := os.WriteFile(file, []byte(strings.TrimSpace(message)+"\n\n"+record), 0o644); err != nil {
			t.Fatal(err)
		}
		start := -1
		for i, command := range commands {
			if strings.Contains(command, "checkout --detach") {
				start = i
			}
		}
		if start < 0 || len(commands) < start+3 {
			t.Fatalf("the reason holds no earlier-commit repair: %v", commands)
		}
		for _, command := range commands[start : start+3] {
			command = strings.NewReplacer("<sha>", offender, "<file>", file).Replace(command)
			fields := strings.Fields(command)
			if fields[0] != "git" {
				t.Fatalf("repair step is not a git command: %q", command)
			}
			testhelpers.MustGit(t, s.wt, append([]string{"-c", "user.name=Task Author", "-c", "user.email=author@example.invalid"}, fields[1:]...)...)
		}

		s.head = testhelpers.MustGit(t, s.wt, "rev-parse", "HEAD")
		if branch := testhelpers.MustGit(t, s.wt, "branch", "--show-current"); branch != s.branch {
			t.Fatalf("repair left the worktree on %q, want %q", branch, s.branch)
		}
		if _, err := s.submit(); err != nil {
			t.Fatalf("resubmission after the repair: %v", err)
		}
	})

	t.Run("E6_valid_records_reverts_and_non_fix_commits_pass", func(t *testing.T) {
		t.Parallel()
		for _, cases := range [][]string{{"c14"}, {"c05", "c15"}, {"c01", "c16"}} {
			s := newRecordScenario(t, fixture.Dir, coding(cases...))
			if _, err := s.submit(); err != nil {
				t.Errorf("cases %v: %v", cases, err)
			}
		}
	})

	t.Run("E7_only_published_commits_are_checked", func(t *testing.T) {
		t.Parallel()
		s := newRecordScenario(t, fixture.Dir, coding("c14"))
		// Integration gains a record-less fix after the worktree was created; the
		// author merges integration into the task branch.
		if err := os.WriteFile(filepath.Join(s.root, "human.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, s.root, "add", "human.txt")
		testhelpers.MustGit(t, s.root, "-c", "user.name=Human", "-c", "user.email=human@example.invalid", "commit", "--quiet", "-m", "fix: a human's change")
		testhelpers.MustGit(t, s.wt, "-c", "user.name=Task Author", "-c", "user.email=author@example.invalid", "merge", "--quiet", "--no-ff", "-m", "Merge integration", "integration")
		s.head = testhelpers.MustGit(t, s.wt, "rev-parse", "HEAD")
		if _, err := s.submit(); err != nil {
			t.Fatalf("an author-merged integration must not be refused: %v", err)
		}
	})

	t.Run("E8_every_doer_and_after_the_test_file_check", func(t *testing.T) {
		t.Parallel()
		planning := newRecordScenario(t, fixture.Dir, recordScenarioOptions{
			rolePair: "epic-planning-pair", status: "EPIC_PLANNING", agentRole: "epic-planner", taskType: models.TaskTypePlanning, cases: []string{"c09"},
		})
		_, err := planning.submit()
		if err == nil || !strings.Contains(asRecordRefusal(t, err).Reason, "record_missing") {
			t.Errorf("a planning doer holding c09 must be refused by the record check, got %v", err)
		}

		noTests := newRecordScenario(t, fixture.Dir, recordScenarioOptions{cases: []string{"c09"}})
		_, err = noTests.submit()
		if err == nil {
			t.Fatal("want the test-file refusal")
		}
		reason := asRecordRefusal(t, err).Reason
		if !strings.Contains(reason, "code tasks must include test files") || strings.Contains(reason, "prevention record") {
			t.Errorf("a coding task without test files gets the test-file refusal first, got %q", reason)
		}
	})

	const branchMessage, rangeMessage = "failed to resolve integration branch HEAD", "failed to check prevention records"
	wantOperationalKeys := []string{"git_exit_code", "git_failure", "operation", "recovery_hint", "task_id"}
	// unresolvableBranch is a coding task whose configured integration branch
	// names nothing.
	unresolvableBranch := func(t *testing.T) *recordScenario {
		t.Helper()
		opts := coding("c14")
		opts.integrationConfig = "no-such-integration-branch"
		return newRecordScenario(t, fixture.Dir, opts)
	}
	asOperational := func(t *testing.T, err error) *OperationalError {
		t.Helper()
		var operational *OperationalError
		if !stderrors.As(err, &operational) {
			t.Fatalf("want an *OperationalError, got %T", err)
		}
		return operational
	}

	t.Run("E9_an_unresolvable_integration_branch_is_operational", func(t *testing.T) {
		t.Parallel()
		s := unresolvableBranch(t)
		before := s.task(t)
		_, err := s.submit()
		operational := asOperational(t, err)
		if operational.Code != operationalCodeGitOperation || operational.Phase != "check-prevention-records" {
			t.Errorf("code %q, phase %q", operational.Code, operational.Phase)
		}
		if operational.Message != branchMessage {
			t.Errorf("message = %q, want %q", operational.Message, branchMessage)
		}
		if got := sortedMapKeys(operational.Details); !reflect.DeepEqual(got, wantOperationalKeys) {
			t.Errorf("details keys = %v, want %v", got, wantOperationalKeys)
		}
		if operational.Details["git_exit_code"] != 128 || operational.Details["git_failure"] != string(git.FailureBadRevision) {
			t.Errorf("git_exit_code = %v, git_failure = %v", operational.Details["git_exit_code"], operational.Details["git_failure"])
		}
		if hint, _ := operational.Details["recovery_hint"].(string); !strings.Contains(hint, "integration branch") {
			t.Error("recovery_hint does not name the integration branch")
		}
		// Git's text stays out of everything the caller sees.
		for _, leaked := range []string{"no-such-integration-branch", "rev-parse", "fatal:"} {
			if strings.Contains(err.Error(), leaked) {
				t.Errorf("the refusal leaks git's text %q", leaked)
			}
		}
		if got := testhelpers.MustGit(t, s.wt, "rev-parse", "HEAD"); got != s.head {
			t.Error("the worktree HEAD moved")
		}
		s.requireUntouched(t, before)
	})

	t.Run("E11_an_unreadable_range_keeps_its_own_message", func(t *testing.T) {
		t.Parallel()
		s := newRecordScenario(t, fixture.Dir, recordScenarioOptions{
			rolePair: "epic-planning-pair", status: "EPIC_PLANNING", agentRole: "epic-planner", taskType: models.TaskTypePlanning, cases: []string{"c14"},
		})
		missing := strings.Repeat("ab", 20)
		testhelpers.AssertNoError(t, s.bb.Modify(func(state *models.State) error {
			state.FindTask(s.taskID).BaseCommit = &missing
			return nil
		}))
		before := s.task(t)
		_, err := s.submit()
		operational := asOperational(t, err)
		if operational.Code != operationalCodeGitOperation || operational.Phase != "check-prevention-records" {
			t.Errorf("code %q, phase %q", operational.Code, operational.Phase)
		}
		if operational.Message != rangeMessage || operational.Message == branchMessage {
			t.Errorf("message = %q, want %q", operational.Message, rangeMessage)
		}
		if got := sortedMapKeys(operational.Details); !reflect.DeepEqual(got, wantOperationalKeys) {
			t.Errorf("details keys = %v, want %v", got, wantOperationalKeys)
		}
		if hint, _ := operational.Details["recovery_hint"].(string); !strings.Contains(hint, "history can be read") {
			t.Error("recovery_hint is not the range-read hint")
		}
		if got := testhelpers.MustGit(t, s.wt, "rev-parse", "HEAD"); got != s.head {
			t.Error("the worktree HEAD moved")
		}
		s.requireUntouched(t, before)
	})

	t.Run("E12_local_check_and_gate_report_the_branch_cause_alike", func(t *testing.T) {
		t.Parallel()
		s := unresolvableBranch(t)
		statePath := paths.New(s.root).StatePath()
		stateBefore, err := os.ReadFile(statePath)
		testhelpers.AssertNoError(t, err)

		_, localErr := CheckSubmission(s.root, CheckSubmissionInput{TaskID: s.taskID, AgentID: s.agentID})
		local := asOperational(t, localErr)
		if got := testhelpers.MustGit(t, s.wt, "rev-parse", "HEAD"); got != s.head {
			t.Error("the local check moved the worktree HEAD")
		}
		if stateAfter, err := os.ReadFile(statePath); err != nil || string(stateAfter) != string(stateBefore) {
			t.Error("the local check changed state.yaml")
		}

		_, gateErr := s.submit()
		gate := asOperational(t, gateErr)
		if local.Code != gate.Code || local.Phase != gate.Phase || local.Message != gate.Message || local.Message != branchMessage {
			t.Errorf("local (%q, %q, %q) and gate (%q, %q, %q) differ", local.Code, local.Phase, local.Message, gate.Code, gate.Phase, gate.Message)
		}
		if !reflect.DeepEqual(sortedMapKeys(local.Details), sortedMapKeys(gate.Details)) {
			t.Errorf("detail keys differ: local %v, gate %v", sortedMapKeys(local.Details), sortedMapKeys(gate.Details))
		}
	})

	t.Run("E10_details_hold_names_only", func(t *testing.T) {
		t.Parallel()
		_, refusal, _ := refused(t, []string{"c09", "c10"})
		wantKeys := []string{"fix_commits_checked", "offending_commit_count", "offending_commits", "prevention_record_document"}
		if got := sortedMapKeys(refusal.Details); !reflect.DeepEqual(got, wantKeys) {
			t.Errorf("details keys = %v, want %v", got, wantKeys)
		}
		if document, _ := refusal.Details["prevention_record_document"].(string); !strings.HasPrefix(document, "~/"+paths.GlobalDirName()+"/") {
			t.Errorf("document = %q", document)
		}
		if refusal.Details["fix_commits_checked"] != 2 || refusal.Details["offending_commit_count"] != 2 {
			t.Errorf("counts = %v, %v", refusal.Details["fix_commits_checked"], refusal.Details["offending_commit_count"])
		}
		hex := regexp.MustCompile(`^[0-9a-f]{40}$`)
		for _, commit := range offendingCommits(t, refusal) {
			if got := sortedMapKeys(commit); !reflect.DeepEqual(got, []string{"problems", "sha", "subject"}) {
				t.Errorf("commit keys = %v", got)
			}
			if !hex.MatchString(commit["sha"].(string)) {
				t.Errorf("sha = %v", commit["sha"])
			}
			for _, problem := range problemsOf(t, commit) {
				if got := sortedMapKeys(problem); !reflect.DeepEqual(got, []string{"key", "problem"}) {
					t.Errorf("problem keys = %v", got)
				}
			}
		}
	})
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestSubmitForReview_PreventionRecordRefusalBounds(t *testing.T) {
	t.Parallel()
	offenders := func(n int, subject string) preventionrecord.Result {
		result := preventionrecord.Result{Checked: n + 3}
		for i := 0; i < n; i++ {
			result.Offenders = append(result.Offenders, preventionrecord.Offender{
				SHA:      fmt.Sprintf("%040d", i),
				Subject:  subject,
				Findings: []preventionrecord.Finding{{Problem: preventionrecord.RecordMissing}},
			})
		}
		return result
	}

	eleven := preventionRecordRefusal(offenders(11, "fix: a"), "task-1", "/wt", "task/task-1")
	if !strings.Contains(eleven.Reason, "and 1 more") || strings.Count(eleven.Reason, "record_missing;")+strings.Count(eleven.Reason, "record_missing.") != 10 {
		t.Errorf("11 offenders: reason %q", eleven.Reason)
	}
	if got := len(eleven.Details["offending_commits"].([]map[string]any)); got != 11 {
		t.Errorf("11 offenders: details hold %d", got)
	}

	ten := preventionRecordRefusal(offenders(10, "fix: a"), "task-1", "/wt", "task/task-1")
	if strings.Contains(ten.Reason, "more") {
		t.Errorf("10 offenders must not say 'and N more': %q", ten.Reason)
	}

	fiftyOne := preventionRecordRefusal(offenders(51, "fix: a"), "task-1", "/wt", "task/task-1")
	if got := len(fiftyOne.Details["offending_commits"].([]map[string]any)); got != 50 {
		t.Errorf("51 offenders: details hold %d, want 50", got)
	}
	if fiftyOne.Details["offending_commit_count"] != 51 || !strings.Contains(fiftyOne.Reason, "and 41 more") {
		t.Errorf("51 offenders: count %v, reason %q", fiftyOne.Details["offending_commit_count"], fiftyOne.Reason)
	}

	subject := "fix: " + strings.Repeat("é", 96)
	if len([]rune(subject)) != 101 {
		t.Fatalf("subject has %d characters", len([]rune(subject)))
	}
	long := preventionRecordRefusal(offenders(1, subject), "task-1", "/wt", "task/task-1")
	if !strings.Contains(long.Reason, `"`+string([]rune(subject)[:100])+`"`) || strings.Contains(long.Reason, subject) {
		t.Errorf("a 101-character subject must be cut to 100 in the reason: %q", long.Reason)
	}
	if got := long.Details["offending_commits"].([]map[string]any)[0]["subject"]; got != subject {
		t.Errorf("details keep the full subject, got %v", got)
	}
}
