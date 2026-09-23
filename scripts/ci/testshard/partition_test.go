package main

import (
	"fmt"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func testWeights() *Weights {
	return &Weights{
		OpsPackage:         "example.com/m/internal/ops",
		DefaultTestSecs:    1,
		DefaultPackageSecs: 5,
		Tests:              map[string]float64{"TestHeavy": 40, "TestMedium": 10},
		Packages:           map[string]float64{"example.com/m/big": 100, "example.com/m/mid": 60},
	}
}

func makeNames(n int, seed int64) []string {
	r := rand.New(rand.NewSource(seed))
	words := []string{"Accept", "Claim", "Merge", "Review", "Submit", "Task", "Worktree", "Gate"}
	seen := map[string]bool{}
	var out []string
	for len(out) < n {
		name := "Test" + words[r.Intn(len(words))] + words[r.Intn(len(words))] + fmt.Sprint(r.Intn(1000))
		if r.Intn(4) == 0 {
			name += "_" + words[r.Intn(len(words))]
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func assertExactCover(t *testing.T, items []string, shards []Shard) {
	t.Helper()
	count := map[string]int{}
	for _, s := range shards {
		for _, it := range s.Items {
			count[it]++
		}
	}
	for _, it := range items {
		if count[it] != 1 {
			t.Fatalf("%q assigned %d times", it, count[it])
		}
	}
	if len(count) != len(items) {
		t.Fatalf("assigned %d distinct items, want %d", len(count), len(items))
	}
}

func TestPartitionLPTCoversEveryItemOnceAndBalances(t *testing.T) {
	w := testWeights()
	items := []string{"example.com/m/big", "example.com/m/mid"}
	for i := 0; i < 30; i++ {
		items = append(items, fmt.Sprintf("example.com/m/p%02d", i))
	}
	shards := partition(groupRest, items, w, 3)
	assertExactCover(t, items, shards)
	var lo, hi float64 = 1e9, 0
	for _, s := range shards {
		lo, hi = min(lo, s.Weight), max(hi, s.Weight)
	}
	// LPT bound: the spread never exceeds the heaviest item.
	if hi-lo > 100 {
		t.Fatalf("unbalanced shards: min %.1f max %.1f", lo, hi)
	}
	again := partition(groupRest, append([]string(nil), items...), w, 3)
	for i := range shards {
		if strings.Join(shards[i].Items, ",") != strings.Join(again[i].Items, ",") {
			t.Fatalf("partition is not deterministic for shard %d", i)
		}
	}
}

func TestPartitionRangesContiguousBalancedNonEmpty(t *testing.T) {
	w := testWeights()
	items := append(makeNames(500, 1), "TestHeavy", "TestMedium")
	items = uniqueSorted(items)
	for n := 1; n <= 7; n++ {
		shards := partitionRanges(groupOps, items, w, n)
		assertExactCover(t, items, shards)
		total := 0.0
		pos := 0
		for _, s := range shards {
			if len(s.Items) == 0 {
				t.Fatalf("n=%d: empty shard %d", n, s.Index)
			}
			for _, it := range s.Items {
				if items[pos] != it {
					t.Fatalf("n=%d: shard %d is not a contiguous run", n, s.Index)
				}
				pos++
			}
			total += s.Weight
		}
		for _, s := range shards {
			if d := s.Weight - total/float64(n); d > 40 || d < -40 {
				t.Fatalf("n=%d: shard %d weight %.1f too far from %.1f", n, s.Index, s.Weight, total/float64(n))
			}
		}
	}
	// More shards than items still gives every shard one item.
	few := []string{"TestA", "TestB", "TestC"}
	shards := partitionRanges(groupOps, few, w, 3)
	for _, s := range shards {
		if len(s.Items) != 1 {
			t.Fatalf("shard %d has %d items, want 1", s.Index, len(s.Items))
		}
	}
}

// TestRangeRegexMatchesExactlyTheRange checks the pattern against the
// definition lo < s <= hi for random strings, including regex metacharacters,
// prefixes of the bounds, and non-ASCII runes.
func TestRangeRegexMatchesExactlyTheRange(t *testing.T) {
	alphabet := []rune("ABTXYZ_abtz09.-[]^$\\|()*+?{}/é\x00")
	r := rand.New(rand.NewSource(7))
	randStr := func(maxLen int) string {
		n := r.Intn(maxLen + 1)
		rs := make([]rune, n)
		for i := range rs {
			rs[i] = alphabet[r.Intn(len(alphabet))]
		}
		return string(rs)
	}
	for trial := 0; trial < 400; trial++ {
		a, b := randStr(6), randStr(6)
		if r.Intn(3) == 0 && len(a) > 1 {
			b = a[:len(a)-1] // prefix relationships are the tricky cases
		}
		if a > b {
			a, b = b, a
		}
		var lo, hi bound
		switch trial % 4 {
		case 0:
			lo, hi = bound{true, a}, bound{true, b}
		case 1:
			hi = bound{true, b}
		case 2:
			lo = bound{true, a}
		}
		if lo.set && hi.set && lo.name >= hi.name {
			continue
		}
		pattern := rangeRegex(lo, hi)
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("lo=%q hi=%q: %v (%s)", lo.name, hi.name, err, pattern)
		}
		probes := []string{lo.name, hi.name, lo.name + "A", hi.name + "A", ""}
		for i := 0; i < 200; i++ {
			probes = append(probes, randStr(7))
		}
		for _, s := range probes {
			want := (!lo.set || s > lo.name) && (!hi.set || s <= hi.name)
			if got := re.MatchString(s); got != want {
				t.Fatalf("lo=%q hi=%q s=%q: match=%v want %v (pattern %s)", lo.name, hi.name, s, got, want, pattern)
			}
		}
	}
}

func TestRangeRegexOfAdjacentShardsCoversAnyName(t *testing.T) {
	items := makeNames(300, 3)
	shards := partitionRanges(groupOps, items, testWeights(), 4)
	var res []*regexp.Regexp
	for i := range shards {
		args := shardArgs(groupOps, shards, i, "pkg")
		if strings.Contains(args[1], "/") {
			t.Fatalf("pattern contains '/', which go test would split into subtest levels: %s", args[1])
		}
		res = append(res, regexp.MustCompile(args[1]))
	}
	// Names that did not exist at planning time must still land in exactly one shard.
	for _, name := range append(makeNames(300, 99), "Test", "TestZZZZ", "Example_a", "FuzzX") {
		n := 0
		for _, re := range res {
			if re.MatchString(name) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%q matched by %d shards", name, n)
		}
	}
}

func TestCheckPartitionRejectsBrokenAssignments(t *testing.T) {
	w := testWeights()
	items := makeNames(120, 5)
	good := func() []Shard { return partitionRanges(groupOps, items, w, 3) }
	if err := checkPartition(groupOps, items, good(), w.OpsPackage, defaultMaxLen); err != nil {
		t.Fatalf("valid partition rejected: %v", err)
	}
	cases := map[string]func([]Shard) []Shard{
		"not assigned": func(s []Shard) []Shard {
			s[1].Items = s[1].Items[1:]
			return s
		},
		"assigned to shards": func(s []Shard) []Shard {
			s[0].Items = append(s[0].Items, s[2].Items[0])
			return s
		},
		"unknown item": func(s []Shard) []Shard {
			s[2].Items = append(s[2].Items, "TestNotListed")
			return s
		},
		// Moving the first item of shard 2 to shard 1's middle keeps the cover
		// exact but breaks contiguity, so the range patterns no longer match
		// the assignment.
		"-run pattern match": func(s []Shard) []Shard {
			moved := s[1].Items[0]
			s[1].Items = s[1].Items[1:]
			s[2].Items = append([]string{moved}, s[2].Items...)
			sort.Strings(s[2].Items)
			return s
		},
		"empty": func(s []Shard) []Shard {
			s[0].Items = append(s[0].Items, s[1].Items...)
			sort.Strings(s[0].Items)
			s[1].Items = nil
			return s
		},
	}
	for want, breakIt := range cases {
		err := checkPartition(groupOps, items, breakIt(good()), w.OpsPackage, defaultMaxLen)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got error %v", want, err)
		}
	}
	if err := checkPartition(groupOps, items, good(), w.OpsPackage, 50); err == nil || !strings.Contains(err.Error(), "command line") {
		t.Errorf("length limit not enforced: %v", err)
	}
}

func TestParseTestListKeepsOnlyRunnableTopLevelNames(t *testing.T) {
	in := "TestB\nBenchmarkX\nExampleFoo\nFuzzParse\nsome stray output\nTestA\nTestA\nok  \texample.com/m/internal/ops\t0.3s\n"
	got, err := parseTestList(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if want := "ExampleFoo,FuzzParse,TestA,TestB"; strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestParsePackageListExcludesOpsPackage(t *testing.T) {
	in := "example.com/m/b\r\nexample.com/m/internal/ops\nexample.com/m/a\n\n"
	got, err := parsePackageList(strings.NewReader(in), "example.com/m/internal/ops")
	if err != nil {
		t.Fatal(err)
	}
	if want := "example.com/m/a,example.com/m/b"; strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestWeightFallsBackToDefault(t *testing.T) {
	w := testWeights()
	if got := w.weight(groupOps, "TestHeavy"); got != 40 {
		t.Fatalf("known test weight %v", got)
	}
	if got := w.weight(groupOps, "TestNew"); got != 1 {
		t.Fatalf("unknown test weight %v", got)
	}
	if got := w.weight(groupRest, "example.com/m/new"); got != 5 {
		t.Fatalf("unknown package weight %v", got)
	}
}
