package a9

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func (p *scenario) harness(t testing.TB, shas []string) *oracle.Normaliser {
	t.Helper()
	n := oracle.NewNormaliser()
	n.Dir(p.dir, oracle.TokenProject)
	n.Dir(p.home, oracle.TokenHome)
	n.Dir(p.root, oracle.TokenTmp)
	for _, sha := range shas {
		if err := n.KeepSHA(sha); err != nil {
			t.Fatal(err)
		}
	}
	return n
}

// Fixed commits are retained only after checking both dates and the message.
// Unknown 40/64-hex strings still pass through equality-preserving ID tokens.
func (p *scenario) fixedSHAs(t testing.TB) []string {
	t.Helper()
	set := map[string]bool{}
	for _, sha := range p.recipeSHAs {
		set[sha] = true
	}
	for _, sha := range strings.Fields(p.git(t, "rev-list", "--all")) {
		parts := strings.SplitN(strings.TrimSpace(p.git(t, "show", "-s", "--format=%aI%n%cI%n%B", sha)), "\n", 3)
		if len(parts) != 3 {
			t.Fatal("A9 collect: commit metadata shape")
		}
		if parts[0] == fixedDate && parts[1] == fixedDate && !a9TimeRE.MatchString(parts[2]) && !a9VolatileRE.MatchString(parts[2]) && !a9StampRE.MatchString(parts[2]) {
			set[sha] = true
		}
	}
	shas := make([]string, 0, len(set))
	for sha := range set {
		shas = append(shas, sha)
	}
	sort.Strings(shas)
	return shas
}

func (p *scenario) runtimeFiles(t testing.TB) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	root := filepath.Join(p.dir, ".liza")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		keep := name == "alerts.log" || strings.HasPrefix(name, "state.yaml.liveness-") || strings.HasPrefix(name, "provider-") || strings.HasPrefix(name, "usage/records-") && strings.HasSuffix(name, ".jsonl")
		if !keep {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[name] = data
		return nil
	})
	if err != nil {
		t.Fatalf("A9 %s collect runtime bundle: %v", p.name, err)
	}
	return out
}

func (p *scenario) listing(t testing.TB) []byte {
	t.Helper()
	var lines []string
	for _, dir := range []string{"agent-prompts", "agent-outputs"} {
		entries, err := os.ReadDir(filepath.Join(p.dir, ".liza", dir))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("A9 %s collect listing", p.name)
		}
		for _, entry := range entries {
			lines = append(lines, dir+"/"+entry.Name())
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// missingArtifacts lists the committed runtime-file goldens of a scenario that
// are absent from the observed artifact names (sorted).
func missingArtifacts(scenario string, observed map[string]bool) []string {
	entries, err := os.ReadDir(filepath.Join("testdata", "golden", "state", scenario, "files"))
	if err != nil {
		return nil
	}
	var missing []string
	for _, entry := range entries {
		if !observed[entry.Name()] {
			missing = append(missing, entry.Name())
		}
	}
	return missing
}

var fileSafeRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (p *scenario) check(t *testing.T, result invocation) {
	t.Helper()
	shas := p.fixedSHAs(t)
	n := p.harness(t, shas)
	pair := func(name string, before, after []byte) {
		norm := newA9Normaliser(shas)
		n.CheckState(t, p.name+"/"+name, []byte(norm.text(string(before))), []byte(norm.text(string(after))))
	}
	pair("state.yaml", p.before, p.state(t))
	// File listings are sets, sorted after tokenisation while retaining duplicates.
	listingNorm := newA9Normaliser(shas)
	pair("listing.txt", []byte(oracle.SortLines(listingNorm.text(string(p.beforeListing)))), []byte(oracle.SortLines(listingNorm.text(string(p.listing(t))))))
	launches, _ := filepath.Glob(filepath.Join(p.recordDir, "launch-*"))
	sort.Strings(launches)
	if len(launches) == 0 {
		pair("launch.yaml", p.before, p.state(t))
	} else {
		for i, launch := range launches {
			var b bytes.Buffer
			paths, _ := filepath.Glob(filepath.Join(launch, "*"))
			sort.Strings(paths)
			for _, path := range paths {
				fmt.Fprintf(&b, "## %s\n", filepath.Base(path))
				b.Write(mustRead(t, path))
			}
			name := "launch.yaml"
			if len(launches) > 1 {
				name = fmt.Sprintf("launch-%d.yaml", i+1)
			}
			pair(name, p.before, b.Bytes())
		}
	}
	afterFiles := p.runtimeFiles(t)
	all := map[string]bool{}
	for name := range p.beforeFiles {
		all[name] = true
	}
	for name := range afterFiles {
		all[name] = true
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	ordinals := map[string]int{}
	observed := map[string]bool{}
	for _, name := range names {
		pattern := a9StampRE.ReplaceAllString(name, "<STAMP>")
		pattern = a9DateRE.ReplaceAllString(pattern, "<DATE>")
		pattern = regexp.MustCompile(`liveness-[0-9a-f]+`).ReplaceAllString(pattern, "liveness-<HASH>")
		ordinals[pattern]++
		for _, token := range []string{"<STAMP>", "<DATE>", "<HASH>"} {
			pattern = strings.ReplaceAll(pattern, token, fmt.Sprint(ordinals[pattern]))
		}
		safe := fileSafeRE.ReplaceAllString(pattern, "-")
		observed[safe] = true
		pair("files/"+safe, p.beforeFiles[name], afterFiles[name])
	}
	// A committed runtime-file golden whose artifact the run no longer writes has
	// no observed side to compare, so its absence is checked here.
	if missing := missingArtifacts(p.name, observed); len(missing) > 0 {
		t.Fatalf("A9 %s collect: committed runtime artifacts not produced: %s", p.name, strings.Join(missing, ", "))
	}
	var human strings.Builder
	fmt.Fprintf(&human, "exit_code: %d\n[stdout]\n", result.code)
	for _, stream := range []string{result.stdout, result.stderr} {
		for _, line := range strings.Split(stream, "\n") {
			if strings.HasPrefix(line, "time=") && strings.Contains(line, " level=") {
				continue
			}
			human.WriteString(line)
			human.WriteByte('\n')
		}
		if stream == result.stdout {
			human.WriteString("[stderr]\n")
		}
	}
	n.Check(t, oracle.Human, p.name+"/agent.txt", []byte(newA9Normaliser(shas).text(human.String())))
	for i, r := range p.records(t) {
		if r.Error != "" {
			t.Fatalf("A9 %s run: stub call %d: %s", p.name, i+1, r.Error)
		}
	}
}
