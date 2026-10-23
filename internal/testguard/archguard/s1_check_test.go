package archguard

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var (
	kFile = Key{File: "internal/x/big.go"}
	kFunc = Key{File: "internal/x/big.go", Func: "Big.Run"}
	kOld  = Key{File: "internal/x/old.go", Func: "Old"}
	kNew  = Key{File: "internal/x/new.go", Func: "Old"}
)

func ratchetArea(id AreaID, entries ...RatchetEntry) *Area {
	return &Area{ID: id, Ratchet: entries}
}

func s2Area(id AreaID, entries ...S2Entry) *Area {
	return &Area{ID: id, S2: entries}
}

func rat(k Key, ceiling int) RatchetEntry { return RatchetEntry{Key: k, Ceiling: ceiling} }

func tableS2(k Key) S2Entry {
	return S2Entry{Key: k, Category: S2Table, Rationale: "lookup table"}
}

// kinds renders violations as sorted "kind key" strings.
func kinds(vs []violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Kind + " " + v.Key.String()
	}
	sort.Strings(out)
	return out
}

func TestCheckS1(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		measured map[Key]int
		snapshot map[Key]int
		areas    []*Area
		want     []string // "kind key", sorted
	}{
		{
			name:     "clean: listed offender within ceiling",
			measured: map[Key]int{kFunc: 120},
			snapshot: map[Key]int{kFunc: 130},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, 125))},
		},
		{
			name:     "clean: ceiling equal to snapshot and measured equal to ceiling",
			measured: map[Key]int{kFile: 900},
			snapshot: map[Key]int{kFile: 900},
			areas:    []*Area{ratchetArea("A1", rat(kFile, 900))},
		},
		{
			name:     "clean: S2-listed offender",
			measured: map[Key]int{kFunc: 400},
			snapshot: map[Key]int{},
			areas:    []*Area{s2Area("A1", tableS2(kFunc))},
		},
		{
			name:     "unlisted offender fails",
			measured: map[Key]int{kFunc: 120, kFile: 900},
			snapshot: map[Key]int{kFunc: 130, kFile: 900},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, 125))},
			want:     []string{"unlisted " + kFile.String()},
		},
		{
			name:     "ceiling overrun fails",
			measured: map[Key]int{kFunc: 126},
			snapshot: map[Key]int{kFunc: 130},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, 125))},
			want:     []string{"overrun " + kFunc.String()},
		},
		{
			name:     "stale ratchet entry fails once the offender is back under the limit",
			measured: map[Key]int{},
			snapshot: map[Key]int{kFunc: 130},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, 125))},
			want:     []string{"stale " + kFunc.String()},
		},
		{
			name:     "stale ratchet entry fails when the offender is exactly at the limit",
			measured: map[Key]int{}, // measureOffenders omits a function of exactly maxFuncLines
			snapshot: map[Key]int{kFunc: 130},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, maxFuncLines))},
			want:     []string{"stale " + kFunc.String()},
		},
		{
			name:     "renamed offender: old key stale, new key unlisted",
			measured: map[Key]int{kNew: 120},
			snapshot: map[Key]int{kOld: 130},
			areas:    []*Area{ratchetArea("A1", rat(kOld, 125))},
			want:     []string{"stale " + kOld.String(), "unlisted " + kNew.String()},
		},
		{
			name:     "renamed offender whose new key is ratchet-listed is not in the snapshot",
			measured: map[Key]int{kNew: 120},
			snapshot: map[Key]int{kOld: 130},
			areas:    []*Area{ratchetArea("A1", rat(kOld, 125), rat(kNew, 125))},
			want:     []string{"not-in-snapshot " + kNew.String(), "stale " + kOld.String()},
		},
		{
			name:     "ceiling raised above its snapshot value fails",
			measured: map[Key]int{kFunc: 120},
			snapshot: map[Key]int{kFunc: 130},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, 131))},
			want:     []string{"above-snapshot " + kFunc.String()},
		},
		{
			name:     "duplicate within one ratchet list",
			measured: map[Key]int{kFunc: 120},
			snapshot: map[Key]int{kFunc: 130},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, 125), rat(kFunc, 125))},
			want:     []string{"duplicate " + kFunc.String()},
		},
		{
			name:     "duplicate across areas",
			measured: map[Key]int{kFunc: 120},
			snapshot: map[Key]int{kFunc: 130},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, 125)), ratchetArea("A2", rat(kFunc, 125))},
			want:     []string{"duplicate " + kFunc.String()},
		},
		{
			name:     "duplicate across ratchet and S2 lists",
			measured: map[Key]int{kFunc: 120},
			snapshot: map[Key]int{kFunc: 130},
			areas:    []*Area{{ID: "A1", Ratchet: []RatchetEntry{rat(kFunc, 125)}, S2: []S2Entry{tableS2(kFunc)}}},
			want:     []string{"duplicate " + kFunc.String()},
		},
		{
			name:     "stale S2 entry fails",
			measured: map[Key]int{},
			snapshot: map[Key]int{},
			areas:    []*Area{s2Area("A1", tableS2(kFunc))},
			want:     []string{"stale " + kFunc.String()},
		},
		{
			name:     "S2 entry with unknown category",
			measured: map[Key]int{kFunc: 400},
			areas:    []*Area{s2Area("A1", S2Entry{Key: kFunc, Category: "whatever", Rationale: "r"})},
			want:     []string{"invalid-s2-entry " + kFunc.String()},
		},
		{
			name:     "S2 entry without rationale",
			measured: map[Key]int{kFunc: 400},
			areas:    []*Area{s2Area("A1", S2Entry{Key: kFunc, Category: S2Generated, Rationale: " "})},
			want:     []string{"invalid-s2-entry " + kFunc.String()},
		},
		{
			name:     "human-ruling S2 entry without ruling reference",
			measured: map[Key]int{kFunc: 400},
			areas:    []*Area{s2Area("A1", S2Entry{Key: kFunc, Category: S2HumanRuling, Rationale: "r"})},
			want:     []string{"invalid-s2-entry " + kFunc.String()},
		},
		{
			name:     "human-ruling S2 entry with ruling reference passes",
			measured: map[Key]int{kFunc: 400},
			areas:    []*Area{s2Area("A1", S2Entry{Key: kFunc, Category: S2HumanRuling, Ruling: "D99", Rationale: "r"})},
		},
		{
			name:     "several violations are reported together",
			measured: map[Key]int{kFile: 900, kFunc: 126, kNew: 120},
			snapshot: map[Key]int{kFunc: 130, kOld: 130},
			areas:    []*Area{ratchetArea("A1", rat(kFunc, 125), rat(kOld, 125))},
			want: []string{
				"overrun " + kFunc.String(),
				"stale " + kOld.String(),
				"unlisted " + kFile.String(),
				"unlisted " + kNew.String(),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := append([]string(nil), tc.want...)
			sort.Strings(want)

			got := kinds(checkS1(tc.measured, tc.snapshot, tc.areas))

			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("violations = %v, want %v", got, want)
			}
		})
	}
}

func TestCheckS1MessagesNameLimitAndAreaFile(t *testing.T) {
	t.Parallel()
	measured := map[Key]int{kFunc: 126, kFile: 900, kNew: 120}
	snapshot := map[Key]int{kFunc: 130, kOld: 130}
	areas := []*Area{
		ratchetArea("A3", rat(kFunc, 125), rat(kOld, 125)),
		s2Area("A5", tableS2(Key{File: "internal/x/gone.go"})),
	}

	vs := checkS1(measured, snapshot, areas)

	byKey := map[string]violation{}
	for _, v := range vs {
		byKey[v.Kind+" "+v.Key.String()] = v
	}
	contain := func(id string, subs ...string) {
		t.Helper()
		v, ok := byKey[id]
		if !ok {
			t.Fatalf("no violation %q in %v", id, kinds(vs))
		}
		for _, s := range subs {
			if !strings.Contains(v.Msg, s) {
				t.Errorf("message for %q = %q, missing %q", id, v.Msg, s)
			}
		}
	}
	a3 := areaFile("A3")
	contain("overrun "+kFunc.String(), kFunc.String(), "126", "125", "100", a3)
	contain("stale "+kOld.String(), kOld.String(), "100", a3)
	contain("stale internal/x/gone.go", "internal/x/gone.go", "800", areaFile("A5"), "S2")
	contain("unlisted "+kFile.String(), kFile.String(), "900", "800", "S2", "reduce")
	contain("unlisted "+kNew.String(), kNew.String(), "120", "100")

	root := filepath.ToSlash(repoRoot())
	for _, v := range vs {
		if strings.Contains(v.Msg, root) || strings.Contains(v.Msg, "/home/") {
			t.Errorf("message %q leaks an absolute path", v.Msg)
		}
	}
}

func TestCheckS1DuplicateMessageNamesEveryAreaFile(t *testing.T) {
	t.Parallel()
	areas := []*Area{ratchetArea("A1", rat(kFunc, 125)), ratchetArea("A2", rat(kFunc, 125))}

	vs := checkS1(map[Key]int{kFunc: 120}, map[Key]int{kFunc: 130}, areas)

	if len(vs) != 1 {
		t.Fatalf("violations = %v, want one duplicate", kinds(vs))
	}
	for _, id := range []AreaID{"A1", "A2"} {
		if !strings.Contains(vs[0].Msg, areaFile(id)) {
			t.Errorf("message %q does not name %s", vs[0].Msg, areaFile(id))
		}
	}
}
