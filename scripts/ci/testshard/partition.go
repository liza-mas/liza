package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Group names. The ops group partitions the top-level tests of a single
// package; the rest group partitions whole packages.
const (
	groupOps  = "ops"
	groupRest = "rest"
)

// Weights is the checked-in cost model used for bin-packing. Values are
// estimated seconds on the Windows runner; only their relative size matters.
type Weights struct {
	Source             string             `json:"source"`
	OpsPackage         string             `json:"ops_package"`
	DefaultTestSecs    float64            `json:"default_test_secs"`
	DefaultPackageSecs float64            `json:"default_package_secs"`
	Tests              map[string]float64 `json:"tests"`
	Packages           map[string]float64 `json:"packages"`
}

func loadWeights(path string) (*Weights, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w Weights
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if w.OpsPackage == "" {
		return nil, fmt.Errorf("%s: ops_package is required", path)
	}
	if w.DefaultTestSecs <= 0 || w.DefaultPackageSecs <= 0 {
		return nil, fmt.Errorf("%s: default weights must be positive", path)
	}
	return &w, nil
}

func (w *Weights) weight(group, item string) float64 {
	table, def := w.Packages, w.DefaultPackageSecs
	if group == groupOps {
		table, def = w.Tests, w.DefaultTestSecs
	}
	if v, ok := table[item]; ok && v > 0 {
		return v
	}
	return def
}

var identRE = regexp.MustCompile(`^[\p{L}_][\p{L}\p{Nd}_]*$`)

// parseTestList extracts runnable top-level names from `go test -list` output.
// Benchmarks are excluded because -run never executes them; the trailing
// "ok <pkg>" line and any stray TestMain output are ignored.
func parseTestList(r io.Reader) ([]string, error) {
	var names []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !identRE.MatchString(line) {
			continue
		}
		for _, prefix := range []string{"Test", "Example", "Fuzz"} {
			if strings.HasPrefix(line, prefix) {
				names = append(names, line)
				break
			}
		}
	}
	return uniqueSorted(names), sc.Err()
}

// parsePackageList extracts import paths from `go list` output, excluding the
// package whose tests the ops group owns.
func parsePackageList(r io.Reader, opsPackage string) ([]string, error) {
	var pkgs []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line == opsPackage || strings.ContainsAny(line, " \t") {
			continue
		}
		pkgs = append(pkgs, line)
	}
	return uniqueSorted(pkgs), sc.Err()
}

func uniqueSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Shard is one bin of a partition.
type Shard struct {
	Index  int
	Items  []string // sorted
	Weight float64
}

// partition assigns every item to exactly one of n shards with greedy
// longest-processing-time bin-packing: heaviest item first, each to the
// currently lightest shard. Ties break on name and lowest shard index, so the
// result depends only on the item set, the weights, and n.
func partition(group string, items []string, w *Weights, n int) []Shard {
	shards := make([]Shard, n)
	for i := range shards {
		shards[i].Index = i
	}
	order := append([]string(nil), items...)
	sort.SliceStable(order, func(a, b int) bool {
		wa, wb := w.weight(group, order[a]), w.weight(group, order[b])
		if wa != wb {
			return wa > wb
		}
		return order[a] < order[b]
	})
	for _, item := range order {
		best := 0
		for i := 1; i < n; i++ {
			if shards[i].Weight < shards[best].Weight {
				best = i
			}
		}
		shards[best].Items = append(shards[best].Items, item)
		shards[best].Weight += w.weight(group, item)
	}
	for i := range shards {
		sort.Strings(shards[i].Items)
	}
	return shards
}

// partitionRanges assigns every item to exactly one of n shards as contiguous
// runs of the sorted item list, cutting at the boundary closest to each ideal
// cumulative weight k*total/n. A contiguous run is expressible as a short
// lexicographic range pattern (see rangeRegex), whereas an explicit list of
// ~300 names per shard would exceed 10k characters even trie-compressed. Each
// shard's load stays within one item's weight of total/n.
func partitionRanges(group string, items []string, w *Weights, n int) []Shard {
	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	total := 0.0
	for _, it := range sorted {
		total += w.weight(group, it)
	}
	shards := make([]Shard, n)
	for i := range shards {
		shards[i].Index = i
	}
	cur, acc := 0, 0.0
	for i, it := range sorted {
		wt := w.weight(group, it)
		// Move to the next shard once the ideal cut lies before this item's
		// midpoint, but never leave a later shard without items.
		for cur < n-1 && len(shards[cur].Items) > 0 &&
			(acc+wt/2 > total*float64(cur+1)/float64(n) || len(sorted)-i <= n-1-cur) {
			cur++
		}
		shards[cur].Items = append(shards[cur].Items, it)
		shards[cur].Weight += wt
		acc += wt
	}
	return shards
}

// partitionFor selects the strategy per group: contiguous ranges for the ops
// tests (keeps the -run pattern short), LPT bin-packing for packages.
func partitionFor(group string, items []string, w *Weights, n int) []Shard {
	if group == groupOps {
		return partitionRanges(group, items, w, n)
	}
	return partition(group, items, w, n)
}

// bound is an optional exclusive-lower or inclusive-upper name limit.
type bound struct {
	set  bool
	name string
}

// rangeBounds returns the name range owned by shard i of a range partition:
// (last name of shard i-1, last name of shard i]. The first shard has no lower
// bound and the last has no upper bound, so adjacent shards cover every
// possible test name exactly once, including names added after planning.
func rangeBounds(shards []Shard, i int) (lo, hi bound) {
	if i > 0 {
		prev := shards[i-1].Items
		lo = bound{true, prev[len(prev)-1]}
	}
	if i < len(shards)-1 {
		cur := shards[i].Items
		hi = bound{true, cur[len(cur)-1]}
	}
	return lo, hi
}

// rangeRegex builds an anchored pattern matching exactly the strings s with
// lo < s <= hi in Go's byte-wise (equivalently, for UTF-8, code-point) order.
// Its length is proportional to the bound names, not to the number of tests in
// the range, which keeps every shard's command line short.
func rangeRegex(lo, hi bound) string {
	var body string
	switch {
	case !lo.set && !hi.set:
		body = ".*"
	case !lo.set:
		body = leqRE([]rune(hi.name))
	case !hi.set:
		body = gtRE([]rune(lo.name))
	default:
		body = betweenRE([]rune(lo.name), []rune(hi.name))
	}
	return "^(" + body + ")$"
}

const maxRune = 0x10FFFF

// classRE renders the rune class [from-to]; an empty range yields "".
func classRE(from, to rune) string {
	if from > to {
		return ""
	}
	if from == to {
		return regexp.QuoteMeta(string(from))
	}
	return "[" + classRune(from) + "-" + classRune(to) + "]"
}

func classRune(r rune) string {
	if r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
		return string(r)
	}
	return fmt.Sprintf(`\x{%x}`, r)
}

func alt(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != never {
			keep = append(keep, p)
		}
	}
	if len(keep) == 1 {
		return keep[0]
	}
	return "(" + strings.Join(keep, "|") + ")"
}

// never marks an alternative that cannot match (an empty rune class); alt
// drops it. It is not a valid pattern fragment, so it cannot collide.
const never = "\x00<never>"

// gtRE matches strings strictly greater than x.
func gtRE(x []rune) string {
	if len(x) == 0 {
		return ".+"
	}
	higher := never
	if c := classRE(x[0]+1, maxRune); c != "" {
		higher = c + ".*"
	}
	return alt(regexp.QuoteMeta(string(x[0]))+gtRE(x[1:]), higher)
}

// leqRE matches strings less than or equal to x.
func leqRE(x []rune) string {
	if len(x) == 0 {
		return ""
	}
	lower := never
	if c := classRE(0, x[0]-1); c != "" {
		lower = c + ".*"
	}
	return alt("", lower, regexp.QuoteMeta(string(x[0]))+leqRE(x[1:]))
}

// betweenRE matches strings s with lo < s <= hi; it requires lo < hi.
func betweenRE(lo, hi []rune) string {
	if len(lo) == 0 {
		// Every non-empty string <= hi.
		lower := never
		if c := classRE(0, hi[0]-1); c != "" {
			lower = c + ".*"
		}
		return alt(lower, regexp.QuoteMeta(string(hi[0]))+leqRE(hi[1:]))
	}
	if lo[0] == hi[0] {
		return regexp.QuoteMeta(string(lo[0])) + betweenRE(lo[1:], hi[1:])
	}
	middle := never
	if c := classRE(lo[0]+1, hi[0]-1); c != "" {
		middle = c + ".*"
	}
	return alt(regexp.QuoteMeta(string(lo[0]))+gtRE(lo[1:]), middle, regexp.QuoteMeta(string(hi[0]))+leqRE(hi[1:]))
}

// shardArgs returns the `go test` arguments (after the fixed flags) for shard
// i of a partition.
func shardArgs(group string, shards []Shard, i int, opsPackage string) []string {
	if group == groupOps {
		lo, hi := rangeBounds(shards, i)
		return []string{"-run", rangeRegex(lo, hi), opsPackage}
	}
	return append([]string(nil), shards[i].Items...)
}

// baseCommand is the fixed prefix of every shard's command line; it is only
// used to measure the full length.
const baseCommand = "go test -json -count=1 -timeout 30m"

func commandLength(args []string) int {
	n := len(baseCommand)
	for _, a := range args {
		n += 1 + len(a)
	}
	return n
}

// checkPartition verifies the coverage invariants for a full partition:
// every item is assigned to exactly one shard, no shard holds an unknown item,
// every ops shard's -run pattern matches exactly its own names among all
// items, and no command line exceeds maxLen.
func checkPartition(group string, items []string, shards []Shard, opsPackage string, maxLen int) error {
	owner := make(map[string]int, len(items))
	for _, s := range shards {
		for _, it := range s.Items {
			if prev, dup := owner[it]; dup {
				return fmt.Errorf("%s: %q assigned to shards %d and %d", group, it, prev+1, s.Index+1)
			}
			owner[it] = s.Index
		}
	}
	known := make(map[string]bool, len(items))
	var missing []string
	for _, it := range items {
		known[it] = true
		if _, ok := owner[it]; !ok {
			missing = append(missing, it)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s: %d item(s) not assigned to any shard: %s", group, len(missing), abbreviate(missing, 20))
	}
	for it, idx := range owner {
		if !known[it] {
			return fmt.Errorf("%s: shard %d holds unknown item %q", group, idx+1, it)
		}
	}
	for i, s := range shards {
		if len(s.Items) == 0 {
			return fmt.Errorf("%s shard %d is empty; reduce the shard count", group, s.Index+1)
		}
		args := shardArgs(group, shards, i, opsPackage)
		if l := commandLength(args); l > maxLen {
			return fmt.Errorf("%s shard %d: command line is %d chars, limit %d; add shards", group, s.Index+1, l, maxLen)
		}
		if group != groupOps {
			continue
		}
		if strings.Contains(args[1], "/") {
			// go test splits -run at unbracketed slashes into subtest levels.
			return fmt.Errorf("%s shard %d: -run pattern contains '/': %s", group, s.Index+1, args[1])
		}
		re, err := regexp.Compile(args[1])
		if err != nil {
			return fmt.Errorf("%s shard %d: invalid -run pattern: %w", group, s.Index+1, err)
		}
		for _, it := range items {
			want := owner[it] == s.Index
			if got := re.MatchString(it); got != want {
				return fmt.Errorf("%s shard %d: -run pattern match(%q)=%v, want %v", group, s.Index+1, it, got, want)
			}
		}
	}
	return nil
}
