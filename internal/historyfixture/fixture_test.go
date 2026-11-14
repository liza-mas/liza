package historyfixture

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const thirtyDays = 30 * 86400

// runGit runs git in dir with the fixture environment and returns trimmed
// stdout. Failures report the subcommand and git's output, never the
// environment.
func runGit(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = Environ(t.TempDir())
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", args[0], err, out)
	}
	return strings.TrimSpace(string(out))
}

func buildInto(t *testing.T) History {
	t.Helper()
	h, err := Build(filepath.Join(t.TempDir(), "history"))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestBuildDeterministic(t *testing.T) {
	a, b := buildInto(t), buildInto(t)
	if len(a.Cases) != 16 || len(a.Cases) != len(b.Cases) {
		t.Fatalf("cases: %d and %d", len(a.Cases), len(b.Cases))
	}
	for i := range a.Cases {
		if len(a.Cases[i].SHA) != 40 || a.Cases[i].SHA != b.Cases[i].SHA {
			t.Errorf("case %s: SHAs %q and %q differ", a.Cases[i].ID, a.Cases[i].SHA, b.Cases[i].SHA)
		}
	}
	if a.Upstream.Head != b.Upstream.Head || a.Downstream.Head != b.Downstream.Head {
		t.Errorf("heads differ: upstream %q and %q, downstream %q and %q",
			a.Upstream.Head, b.Upstream.Head, a.Downstream.Head, b.Downstream.Head)
	}
	for _, r := range []Repository{a.Upstream, a.Downstream} {
		if got := runGit(t, r.Dir, "", "rev-parse", "HEAD"); got != r.Head {
			t.Errorf("head %q, want %q", got, r.Head)
		}
	}
}

// hunk is a run of pre-image lines of one file.
type hunk struct {
	path       string
	start, end int
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+`)

// preImage returns the pre-image line ranges of the commit's -M -U0 hunks. A
// pure insertion after line a (a >= 1) counts line a; one at line 0 and new
// files have no pre-image line.
func preImage(t *testing.T, dir, sha string) []hunk {
	t.Helper()
	var hunks []hunk
	path := ""
	for _, line := range strings.Split(runGit(t, dir, "", "show", "-M", "-U0", "--format=", sha), "\n") {
		switch {
		case strings.HasPrefix(line, "--- a/"):
			path = strings.TrimPrefix(line, "--- a/")
		case strings.HasPrefix(line, "--- "):
			path = ""
		}
		m := hunkHeader.FindStringSubmatch(line)
		if m == nil || path == "" {
			continue
		}
		a, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		switch {
		case n == 0 && a >= 1:
			hunks = append(hunks, hunk{path, a, a})
		case n > 0:
			hunks = append(hunks, hunk{path, a, a + n - 1})
		}
	}
	return hunks
}

var blameHeader = regexp.MustCompile(`^([0-9a-f]{40}) \d+ \d+`)

// blamed maps every commit that blame names for the commit's pre-image lines,
// on its first parent, to that commit's committer time.
func blamed(t *testing.T, dir, sha string) map[string]int64 {
	t.Helper()
	hunks := preImage(t, dir, sha)
	if len(hunks) == 0 {
		t.Fatalf("no pre-image lines for %s", sha)
	}
	times := map[string]int64{}
	for _, h := range hunks {
		current := ""
		out := runGit(t, dir, "", "blame", "--porcelain", "-L", strconv.Itoa(h.start)+","+strconv.Itoa(h.end), sha+"^1", "--", h.path)
		for _, line := range strings.Split(out, "\n") {
			if m := blameHeader.FindStringSubmatch(line); m != nil {
				current = m[1]
				if _, ok := times[current]; !ok {
					times[current] = 0
				}
			} else if v, ok := strings.CutPrefix(line, "committer-time "); ok && current != "" {
				times[current], _ = strconv.ParseInt(v, 10, 64)
			}
		}
	}
	return times
}

func newest(times map[string]int64) int64 {
	var max int64
	for _, v := range times {
		if v > max {
			max = v
		}
	}
	return max
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestCaseProperties(t *testing.T) {
	h := buildInto(t)
	dir := func(c Case) string { return h.Repo(c.Repo).Dir }
	byID := func(id string) Case {
		c, ok := h.Case(id)
		if !ok {
			t.Fatalf("no case %s", id)
		}
		return c
	}

	// Every case: subject type, dates and the table's repository.
	for _, c := range h.Cases {
		t.Run("table/"+c.ID, func(t *testing.T) {
			d := dir(c)
			if got := runGit(t, d, "", "cat-file", "-t", c.SHA); got != "commit" {
				t.Fatalf("%s is a %q object in %s", c.SHA, got, c.Repo)
			}
			subject := runGit(t, d, "", "log", "-1", "--format=%s", c.SHA)
			if subject != c.Subject {
				t.Errorf("subject %q, want %q", subject, c.Subject)
			}
			switch c.Type {
			case "merge":
				if !strings.HasPrefix(subject, "Merge branch ") {
					t.Errorf("subject %q is not a merge subject", subject)
				}
			case "revert":
				if !strings.HasPrefix(subject, `Revert "`) {
					t.Errorf("subject %q is not git's revert subject", subject)
				}
			default:
				if !strings.HasPrefix(subject, c.Type+": ") {
					t.Errorf("subject %q lacks type %q", subject, c.Type)
				}
			}
			dates := strings.Fields(runGit(t, d, "", "log", "-1", "--format=%at %ct", c.SHA))
			if want := strconv.FormatInt(c.Date.Unix(), 10); len(dates) != 2 || dates[1] != want {
				t.Errorf("committer date %v, want %s", dates, want)
			} else if c.Kind != KindCherryPick && dates[0] != want {
				t.Errorf("author date %s differs from committer date %s", dates[0], want)
			}
		})
	}

	// Blame targets: the repaired lines belong to the stated commit, and the
	// newest of them is young or old as the table says.
	blameCases := []struct {
		fix, target string
		young       bool
	}{
		{"u03", "u02", true},
		{"u04", "u03", true},
		{"u05", "u02", true},
		{"u06", "u01", false},
		{"u07", "u01", false},
		{"b02", "b01", true},
		{"d03", "d01", true},
	}
	for _, bc := range blameCases {
		t.Run("blame/"+bc.fix, func(t *testing.T) {
			fix, target := byID(bc.fix), byID(bc.target)
			got := blamed(t, dir(fix), fix.SHA)
			if len(got) != 1 {
				t.Fatalf("pre-image lines blame to %d commits, want only %s", len(got), bc.target)
			}
			if _, ok := got[target.SHA]; !ok {
				t.Fatalf("pre-image lines do not blame to %s", bc.target)
			}
			age := fix.Date.Unix() - newest(got)
			if bc.young && age > thirtyDays {
				t.Errorf("newest blamed line is %d days old, want at most 30", age/86400)
			}
			if !bc.young && age <= thirtyDays {
				t.Errorf("newest blamed line is %d days old, want more than 30", age/86400)
			}
		})
	}
	t.Run("blame/ages", func(t *testing.T) {
		u06, u01 := byID("u06"), byID("u01")
		if days := (u06.Date.Unix() - u01.Date.Unix()) / 86400; days != 55 {
			t.Errorf("u06 repairs code %d days old, want 55", days)
		}
	})

	t.Run("proof population touches only the test", func(t *testing.T) {
		u05 := byID("u05")
		if got := runGit(t, dir(u05), "", "diff-tree", "--no-commit-id", "--name-only", "-r", u05.SHA); got != testPath {
			t.Errorf("changed files %q, want only %s", got, testPath)
		}
		for _, id := range []string{"u03", "u04", "u06", "u07", "b02", "d03"} {
			c := byID(id)
			for _, f := range lines(runGit(t, dir(c), "", "diff-tree", "--no-commit-id", "--name-only", "-r", c.SHA)) {
				if strings.HasPrefix(f, "tests/") {
					t.Errorf("%s changes %s", id, f)
				}
			}
		}
	})

	t.Run("duplicate shares a patch-id and has no cherry-pick line", func(t *testing.T) {
		u03, d02 := byID("u03"), byID("d02")
		patchID := func(c Case) string {
			show := runGit(t, dir(c), "", "show", c.SHA)
			return strings.Fields(runGit(t, dir(c), show+"\n", "patch-id", "--stable"))[0]
		}
		if patchID(u03) != patchID(d02) {
			t.Error("u03 and d02 have different stable patch-ids")
		}
		if u03.SHA == d02.SHA {
			t.Error("u03 and d02 share a SHA")
		}
		if body := runGit(t, dir(d02), "", "log", "-1", "--format=%B", d02.SHA); strings.Contains(body, "cherry picked from") {
			t.Errorf("d02's message has a cherry-picked line: %q", body)
		}
		if got := runGit(t, dir(d02), "", "log", "-1", "--format=%at", d02.SHA); got != strconv.FormatInt(u03.Date.Unix(), 10) {
			t.Errorf("d02 author date %s is not u03's", got)
		}
	})

	t.Run("downstream is cloned after u02 and before u03", func(t *testing.T) {
		d01, u02 := byID("d01"), byID("u02")
		if got := runGit(t, dir(d01), "", "rev-parse", d01.SHA+"^"); got != u02.SHA {
			t.Errorf("d01's parent %s, want u02 %s", got, u02.SHA)
		}
		d02, u03 := byID("d02"), byID("u03")
		if got := runGit(t, dir(d02), "", "rev-parse", d02.SHA+"^"); got != d01.SHA {
			t.Errorf("d02's parent %s, want d01 %s", got, d01.SHA)
		}
		if got := runGit(t, dir(d02), "", "rev-list", "main"); contains(lines(got), u03.SHA) {
			t.Error("downstream's history contains u03 itself")
		}
	})

	t.Run("in-run correction enters through the merge", func(t *testing.T) {
		u08, u09, b01, b02 := byID("u08"), byID("u09"), byID("b01"), byID("b02")
		firstParent := lines(runGit(t, dir(u09), "", "rev-list", "--first-parent", "main"))
		if contains(firstParent, b02.SHA) || contains(firstParent, b01.SHA) {
			t.Error("b01 or b02 is on main's first-parent chain")
		}
		if !contains(firstParent, u09.SHA) {
			t.Error("the merge is not on main's first-parent chain")
		}
		if got := runGit(t, dir(u09), "", "rev-parse", u09.SHA+"^1", u09.SHA+"^2"); got != u08.SHA+"\n"+b02.SHA {
			t.Errorf("merge parents %q, want u08 then b02", got)
		}
		entered := lines(runGit(t, dir(u09), "", "rev-list", u09.SHA, "^"+u09.SHA+"^1"))
		want := []string{u09.SHA, b02.SHA, b01.SHA}
		if len(entered) != len(want) {
			t.Fatalf("merge brings in %d commits, want %d", len(entered), len(want))
		}
		for _, sha := range want {
			if !contains(entered, sha) {
				t.Errorf("%s does not enter through the merge", sha)
			}
		}
	})

	t.Run("misleading message", func(t *testing.T) {
		u07 := byID("u07")
		if msg := runGit(t, dir(u07), "", "log", "-1", "--format=%B", u07.SHA); !strings.Contains(msg, "Comment-only") {
			t.Errorf("message does not claim a comment-only change: %q", msg)
		}
		var removed, added []string
		for _, line := range lines(runGit(t, dir(u07), "", "show", "-U0", "--format=", u07.SHA)) {
			switch {
			case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
				removed = append(removed, line[1:])
			case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
				added = append(added, line[1:])
			}
		}
		code := func(ls []string, op string) bool {
			for _, l := range ls {
				if s := strings.TrimSpace(l); !strings.HasPrefix(s, "#") && strings.Contains(s, op) {
					return true
				}
			}
			return false
		}
		if !code(removed, "a_start < b_end") || !code(added, "a_start <= b_end") {
			t.Errorf("the diff does not change a comparison: -%q +%q", removed, added)
		}
	})

	t.Run("record", func(t *testing.T) {
		u06 := byID("u06")
		trailers := lines(runGit(t, dir(u06), "", "log", "-1", "--format=%(trailers:only,unfold)", u06.SHA))
		var keys []string
		values := map[string]string{}
		for _, l := range trailers {
			if l == "" {
				continue
			}
			k, v, _ := strings.Cut(l, ": ")
			keys = append(keys, k)
			values[k] = v
		}
		want := []string{"Prevention-Cause", "Prevention-Concept", "Prevention-Disposition", "Prevention-Owner", "Prevention-Class"}
		if strings.Join(keys, ",") != strings.Join(want, ",") {
			t.Fatalf("final trailer block keys %v, want %v", keys, want)
		}
		if values["Prevention-Cause"] == "" {
			t.Error("empty cause")
		}
		if ok, _ := regexp.MatchString(`^([A-Za-z][A-Za-z0-9._-]*|[^\s#]*/[^\s#]*(#[A-Za-z0-9_.]+)?)$`, values["Prevention-Concept"]); !ok {
			t.Errorf("concept %q has neither identifier form", values["Prevention-Concept"])
		}
		if values["Prevention-Disposition"] != "scheduled" || values["Prevention-Owner"] == "" {
			t.Errorf("disposition %q with owner %q", values["Prevention-Disposition"], values["Prevention-Owner"])
		}
		if !contains([]string{"gap", "unenforced", "not-consulted", "none-needed"}, values["Prevention-Class"]) {
			t.Errorf("class %q is outside the closed set", values["Prevention-Class"])
		}
		// The block is the message's final paragraph: the record is only read there.
		msg := runGit(t, dir(u06), "", "log", "-1", "--format=%B", u06.SHA)
		paragraphs := strings.Split(msg, "\n\n")
		if last := paragraphs[len(paragraphs)-1]; strings.Count(last, "Prevention-") != 5 {
			t.Errorf("final paragraph is not the record: %q", last)
		}
		for _, c := range h.Cases {
			if c.ID == "u06" {
				continue
			}
			if m := runGit(t, dir(c), "", "log", "-1", "--format=%B", c.SHA); strings.Contains(m, "Prevention-") {
				t.Errorf("%s carries a record", c.ID)
			}
		}
	})

	t.Run("thin message", func(t *testing.T) {
		d03 := byID("d03")
		if body := runGit(t, dir(d03), "", "log", "-1", "--format=%b", d03.SHA); body != "" {
			t.Errorf("d03 has a body: %q", body)
		}
	})

	t.Run("revert", func(t *testing.T) {
		u10, u11 := byID("u10"), byID("u11")
		body := runGit(t, dir(u11), "", "log", "-1", "--format=%b", u11.SHA)
		if !strings.Contains(body, "This reverts commit "+u10.SHA+".") {
			t.Errorf("revert body %q does not name u10", body)
		}
		if _, err := os.Stat(filepath.Join(dir(u11), "src", "cache_key.py")); !os.IsNotExist(err) {
			t.Errorf("u10's file survives its revert: %v", err)
		}
	})
}

func TestNoAnswersInHistory(t *testing.T) {
	h := buildInto(t)
	ids := make([]string, 0, len(h.Cases))
	for _, c := range h.Cases {
		ids = append(ids, c.ID)
	}
	token := regexp.MustCompile(`(?i)\b(` + strings.Join(ids, "|") + `)\b`)
	if !token.MatchString("see u03") {
		t.Fatal("the case-id pattern does not match a case id")
	}

	for _, r := range []Repository{h.Upstream, h.Downstream} {
		name := filepath.Base(r.Dir)
		if got := runGit(t, r.Dir, "", "tag", "--list"); got != "" {
			t.Errorf("%s has tags: %q", name, got)
		}
		if got := runGit(t, r.Dir, "", "for-each-ref", "--format=%(refname)"); got != "refs/heads/main" {
			t.Errorf("%s refs: %q, want only refs/heads/main", name, got)
		}
		// Messages, identities, paths (diff headers) and every added or removed
		// line, across all reachable commits.
		history := runGit(t, r.Dir, "", "log", "-p", "--format=%an %ae %cn %ce%n%B", Branch)
		if !strings.Contains(history, "src/") {
			t.Fatalf("%s: the history scan saw no content", name)
		}
		if m := token.FindString(history); m != "" {
			t.Errorf("%s: history names a case: %q", name, m)
		}
		if m := token.FindString(runGit(t, r.Dir, "", "ls-files")); m != "" {
			t.Errorf("%s: a path names a case: %q", name, m)
		}
	}
}

func TestBuildRefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(dir); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("err = %v, want a non-empty refusal", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory changed: %v, %d entries", err, len(entries))
	}

	if _, err := Build(t.TempDir()); err != nil {
		t.Fatalf("empty directory refused: %v", err)
	}
}

func TestEnvironNamesOnly(t *testing.T) {
	t.Setenv("HISTORYFIXTURE_SENTINEL", "must-not-be-inherited")
	for _, entry := range Environ("home") {
		if strings.HasPrefix(entry, "HISTORYFIXTURE_SENTINEL=") {
			t.Fatal("the fixture environment inherits a host variable")
		}
	}
}
