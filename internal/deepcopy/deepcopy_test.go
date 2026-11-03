package deepcopy

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type leaf struct {
	Names []string
	At    time.Time
}

type root struct {
	Items  []leaf
	ByName map[string]*leaf
	Any    any
	Count  int
}

func TestOfIsolatesEveryReference(t *testing.T) {
	src := &root{
		Items:  []leaf{{Names: []string{"a"}}},
		ByName: map[string]*leaf{"x": {Names: []string{"b"}}},
		Any:    []any{"c"},
		Count:  1,
	}
	clone := Of(src)
	if !reflect.DeepEqual(src, clone) {
		t.Fatalf("copy differs from its source: %#v", clone)
	}
	clone.Items[0].Names[0] = "mutated"
	clone.ByName["x"].Names[0] = "mutated"
	clone.Any.([]any)[0] = "mutated"
	if src.Items[0].Names[0] != "a" || src.ByName["x"].Names[0] != "b" || src.Any.([]any)[0] != "c" {
		t.Fatalf("mutating the copy changed the source: %#v", src)
	}
	if Of[root](nil) != nil {
		t.Fatal("Of(nil) is not nil")
	}
}

type aliasing struct {
	hidden int
	Shared []string
}

func TestShallowAliasesReportsAliasingStructs(t *testing.T) {
	if found := ShallowAliases(reflect.TypeFor[root](), "root"); len(found) != 0 {
		t.Fatalf("time.Time-only structs reported: %v", found)
	}
	found := ShallowAliases(reflect.TypeFor[struct{ Inner []aliasing }](), "outer")
	if len(found) != 1 || !strings.Contains(found[0], "outer.Inner[]") || !strings.Contains(found[0], "Shared") {
		t.Fatalf("want one report naming outer.Inner[] and Shared, got %v", found)
	}
	_ = aliasing{hidden: 0}
}
