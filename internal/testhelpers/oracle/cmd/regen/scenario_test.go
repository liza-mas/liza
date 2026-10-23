package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// These repositories are created through the recording workflow, not through
// synthetic history responses. All Git identities and configuration are fixtures.
type scenario struct {
	t     *testing.T
	dir   string
	env   []string
	start string
}

func newScenario(t *testing.T) *scenario {
	t.Helper()
	s := &scenario{t: t, dir: t.TempDir()}
	var err error
	s.env, err = oracle.ChildEnv(oracle.EnvBase{Home: t.TempDir(), Path: os.Getenv("PATH"), Tmp: t.TempDir()},
		"GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Oracle Test", "GIT_AUTHOR_EMAIL=oracle@example.invalid",
		"GIT_COMMITTER_NAME=Oracle Test", "GIT_COMMITTER_EMAIL=oracle@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	s.git("init", "-q", "-b", "main")
	s.git("config", "commit.gpgsign", "false")
	s.put("source.txt", "old")
	s.start = s.commit("starting production")
	s.put(ledgerPath, "start "+s.start+"\n")
	s.put("internal/oracle/demo/marker.txt", "a")
	s.put(sampleGolden, "old/a\n")
	s.commit("add oracle")
	return s
}

func (s *scenario) put(name, value string) {
	s.t.Helper()
	p := filepath.Join(s.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(value), 0644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *scenario) git(args ...string) string {
	s.t.Helper()
	c := exec.Command("git", append([]string{"-C", s.dir}, args...)...)
	c.Env = s.env
	b, err := c.CombinedOutput()
	if err != nil {
		s.t.Fatalf("fixture git %s: %v\n%s", args[0], err, b)
	}
	return strings.TrimSpace(string(b))
}

func (s *scenario) commit(message string) string {
	s.t.Helper()
	s.git("add", "--", ".")
	s.git("commit", "-q", "-m", message)
	return s.git("rev-parse", "HEAD")
}

func (s *scenario) record(reason string) {
	s.t.Helper()
	p := filepath.Join(s.dir, ledgerPath)
	b, err := os.ReadFile(p)
	if err != nil {
		s.t.Fatal(err)
	}
	s.put(ledgerPath, string(b)+"changed "+sampleGolden+" "+reason+"\n")
}

func (s *scenario) firstChange() string {
	s.git("checkout", "-q", "-b", "change-one")
	s.put("source.txt", "new")
	s.put(sampleGolden, "new/a\n")
	s.record("C1")
	sha := s.commit("C1 with its record")
	s.git("checkout", "-q", "main")
	s.git("merge", "-q", "--no-ff", "change-one", "-m", "merge C1")
	return sha
}

func fakeRegen(_ context.Context, dir, _ string, _ bool, _ []string) (map[string][]byte, error) {
	source, err := os.ReadFile(filepath.Join(dir, "source.txt"))
	if err != nil {
		return nil, err
	}
	marker, err := os.ReadFile(filepath.Join(dir, "internal/oracle/demo/marker.txt"))
	if err != nil {
		return nil, err
	}
	return map[string][]byte{sampleGolden: []byte(string(source) + "/" + string(marker) + "\n")}, nil
}

func (s *scenario) check(want int, fragments ...string) string {
	s.t.Helper()
	return s.run(fakeRegen, want, nil, fragments...)
}

func (s *scenario) run(reg regenerator, want int, args []string, fragments ...string) string {
	s.t.Helper()
	refs := s.git("for-each-ref", "--format=%(refname) %(objectname)")
	var roots []string
	wrapped := func(ctx context.Context, dir, sha string, baseline bool, pkgs []string) (map[string][]byte, error) {
		roots = append(roots, filepath.Dir(dir))
		return reg(ctx, dir, sha, baseline, pkgs)
	}
	var out, stderr bytes.Buffer
	code := run(context.Background(), append([]string{"-repo", s.dir}, args...), &out, &stderr, wrapped)
	report := out.String() + stderr.String()
	if code != want {
		s.t.Fatalf("exit %d, want %d:\n%s", code, want, report)
	}
	for _, f := range fragments {
		if !strings.Contains(report, f) {
			s.t.Fatalf("missing %q in report:\n%s", f, report)
		}
	}
	if got := s.git("worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		s.t.Fatalf("temporary worktree leaked: %s", got)
	}
	if s.git("for-each-ref", "--format=%(refname) %(objectname)") != refs {
		s.t.Fatal("refs changed")
	}
	for _, root := range roots {
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			s.t.Fatal("temporary root remains")
		}
	}
	return report
}

func TestRegenHistoryChains(t *testing.T) {
	t.Run("unchanged H1", func(t *testing.T) { newScenario(t).check(0, "ok "+sampleGolden+" H1") })
	for _, reason := range []string{"C2", "determinism"} {
		t.Run("two links "+reason, func(t *testing.T) {
			s := newScenario(t)
			c1 := s.firstChange()
			s.check(0, "chain pre ", "C1 "+c1)
			s.git("checkout", "-q", "-b", "change-two")
			if reason == "C2" {
				s.put("source.txt", "newer")
				s.put(sampleGolden, "newer/a\n")
			} else {
				s.put("internal/oracle/demo/marker.txt", "b")
				s.put(sampleGolden, "new/b\n")
			}
			s.record(reason)
			s.commit("second change with record")
			s.git("checkout", "-q", "main")
			s.put("unrelated.txt", "main progressed")
			s.commit("advance main")
			s.git("checkout", "-q", "change-two")
			s.git("rebase", "main")
			c2 := s.git("rev-parse", "HEAD")
			s.git("checkout", "-q", "main")
			s.git("merge", "-q", "--no-ff", "change-two", "-m", "merge second")
			s.check(0, "C1 "+c1, reason+" "+c2)
		})
	}
}

func TestRegenRejectsUnexplained(t *testing.T) {
	for _, tt := range []struct {
		name, rule string
		alter      func(*scenario)
	}{
		{"committed without record", "change without record", func(s *scenario) { s.put(sampleGolden, "unrecorded\n"); s.commit("unrecorded") }},
		{"uncommitted", "working tree differs", func(s *scenario) { s.put(sampleGolden, "uncommitted\n") }},
		{"record without change", "record without change", func(s *scenario) { s.record("C2"); s.commit("empty record") }},
		{"unreproduced point", "change point reproduction", func(s *scenario) { s.put(sampleGolden, "wrong\n"); s.record("C2"); s.commit("wrong blob") }},
		{"prechange", "pre-change reproduction", func(s *scenario) {
			// This edits an earlier oracle marker, so only the first-parent
			// baseline link fails; both change points still reproduce.
			s.git("checkout", "-q", "-b", "bad-pre", s.start)
			s.put(ledgerPath, "start "+s.start+"\n")
			s.put("internal/oracle/demo/marker.txt", "a")
			s.put(sampleGolden, "bad-pre\n")
			s.commit("unreproducible original")
			s.put("source.txt", "new")
			s.put(sampleGolden, "new/a\n")
			s.record("C1")
			s.commit("C1")
		}},
		{"merge changes golden", "merge changes golden", func(s *scenario) {
			s.git("checkout", "-q", "-b", "side")
			s.put("side.txt", "side")
			s.commit("side")
			s.git("checkout", "-q", "main")
			s.put("main.txt", "main")
			s.commit("main")
			s.git("merge", "--no-ff", "--no-commit", "side")
			s.put(sampleGolden, "merge\n")
			s.record("C2")
			s.commit("merge changes golden")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newScenario(t)
			s.firstChange()
			tt.alter(s)
			s.check(1, "unexplained "+sampleGolden, tt.rule)
		})
	}
}

func TestRegenMissingExtraAndWriteNew(t *testing.T) {
	s := newScenario(t)
	missing := "internal/oracle/demo/testdata/golden/json/new.json"
	extra := "internal/oracle/demo/testdata/golden/artifact/extra.txt"
	s.put(extra, "extra")
	reg := func(context.Context, string, string, bool, []string) (map[string][]byte, error) {
		return map[string][]byte{sampleGolden: []byte("different"), missing: []byte("new")}, nil
	}
	s.run(reg, 1, nil, "missing "+missing, "extra "+extra, "unexplained "+sampleGolden)
	s.run(reg, 0, []string{"-mode", "write-new"}, "wrote "+missing)
	b, err := os.ReadFile(filepath.Join(s.dir, sampleGolden))
	if err != nil || string(b) != "old/a\n" {
		t.Fatal("write-new overwrote an existing golden")
	}
	b, err = os.ReadFile(filepath.Join(s.dir, missing))
	if err != nil || string(b) != "new" {
		t.Fatal("write-new did not copy missing bytes")
	}
}

func TestRegenBaselineFixAndHistoricalOverlay(t *testing.T) {
	s := newScenario(t)
	s.put("source.txt", "fixed")
	s.put("internal/oracle/demo/marker.txt", "ignored-fix-oracle")
	fix := s.commit("urgent fix including oracle")
	s.put("internal/oracle/demo/marker.txt", "a")
	s.put(sampleGolden, "fixed/a\n")
	s.put(ledgerPath, "start "+s.start+"\nfix "+fix+"\n")
	s.commit("absorb fix")
	pre := s.git("rev-parse", "HEAD")
	c1 := s.firstChange()
	s.put("internal/oracle/demo/marker.txt", "b")
	s.put(sampleGolden, "new/b\n")
	s.record("determinism")
	c2 := s.commit("normaliser edit")
	seen := map[string]bool{}
	reg := func(ctx context.Context, dir, sha string, baseline bool, pkgs []string) (map[string][]byte, error) {
		seen[sha+"/"+strings.ToLower(boolText(baseline))] = true
		return fakeRegen(ctx, dir, sha, baseline, pkgs)
	}
	s.run(reg, 0, nil, "chain pre "+pre, "C1 "+c1, "determinism "+c2)
	for _, key := range []string{pre + "/true", c1 + "/false", c2 + "/false"} {
		if !seen[key] {
			t.Fatalf("tree kind/commit not regenerated: %s", key)
		}
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestRegenCleanupOnRegeneratorError(t *testing.T) {
	s := newScenario(t)
	s.run(func(context.Context, string, string, bool, []string) (map[string][]byte, error) {
		return nil, errors.New("fixture regeneration failed")
	}, 2, nil, "fixture regeneration failed")
}

func TestRegenLedgerIsAppendOnly(t *testing.T) {
	s := newScenario(t)
	s.firstChange()
	s.put(ledgerPath, "start "+s.start+"\n")
	s.commit("remove a provenance record")
	s.run(fakeRegen, 2, nil, "ledger is not append-only")
}

func TestRegenRecordWithoutChangeBeforeC(t *testing.T) {
	s := newScenario(t)
	s.record("determinism")
	s.commit("record without change on non-chained golden")
	s.check(1, "unexplained "+sampleGolden, "record without change")
}

func TestRegenRejectsTwoRecordsForOneChange(t *testing.T) {
	s := newScenario(t)
	s.firstChange()
	s.put("source.txt", "newer")
	s.put(sampleGolden, "newer/a\n")
	s.record("C2")
	s.record("determinism")
	s.commit("two records for one blob")
	s.check(1, "unexplained "+sampleGolden, "change has multiple records")
}

func TestRegenCancellationCleansWorktrees(t *testing.T) {
	s := newScenario(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var root string
	reg := func(context.Context, string, string, bool, []string) (map[string][]byte, error) {
		cancel()
		return nil, ctx.Err()
	}
	wrapped := func(ctx context.Context, dir, sha string, baseline bool, pkgs []string) (map[string][]byte, error) {
		root = filepath.Dir(dir)
		return reg(ctx, dir, sha, baseline, pkgs)
	}
	var out bytes.Buffer
	if code := run(ctx, []string{"-repo", s.dir}, &out, &out, wrapped); code != 2 || !strings.Contains(out.String(), "context canceled") {
		t.Fatalf("cancelled regeneration exit %d: %s", code, &out)
	}
	if strings.Count(s.git("worktree", "list", "--porcelain"), "worktree ") != 1 {
		t.Fatal("cancelled worktree leaked")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled temporary root leaked")
	}
}

func TestRegenPackagePatterns(t *testing.T) {
	golden := "internal/oracle/a3/control/testdata/golden/human/x.txt"
	for _, pattern := range []string{"./internal/oracle/...", "./internal/oracle/.../control", "./internal/oracle/a.../control", "./internal/oracle/a3/control/...", "./internal/oracle/a3/control"} {
		if !selected(golden, []string{pattern}) {
			t.Errorf("package not selected by %s", pattern)
		}
	}
	if selected(golden, []string{"./internal/oracle/a3"}) {
		t.Fatal("exact package selected a child")
	}
}

func TestRegenPackageFilter(t *testing.T) {
	s := newScenario(t)
	s.put("internal/oracle/other/testdata/golden/human/other.txt", "not selected")
	s.run(fakeRegen, 0, []string{"-pkg", "./internal/oracle/demo/..."}, "ok "+sampleGolden+" H1")
}
