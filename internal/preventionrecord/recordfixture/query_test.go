package recordfixture

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/gitbash"
)

// documentPath is the source file authors edit, not an embedded or installed copy.
var documentPath = filepath.Join("..", "..", "..", "skills", "shared", "references", "prevention-record.md")

const (
	periodHeading  = "### Period query"
	conceptHeading = "### Concept query"
)

func readDocument(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// shBlocks returns the body lines of every fenced sh block, keyed by the
// heading line it sits under, and the number of such blocks.
func shBlocks(t *testing.T, document string) (map[string][]string, int) {
	t.Helper()
	blocks := map[string][]string{}
	var heading string
	var inFence, isSh bool
	var count int
	for line := range strings.SplitSeq(document, "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			if inFence {
				inFence, isSh = false, false
				continue
			}
			inFence = true
			isSh = strings.TrimSpace(strings.TrimPrefix(line, "```")) == "sh"
			if isSh {
				count++
			}
		case inFence && isSh:
			blocks[heading] = append(blocks[heading], line)
		case !inFence && strings.HasPrefix(line, "#"):
			heading = line
		}
	}
	return blocks, count
}

// documentQueries extracts the two single-line blocks from the document.
func documentQueries(t *testing.T) (period, concept string) {
	t.Helper()
	blocks, count := shBlocks(t, readDocument(t))
	if count != 2 {
		t.Fatalf("document has %d sh blocks, want exactly 2", count)
	}
	for heading, want := range map[string]*string{periodHeading: &period, conceptHeading: &concept} {
		lines := blocks[heading]
		if len(lines) != 1 || strings.TrimSpace(lines[0]) == "" {
			t.Fatalf("heading %q holds %d lines, want one command", heading, len(lines))
		}
		*want = lines[0]
	}
	return period, concept
}

type queryRunner struct {
	t     *testing.T
	shell string
	repo  Repo
	home  string
}

func newQueryRunner(t *testing.T, repo Repo) queryRunner {
	t.Helper()
	shell, err := gitbash.Resolve()
	if err != nil {
		t.Fatalf("no shell to run the document's blocks: %v", err)
	}
	return queryRunner{t: t, shell: shell, repo: repo, home: t.TempDir()}
}

func (q queryRunner) run(block string, vars ...string) string {
	q.t.Helper()
	cmd := exec.Command(q.shell, "-c", block)
	cmd.Dir = q.repo.Dir
	cmd.Env = append(Environ(q.home), vars...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		q.t.Fatalf("query failed: %v: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		q.t.Errorf("query wrote to stderr: %s", stderr.String())
	}
	return stdout.String()
}

// expect renders the lines the table predicts for the given cases, in order.
func (q queryRunner) expect(ids ...string) string {
	q.t.Helper()
	var out strings.Builder
	for _, id := range ids {
		c, ok := q.repo.Case(id)
		if !ok || !c.Listed {
			q.t.Fatalf("case %q is not a listed case", id)
		}
		out.WriteString(c.Line() + "\n")
	}
	return out.String()
}

func (q queryRunner) listedNewestFirst() []string {
	var ids []string
	for i := len(q.repo.Cases) - 1; i >= 0; i-- {
		if q.repo.Cases[i].Listed {
			ids = append(ids, q.repo.Cases[i].ID)
		}
	}
	return ids
}

func TestPreventionRecordQueries(t *testing.T) {
	periodBlock, conceptBlock := documentQueries(t)
	repo := buildInto(t)
	q := newQueryRunner(t, repo)

	const whole = "2026-02-01T00:00:00Z"
	const wholeEnd = "2026-04-01T00:00:00Z"
	wholeVars := []string{"SINCE=" + whole, "UNTIL=" + wholeEnd}
	window := func(id string) []string {
		c, _ := repo.Case(id)
		return []string{
			"SINCE=" + c.Date.AddDate(0, 0, -14).Format(queryDateLayout),
			"UNTIL=" + c.Date.Format(queryDateLayout),
		}
	}
	concept := func(vars []string, tokens string) []string {
		return append(append([]string{}, vars...), "CONCEPT="+tokens)
	}

	t.Run("period lists every fix, newest first, with its record fields", func(t *testing.T) {
		got := q.run(periodBlock, wholeVars...)
		want := q.expect(q.listedNewestFirst()...)
		if got != want {
			t.Errorf("period output:\n%s\nwant:\n%s", got, want)
		}
		for _, absent := range []string{"c01", "c15", "c16"} {
			c, _ := repo.Case(absent)
			if strings.Contains(got, c.SHA) {
				t.Errorf("period lists %s, which is not a fix", absent)
			}
		}
	})

	t.Run("period keeps only the commits inside it", func(t *testing.T) {
		got := q.run(periodBlock, window("c07")...)
		if want := q.expect("c07", "c06", "c05", "c04", "c03", "c02"); got != want {
			t.Errorf("14 days ending at c07:\n%s\nwant:\n%s", got, want)
		}
		if got := q.run(periodBlock, "SINCE=2027-01-01T00:00:00Z", "UNTIL=2027-02-01T00:00:00Z"); got != "" {
			t.Errorf("an empty period lists %q", got)
		}
	})

	concepts := []struct {
		name   string
		vars   []string
		tokens string
		want   []string
	}{
		{"an invariant fixed twice within 14 days", window("c07"), "T0.2", []string{"c07", "c02"}},
		{"a non-fix commit carrying the record is not listed", wholeVars, "T0.2", []string{"c07", "c02"}},
		{"a concept fixed twice more than 14 days apart", window("c17"), "S1.1", []string{"c17"}},
		{"the same concept across the whole fixture", wholeVars, "S1.1", []string{"c17", "c08"}},
		{"a record line outside the final block is not read", wholeVars, "T0.21", []string{"c14", "c11", "c03"}},
		{"a moved boundary found by its current token", wholeVars, "internal/work/drain.go#Drain", []string{"c18"}},
		{"both tokens of a moved boundary", wholeVars, "internal/work/drain.go#Drain internal/queue/drain.go#Drain", []string{"c18", "c14", "c13"}},
		{"a token is not a prefix match", wholeVars, "internal/cache/store.go", []string{"c10"}},
		{"a token is case-sensitive", wholeVars, "t0.2", nil},
		{"no token lists nothing", wholeVars, "", nil},
	}
	for _, tc := range concepts {
		t.Run("concept: "+tc.name, func(t *testing.T) {
			got := q.run(conceptBlock, concept(tc.vars, tc.tokens)...)
			if want := q.expect(tc.want...); got != want {
				t.Errorf("CONCEPT=%q:\n%s\nwant:\n%s", tc.tokens, got, want)
			}
		})
	}

	t.Run("repository settings do not change either block", func(t *testing.T) {
		period := q.run(periodBlock, wholeVars...)
		t21 := q.run(conceptBlock, concept(wholeVars, "T0.21")...)
		for _, setting := range [][]string{
			{"grep.patternType", "fixed"},
			{"log.showSignature", "true"},
		} {
			runGit(t, repo.Dir, "config", setting[0], setting[1])
		}
		if got := q.run(periodBlock, wholeVars...); got != period {
			t.Errorf("period output changed under repository settings:\n%s\nwas:\n%s", got, period)
		}
		if got := q.run(conceptBlock, concept(wholeVars, "T0.21")...); got != t21 {
			t.Errorf("T0.21 output changed under repository settings:\n%s\nwas:\n%s", got, t21)
		}
	})
}

func TestPreventionRecordDocumentStructure(t *testing.T) {
	document := readDocument(t)
	for _, heading := range []string{periodHeading, conceptHeading} {
		if strings.Count(document, "\n"+heading+"\n") != 1 {
			t.Errorf("heading %q does not occur exactly once", heading)
		}
	}
	for _, word := range []string{"git 2.22.0", "POSIX `awk`", "final trailer block"} {
		if !strings.Contains(document, word) {
			t.Errorf("document does not state %q", word)
		}
	}
	for _, key := range []string{"Prevention-Cause", "Prevention-Concept", "Prevention-Disposition", "Prevention-Owner", "Prevention-Class"} {
		if !strings.Contains(document, "`"+key+"`") {
			t.Errorf("document does not define key %s", key)
		}
	}
}

func TestPreventionRecordDocumentBrandNeutral(t *testing.T) {
	document := readDocument(t)
	lower := strings.ToLower(document)
	for _, token := range []string{"liza", "omni", "§"} {
		if strings.Contains(lower, token) {
			t.Errorf("document contains the forbidden token %q", token)
		}
	}
	for _, token := range []string{"materialize", "recordfixture", "makefile", "go.mod"} {
		if strings.Contains(lower, token) {
			t.Errorf("document names the project tooling %q", token)
		}
	}
}
