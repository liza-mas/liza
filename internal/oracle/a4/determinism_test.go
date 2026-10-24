package a4

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

var (
	instantRE        = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z| ?[+-]\d{2}:?\d{2}(?: (?:[A-Z]{2,5}|[+-]\d{2}(?:\d{2})?))?| UTC)?`)
	offsetRE         = regexp.MustCompile(`[+-]\d{2}:?\d{2}`)
	identityRE       = regexp.MustCompile(`(?i)objects/[0-9a-f]{2}/[0-9a-f]{64}\.json|\b(?:[0-9a-f]{64}|[0-9a-f]{40})\b`)
	objectPathRE     = regexp.MustCompile(`objects/[0-9a-f]{2}/([0-9a-f]{64})\.json`)
	durationRE       = regexp.MustCompile(`\b(?:\d+(?:\.\d+)?(?:ns|us|µs|ms|h|m|s))+\b`)
	reportDurationRE = regexp.MustCompile(`(?m)((?:Elapsed|Remaining|Overdue by):\*?\*?\s*)[^\r\n]+`)
	archiveBytesRE   = regexp.MustCompile(`("bytes"\s*:\s*)\d+`)
	humanBytesRE     = regexp.MustCompile(`\(\d+ bytes\)`)
)

// Each document owns its mapping: before, after, then files share identity tokens.
type a4Normaliser struct {
	harness       *oracle.Normaliser
	shas, digests map[string]string
	nextSHA       int
	paths         [][2]string
}

func newA4Normaliser(h *oracle.Normaliser) *a4Normaliser {
	return &a4Normaliser{harness: h, shas: map[string]string{}, digests: map[string]string{}}
}

// Replace only the directory literal. The harness path-tail conversion treats
// a JSON escape such as backslash-n as a separator; doing this first preserves
// the archive object's encoded YAML content.
func (n *a4Normaliser) dir(path, token string) {
	forms := []string{path, filepath.ToSlash(path)}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		forms = append(forms, resolved, filepath.ToSlash(resolved))
	}
	for _, form := range forms {
		n.paths = append(n.paths, [2]string{form, token}, [2]string{strings.ReplaceAll(form, "/", `\/`), token})
	}
	sort.SliceStable(n.paths, func(i, j int) bool { return len(n.paths[i][0]) > len(n.paths[j][0]) })
}

func (n *a4Normaliser) digest(s string) string {
	s = strings.ToLower(s)
	if token := n.digests[s]; token != "" {
		return token
	}
	token := fmt.Sprintf("<DIGEST %d>", len(n.digests)+1)
	n.digests[s] = token
	return token
}

func (n *a4Normaliser) text(s string) string {
	for _, path := range n.paths {
		s = strings.ReplaceAll(s, path[0], path[1])
	}
	s = instantRE.ReplaceAllStringFunc(s, func(instant string) string {
		zone := "Z"
		if offset := offsetRE.FindString(instant[10:]); offset != "" {
			zone = offset
			if len(zone) == 5 {
				zone = zone[:3] + ":" + zone[3:]
			}
		}
		return "<INSTANT " + zone + ">"
	})
	s = identityRE.ReplaceAllStringFunc(s, func(value string) string {
		value = strings.ToLower(value)
		if parts := objectPathRE.FindStringSubmatch(value); len(parts) != 0 {
			token := n.digest(parts[1])
			return "objects/" + strings.TrimSuffix(token, ">") + ":2>/" + token + ".json"
		}
		if len(value) == 64 {
			return n.digest(value)
		}
		if token := n.shas[value]; token != "" {
			return token
		}
		n.nextSHA++
		token := fmt.Sprintf("<SHA %d>", n.nextSHA)
		n.shas[value] = token
		return token
	})
	s = reportDurationRE.ReplaceAllString(s, "${1}<DURATION>")
	s = durationRE.ReplaceAllString(s, "<DURATION>")
	return n.harness.Text(s)
}

func (n *a4Normaliser) archiveText(s string) string {
	s = archiveBytesRE.ReplaceAllString(s, `${1}"<BYTES>"`)
	s = humanBytesRE.ReplaceAllString(s, "(<BYTES> bytes)")
	return n.text(s)
}

func TestA4Determinism(t *testing.T) {
	n := newA4Normaliser(oracle.NewNormaliser())
	sha, digest := strings.Repeat("a", 40), strings.Repeat("b", 64)
	input := "2020-01-01T00:00:00.123Z 2020-01-01 02:00:00 +0200 EET 2020-01-01T00:00:00-0500 " + sha + " " + digest + " objects/bb/" + digest + ".json 12m3.4s\n**Elapsed:** 10m\n**Overdue by:** -47m\n"
	got := n.text(input)
	want := "<INSTANT Z> <INSTANT +02:00> <INSTANT -05:00> <SHA 1> <DIGEST 1> objects/<DIGEST 1:2>/<DIGEST 1>.json <DURATION>\n**Elapsed:** <DURATION>\n**Overdue by:** <DURATION>\n"
	if got != want {
		t.Fatalf("determinism tokens differ:\ngot %s\nwant %s", got, want)
	}
	if n.text(got) != got {
		t.Fatal("normalisation is not idempotent")
	}
	ordered := newA4Normaliser(oracle.NewNormaliser()).text(strings.Repeat("c", 64) + " objects/bb/" + digest + ".json")
	if ordered != "<DIGEST 1> objects/<DIGEST 2:2>/<DIGEST 2>.json" {
		t.Fatal("digest order must follow first appearance, including object paths")
	}
	if n.archiveText(`{"bytes":2010}`) != `{"bytes":"<BYTES>"}` {
		t.Fatal("archive bytes not tokenised")
	}
	if n.text(`{"bytes":26}`) != `{"bytes":26}` {
		t.Fatal("ordinary byte count changed")
	}
	h := oracle.NewNormaliser()
	h.Dir("/fixture/root", "<FIXTURE>")
	rootNorm := newA4Normaliser(h)
	rootNorm.dir("/fixture/root", "<FIXTURE>")
	if rootNorm.text(`{"value_yaml":"path: /fixture/root/specs/p.md\n"}`) != `{"value_yaml":"path: <FIXTURE>/specs/p.md\n"}` {
		t.Fatal("archive object root not tokenised")
	}
}
