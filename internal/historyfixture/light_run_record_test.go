package historyfixture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/preventionrecord/recordfixture"
)

// The light run's committed evidence: the overlay copied into the sample-record
// repository and the recorded report of the skill's light mode over it.
const (
	lightRunOverlayDir = "testdata/light-overlay"
	lightRunRecordPath = "testdata/recorded-runs/light-run.md"
)

var (
	lightRunHeadsLine  = regexp.MustCompile(`(?m)^- Fixture heads: i3 ([0-9a-f]{40})$`)
	lightRunHexToken   = regexp.MustCompile(`[0-9a-f]{7,40}`)
	lightRunHeading    = regexp.MustCompile(`^(#+) (.*)$`)
	lightRunPathToken  = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]*)+`)
	lightRunConceptT02 = regexp.MustCompile(`\bT0\.2\b`)
	lightRunFiveDays   = regexp.MustCompile(`(^|[^0-9A-Za-z.-])5([^0-9A-Za-z.-]|$)`)
)

func lightRunRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// lightRunSection returns the lines under the first heading containing name,
// up to the next heading of the same or a higher level.
func lightRunSection(t *testing.T, doc, name string) []string {
	t.Helper()
	var out []string
	level := 0
	for _, line := range strings.Split(doc, "\n") {
		m := lightRunHeading.FindStringSubmatch(line)
		if level == 0 {
			if m != nil && strings.Contains(m[2], name) {
				level = len(m[1])
			}
			continue
		}
		if m != nil && len(m[1]) <= level {
			break
		}
		out = append(out, line)
	}
	if level == 0 {
		t.Fatalf("no heading contains %q", name)
	}
	return out
}

// lightRunNames reports whether text names the commit: its full SHA, or a
// prefix of at least 7 hex characters, appears as a whole token.
func lightRunNames(text, sha string) bool {
	for _, tok := range lightRunHexToken.FindAllString(text, -1) {
		if strings.HasPrefix(sha, tok) {
			return true
		}
	}
	return false
}

func lightRunBuild(t *testing.T) recordfixture.Repo {
	t.Helper()
	repo, err := recordfixture.Build(filepath.Join(t.TempDir(), "i3"))
	if err != nil {
		t.Fatalf("build the sample-record repository: %v", err)
	}
	return repo
}

func lightRunSHA(t *testing.T, repo recordfixture.Repo, id string) string {
	t.Helper()
	c, ok := repo.Case(id)
	if !ok {
		t.Fatalf("case %s is not in the sample-record repository", id)
	}
	return c.SHA
}

func TestLightRunOverlay(t *testing.T) {
	invariants := lightRunRead(t, filepath.Join(lightRunOverlayDir, "invariants.md"))
	guardrails := lightRunRead(t, filepath.Join(lightRunOverlayDir, "guardrails.md"))

	lessons, err := os.ReadDir(filepath.Join(lightRunOverlayDir, "lessons"))
	if err != nil {
		t.Fatalf("read the lessons directory: %v", err)
	}
	if len(lessons) != 1 {
		t.Fatalf("lessons directory holds %d entries, want one lesson file", len(lessons))
	}

	// Table rows: the first cell's first backticked token is the ID.
	var ids []string
	noID := 0
	enforced := map[string]string{}
	inTable := false
	for _, line := range strings.Split(invariants, "\n") {
		if strings.HasPrefix(line, "| Invariant |") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 3 || strings.HasPrefix(strings.TrimSpace(cells[0]), "---") {
			continue
		}
		first := strings.TrimSpace(cells[0])
		if strings.HasPrefix(first, "`") {
			id := strings.SplitN(first[1:], "`", 2)[0]
			ids = append(ids, id)
			enforced[id] = strings.TrimSpace(cells[2])
		} else {
			noID++
		}
	}
	if got, want := strings.Join(ids, " "), "T0.2 T0.21 S1.1 R1.1"; got != want {
		t.Errorf("invariant IDs = %q, want %q", got, want)
	}
	if noID != 1 {
		t.Errorf("rows without an ID = %d, want 1", noID)
	}
	if enforced["T0.21"] != "unenforced" {
		t.Errorf("T0.21 Enforced cell = %q, want %q", enforced["T0.21"], "unenforced")
	}
	for _, id := range []string{"T0.2", "S1.1", "R1.1"} {
		if enforced[id] == "unenforced" || enforced[id] == "" {
			t.Errorf("%s Enforced cell = %q, want an enforcement", id, enforced[id])
		}
	}

	var triggers []string
	for _, line := range strings.Split(guardrails, "\n") {
		if strings.HasPrefix(line, "**Triggers:**") {
			triggers = append(triggers, line)
		}
	}
	if len(triggers) != 1 {
		t.Fatalf("guardrails carry %d Triggers lines, want 1", len(triggers))
	}
	paths := lightRunPathToken.FindAllString(triggers[0], -1)
	if len(paths) != 1 || paths[0] != "internal/api/" {
		t.Errorf("Triggers line names paths %q, want only internal/api/", paths)
	}
}

func TestLightRunRecord(t *testing.T) {
	doc := lightRunRead(t, lightRunRecordPath)
	repo := lightRunBuild(t)

	m := lightRunHeadsLine.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("record has no `- Fixture heads: i3 <sha>` line")
	}
	if m[1] != repo.Head {
		t.Errorf("recorded Fixture heads SHA %s differs from a fresh build's HEAD %s", m[1], repo.Head)
	}
	if strings.Contains(doc, "/tmp/") {
		t.Error("record names a host temporary path; absolute paths must read <fixture>")
	}

	// The four protocol headings, in order.
	pos := -1
	for _, h := range []string{"## Header", "## Report", "## Evidence map", "## Deviations"} {
		i := strings.Index(doc, "\n"+h+"\n")
		if i <= pos {
			t.Errorf("heading %q missing or out of order", h)
		}
		pos = i
	}

	sha := func(id string) string { return lightRunSHA(t, repo, id) }

	widened := strings.Join(lightRunSection(t, doc, "Widened triggers"), "\n")
	if !strings.Contains(widened, "internal/cache/") {
		t.Error("Widened triggers does not name internal/cache/")
	}
	for _, id := range []string{"c04", "c05"} {
		if !lightRunNames(widened, sha(id)) {
			t.Errorf("Widened triggers does not name %s", id)
		}
	}

	open := strings.Join(lightRunSection(t, doc, "Open dispositions"), "\n")
	named := false
	for _, id := range []string{"c03", "c04", "c14"} {
		named = named || lightRunNames(open, sha(id))
	}
	if !named {
		t.Error("Open dispositions names none of c03, c04, c14")
	}

	found := false
	for _, line := range lightRunSection(t, doc, "Fixed-twice intervals") {
		if !lightRunConceptT02.MatchString(line) {
			continue
		}
		if lightRunNames(line, sha("c02")) && lightRunNames(line, sha("c07")) && lightRunFiveDays.MatchString(line) {
			found = true
		}
	}
	if !found {
		t.Error("no Fixed-twice intervals line names T0.2, c02, c07 and 5")
	}
}
