package preventionrecord

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/liza-mas/liza/internal/preventionrecord/recordfixture"
)

// TestMain pins the process environment git inherits: no user or system
// configuration, no terminal prompt.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "preventionrecord-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create a git home")
		os.Exit(2)
	}
	for _, kv := range recordfixture.Environ(home) {
		name, value, _ := strings.Cut(kv, "=")
		os.Setenv(name, value)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

var (
	fixtureOnce sync.Once
	fixtureRepo recordfixture.Repo
	fixtureErr  error
	fixtureRoot string
)

// builtFixture builds the I3 repository once for the package.
func builtFixture(t *testing.T) recordfixture.Repo {
	t.Helper()
	fixtureOnce.Do(func() {
		fixtureRoot, fixtureErr = os.MkdirTemp("", "preventionrecord-fixture-")
		if fixtureErr == nil {
			fixtureRepo, fixtureErr = recordfixture.Build(filepath.Join(fixtureRoot, "repo"))
		}
	})
	if fixtureErr != nil {
		t.Fatalf("build the fixture: %v", fixtureErr)
	}
	return fixtureRepo
}

func git(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = os.Environ()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", args[0], err, out)
	}
	return strings.TrimSpace(string(out))
}

// caseResult checks one case of the fixture as its own one-commit range.
func caseResult(t *testing.T, id string) Result {
	t.Helper()
	repo := builtFixture(t)
	result, err := Check(repo.Dir, "case/"+id+"^", "case/"+id, "case/"+id+"^")
	if err != nil {
		t.Fatalf("case %s: %v", id, err)
	}
	return result
}

func offenderFindings(t *testing.T, result Result) []Finding {
	t.Helper()
	if len(result.Offenders) != 1 {
		t.Fatalf("offenders = %+v, want exactly one", result.Offenders)
	}
	return result.Offenders[0].Findings
}

func TestRangeFlagsEachBrokenCase(t *testing.T) {
	tests := []struct {
		id   string
		want []Finding
	}{
		{"c08", []Finding{f(DispositionAccepted, KeyDisposition)}},
		{"c09", []Finding{f(RecordMissing, "")}},
		{"c10", []Finding{f(ValueInvalid, KeyDisposition)}},
		{"c11", []Finding{f(KeyMissing, KeyClass)}},
		{"c12", []Finding{f(OwnerUnexpected, KeyOwner)}},
		{"c13", []Finding{f(OutsideFinalBlock, "")}},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := caseResult(t, tt.id)
			if result.Checked != 1 {
				t.Errorf("checked = %d, want 1", result.Checked)
			}
			if got := offenderFindings(t, result); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findings = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRangePassesValidAndUncheckedCases(t *testing.T) {
	for _, id := range []string{"c02", "c03", "c04", "c05", "c06", "c07", "c14", "c17", "c18"} {
		result := caseResult(t, id)
		if result.Checked != 1 || len(result.Offenders) != 0 {
			t.Errorf("%s: %+v, want one fix commit checked and no offender", id, result)
		}
	}
	// A revert and non-fix commits carry records or look like fixes, and are not checked.
	for _, id := range []string{"c15", "c16"} {
		result := caseResult(t, id)
		if result.Checked != 0 || len(result.Offenders) != 0 {
			t.Errorf("%s: %+v, want nothing checked", id, result)
		}
	}
}

// c01 is the fixture's root commit: it has no parent to range from, so it is
// replayed onto a repository of its own.
func TestRangeLeavesTheRootNonFixCommitUnchecked(t *testing.T) {
	repo := builtFixture(t)
	dir := t.TempDir()
	git(t, dir, "", "init", "--quiet", "--initial-branch=main")
	git(t, dir, "", "-c", "user.name=T", "-c", "user.email=t@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "initial")
	base := git(t, dir, "", "rev-parse", "HEAD")
	git(t, dir, "", "fetch", "--quiet", repo.Dir, "refs/tags/case/c01:refs/tags/case/c01")
	git(t, dir, "", "-c", "user.name=T", "-c", "user.email=t@example.invalid", "cherry-pick", "--allow-empty", "case/c01")
	result, err := Check(dir, base, "HEAD", base)
	if err != nil {
		t.Fatal(err)
	}
	if result.Checked != 0 || len(result.Offenders) != 0 {
		t.Errorf("result = %+v", result)
	}
}

func TestRangeOverTheWholeFixtureFlagsExactlyC08ToC13(t *testing.T) {
	repo := builtFixture(t)
	result, err := Check(repo.Dir, "case/c01", "case/c18", "case/c01")
	if err != nil {
		t.Fatal(err)
	}
	if result.Checked != 15 {
		t.Errorf("checked = %d, want 15 (c02-c18 less c15, c16)", result.Checked)
	}
	var got []string
	for _, offender := range result.Offenders {
		for _, c := range repo.Cases {
			if c.SHA == offender.SHA {
				got = append(got, c.ID)
			}
		}
	}
	if want := []string{"c08", "c09", "c10", "c11", "c12", "c13"}; !reflect.DeepEqual(got, want) {
		t.Errorf("offenders = %v, want %v in commit order", got, want)
	}
}

// tempRepo is a repository with an initial commit.
type tempRepo struct {
	dir  string
	base string
}

func newTempRepo(t *testing.T) tempRepo {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "", "init", "--quiet", "--initial-branch=main")
	git(t, dir, "", "config", "user.name", "T")
	git(t, dir, "", "config", "user.email", "t@example.invalid")
	git(t, dir, "", "commit", "--quiet", "--allow-empty", "-m", "chore: initial")
	return tempRepo{dir: dir, base: git(t, dir, "", "rev-parse", "HEAD")}
}

func (r tempRepo) commit(t *testing.T, message string) string {
	t.Helper()
	git(t, r.dir, message, "commit", "--quiet", "--allow-empty", "--cleanup=verbatim", "-F", "-")
	return git(t, r.dir, "", "rev-parse", "HEAD")
}

func (r tempRepo) check(t *testing.T) Result {
	t.Helper()
	result, err := Check(r.dir, r.base, "HEAD", r.base)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRangeReadsRawLinesNotGitsParsedForm(t *testing.T) {
	cause := "Prevention-Cause: a closed store accepted writes\n"
	rest := "Prevention-Concept: T0.2\nPrevention-Disposition: removed\nPrevention-Class: gap\n"
	tests := []struct {
		name    string
		message string
		want    []Finding
	}{
		{"folded value", "fix: x\n\nBody.\n\nPrevention-Cause: unchecked\n assumption\n" + rest, []Finding{f(ValueInvalid, KeyCause)}},
		{"stale record above the final block", "fix: x\n\nPrevention-Old_Key: stale\n\nBody.\n\n" + cause + rest, []Finding{f(OutsideFinalBlock, "")}},
		{"unknown key inside a block git keeps", "fix: x\n\nBody.\n\n" + cause + rest + "Prevention-Old_Key: stale\nSigned-off-by: A <a@example.invalid>\n", []Finding{f(KeyUnknown, "Prevention-Old_Key")}},
		{"blank before the colon", "fix: x\n\nBody.\n\n" + cause + "Prevention-Concept: T0.2\nPrevention-Disposition: removed\nPrevention-Class : gap\n", nil},
		{"record above a co-author paragraph", "fix: x\n\n" + cause + rest + "\nCo-Authored-By: A <a@example.invalid>\n", []Finding{f(OutsideFinalBlock, "")}},
		{"record sharing the paragraph with a co-author", "fix: x\n\nBody.\n\n" + cause + rest + "Co-Authored-By: A <a@example.invalid>\n", nil},
		{"subject only", "fix: x\n", []Finding{f(RecordMissing, "")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTempRepo(t)
			r.commit(t, tt.message)
			result := r.check(t)
			if result.Checked != 1 {
				t.Fatalf("checked = %d, want 1", result.Checked)
			}
			if tt.want == nil {
				if len(result.Offenders) != 0 {
					t.Errorf("offenders = %+v, want none", result.Offenders)
				}
				return
			}
			if got := offenderFindings(t, result); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findings = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRangeLeavesCommitsTheRebaseSkipsAndMergesOut(t *testing.T) {
	for _, merged := range []bool{true, false} {
		t.Run(fmt.Sprintf("integration_merged=%t", merged), func(t *testing.T) {
			r := newTempRepo(t)
			git(t, r.dir, "", "checkout", "--quiet", "-b", "task")
			// A record-less fix the integration branch also carries as the same change.
			if err := os.WriteFile(filepath.Join(r.dir, "shared.txt"), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, r.dir, "", "add", "shared.txt")
			// Distinct committer dates give the cherry-pick its own SHA: in the
			// same second git rebuilds the identical commit, which integration
			// then holds by reachability and the patch identity goes untested.
			t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")
			git(t, r.dir, "fix: shared change\n", "commit", "--quiet", "-F", "-")
			git(t, r.dir, "", "checkout", "--quiet", "main")
			t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:01Z")
			git(t, r.dir, "", "cherry-pick", "task")
			if git(t, r.dir, "", "rev-parse", "HEAD") == git(t, r.dir, "", "rev-parse", "task") {
				t.Fatal("the cherry-pick rebuilt the task commit; the fixture proves nothing")
			}
			// A record-less fix only integration holds, merged into the task branch.
			if err := os.WriteFile(filepath.Join(r.dir, "human.txt"), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, r.dir, "", "add", "human.txt")
			git(t, r.dir, "fix: a human's change\n", "commit", "--quiet", "-F", "-")
			integration := git(t, r.dir, "", "rev-parse", "HEAD")
			git(t, r.dir, "", "checkout", "--quiet", "task")
			// A record-less fix only the task holds stays checked.
			if err := os.WriteFile(filepath.Join(r.dir, "task.txt"), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, r.dir, "", "add", "task.txt")
			git(t, r.dir, "fix: task only\n", "commit", "--quiet", "-F", "-")
			if merged {
				git(t, r.dir, "", "merge", "--quiet", "--no-ff", "-m", "Merge main", "main")
			}
			head := git(t, r.dir, "", "rev-parse", "HEAD")

			result, err := Check(r.dir, integration, head, r.base)
			if err != nil {
				t.Fatal(err)
			}
			if result.Checked != 1 || len(result.Offenders) != 1 || result.Offenders[0].Subject != "fix: task only" {
				t.Errorf("result = %+v, want only the task's own fix checked", result)
			}
		})
	}
}

func TestRangeReportsAGitFailureWithItsText(t *testing.T) {
	r := newTempRepo(t)
	_, err := Check(r.dir, "no-such-branch", "HEAD", r.base)
	if err == nil {
		t.Fatal("want an error for an unresolvable revision")
	}
	if !strings.Contains(err.Error(), "no-such-branch") {
		t.Errorf("error %q does not carry git's text", err)
	}
}

// trailerPairs reads key and value pairs out of lines as written.
func trailerPairs(lines []string) [][2]string {
	var pairs [][2]string
	for _, line := range lines {
		if !strings.HasPrefix(line, "Prevention-") {
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		pairs = append(pairs, [2]string{strings.TrimRight(key, " \t"), strings.Trim(value, " \t")})
	}
	return pairs
}

// An accepted record is what the period query counts: its entries equal the
// Prevention- trailers git parses from the commit.
func TestRangeAcceptedEntriesEqualGitsParsedTrailers(t *testing.T) {
	cause := "Prevention-Cause: a closed store accepted writes\n"
	rest := "Prevention-Concept: T0.2\nPrevention-Disposition: removed\nPrevention-Class: gap\n"

	repo := builtFixture(t)
	type subject struct {
		name, dir, sha string
		commit         Commit
	}
	var subjects []subject
	whole, err := ReadRange(repo.Dir, "case/c01", "case/c18", "case/c01")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range whole {
		subjects = append(subjects, subject{name: c.SHA[:12], dir: repo.Dir, sha: c.SHA, commit: c})
	}
	for name, message := range map[string]string{
		"blank before colon": "fix: x\n\nBody.\n\n" + cause + "Prevention-Concept: T0.2\nPrevention-Disposition: removed\nPrevention-Class : gap\n",
		"co-author":          "fix: x\n\nBody.\n\n" + cause + rest + "Co-Authored-By: A <a@example.invalid>\n",
		"padded":             "fix: x\n\nBody.\n\n" + cause + "Prevention-Concept:\tT0.2\nPrevention-Disposition:   removed  \nPrevention-Class: gap\n",
	} {
		r := newTempRepo(t)
		r.commit(t, message)
		commits, err := ReadRange(r.dir, r.base, "HEAD", r.base)
		if err != nil || len(commits) != 1 {
			t.Fatalf("%s: %v, %d commits", name, err, len(commits))
		}
		subjects = append(subjects, subject{name: name, dir: r.dir, sha: commits[0].SHA, commit: commits[0]})
	}

	compared := 0
	for _, s := range subjects {
		if !IsFixSubject(s.commit.Subject) || len(Validate(s.commit.Message, s.commit.Trailers)) != 0 {
			continue
		}
		out := git(t, s.dir, "", "log", "-1", "--format=%(trailers:only,unfold)", s.sha)
		want := trailerPairs(strings.Split(out, "\n"))
		var got [][2]string
		for _, e := range readBlock(s.commit.Trailers) {
			got = append(got, [2]string{e.key, e.value})
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: entries %v, git trailers %v", s.name, got, want)
		}
		compared++
	}
	if compared < 12 {
		t.Errorf("compared %d accepted records, want at least 12", compared)
	}
}

// The published-range check refuses a whitespace-bearing concept or owner that
// is otherwise a valid record.
func TestRangeRejectsWhitespaceInTokenValues(t *testing.T) {
	for _, key := range []string{KeyConcept, KeyOwner} {
		t.Run(key, func(t *testing.T) {
			block := valid
			if key == KeyConcept {
				block = strings.Replace(block, "T0.2", "internal/\vstore.go", 1)
			} else {
				block = strings.Replace(block, "removed", "scheduled", 1) + "Prevention-Owner: TASK-\v42\n"
			}
			repo := newTempRepo(t)
			repo.commit(t, message(block))
			result := repo.check(t)
			if result.Checked != 1 || len(result.Offenders) != 1 {
				t.Fatalf("checked=%d offenders=%d; want 1 and 1", result.Checked, len(result.Offenders))
			}
			if got, want := result.Offenders[0].Findings, []Finding{f(ValueInvalid, key)}; !reflect.DeepEqual(got, want) {
				t.Errorf("findings=%v; want %v", got, want)
			}
		})
	}
}
