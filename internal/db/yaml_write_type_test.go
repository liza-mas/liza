package db

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

type writeShapeRecursive struct {
	Name string
	Next *writeShapeRecursive
}

type writeShapeEmbedded struct{ Value string }

// These characterize projectedWriteType's result per shape: which classification
// each type gets, which struct fields are kept, ignored or inlined, and that the
// recursion marker propagates through every container kind.
func TestProjectedWriteTypeShapes(t *testing.T) {
	type plain struct {
		A      string            `yaml:"a"`
		Skip   string            `yaml:"-"`
		Inline map[string]string `yaml:",inline"`
		List   []string          `yaml:"list"`
	}
	type withLifecycle struct {
		models.LifecycleIdentity `yaml:",inline"`
		Extra                    string `yaml:"extra"`
	}
	type withEmbedded struct {
		writeShapeEmbedded
		Extra string
	}
	type holdsRecursive struct {
		Before string
		Loop   writeShapeRecursive
		After  string
	}
	stringer := reflect.TypeFor[fmt.Stringer]()
	cases := []struct {
		name string
		typ  reflect.Type
		want writeType
		// typeOf is checked separately for derived (non-opaque) types.
		typeOf reflect.Type
	}{
		{name: "time", typ: reflect.TypeFor[time.Time](), want: writeType{opaque: true}, typeOf: reflect.TypeFor[time.Time]()},
		{name: "time pointer", typ: reflect.TypeFor[*time.Time](), want: writeType{opaque: true}, typeOf: reflect.TypeFor[*time.Time]()},
		{name: "acceptance result", typ: reflect.TypeFor[models.AcceptanceCommandResult](), want: writeType{opaque: true}, typeOf: reflect.TypeFor[models.AcceptanceCommandResult]()},
		{name: "text marshaler", typ: reflect.TypeFor[writeTextValue](), want: writeType{opaque: true, parse: true}, typeOf: reflect.TypeFor[writeTextValue]()},
		{name: "pointer unmarshaler", typ: reflect.TypeFor[writeInlineDecoded](), want: writeType{opaque: true, parse: true}, typeOf: reflect.TypeFor[writeInlineDecoded]()},
		{name: "string", typ: reflect.TypeFor[string](), want: writeType{}, typeOf: writeStringType},
		{name: "int", typ: reflect.TypeFor[int](), want: writeType{}, typeOf: reflect.TypeFor[int]()},
		{name: "pointer to string", typ: reflect.TypeFor[*string](), want: writeType{}, typeOf: reflect.PointerTo(writeStringType)},
		{name: "slice of string", typ: reflect.TypeFor[[]string](), want: writeType{}, typeOf: reflect.SliceOf(writeStringType)},
		{name: "array of string", typ: reflect.TypeFor[[3]string](), want: writeType{}, typeOf: reflect.ArrayOf(3, writeStringType)},
		{name: "slice of int unchanged", typ: reflect.TypeFor[[]int](), want: writeType{}, typeOf: reflect.TypeFor[[]int]()},
		{name: "string map", typ: reflect.TypeFor[map[string]string](), want: writeType{}, typeOf: reflect.MapOf(writeStringType, writeStringType)},
		{name: "non-string key map", typ: reflect.TypeFor[map[int]string](), want: writeType{opaque: true, parse: true}, typeOf: reflect.TypeFor[map[int]string]()},
		{name: "empty interface", typ: reflect.TypeFor[any](), want: writeType{}, typeOf: reflect.TypeFor[any]()},
		{name: "method interface", typ: stringer, want: writeType{opaque: true, parse: true}, typeOf: stringer},
		{
			name: "plain struct", typ: reflect.TypeFor[plain](),
			want: writeType{fields: []int{0, 2, 3}, inlineMaps: map[int]bool{2: true}, ignored: []int{1}},
			typeOf: reflect.StructOf([]reflect.StructField{
				{Name: "A", Type: writeStringType, Tag: `yaml:"a"`},
				{Name: "Skip", Type: reflect.TypeFor[string](), Tag: `yaml:"-"`},
				{Name: "Inline", Type: reflect.MapOf(reflect.TypeFor[string](), writeStringType), Tag: `yaml:",inline"`},
				{Name: "List", Type: reflect.SliceOf(writeStringType), Tag: `yaml:"list"`},
			}),
		},
		{
			name: "lifecycle identity inline", typ: reflect.TypeFor[withLifecycle](),
			want: writeType{fields: []int{0, 1}},
		},
		{name: "other embedded field", typ: reflect.TypeFor[withEmbedded](), want: writeType{opaque: true, parse: true}, typeOf: reflect.TypeFor[withEmbedded]()},
		{name: "recursive struct", typ: reflect.TypeFor[writeShapeRecursive](), want: writeType{opaque: true, parse: true, recursive: true, fields: []int{0}}, typeOf: reflect.TypeFor[writeShapeRecursive]()},
		{name: "recursive pointer", typ: reflect.TypeFor[*writeShapeRecursive](), want: writeType{opaque: true, parse: true, recursive: true}, typeOf: reflect.TypeFor[*writeShapeRecursive]()},
		{name: "recursive slice", typ: reflect.TypeFor[[]writeShapeRecursive](), want: writeType{opaque: true, parse: true, recursive: true}, typeOf: reflect.TypeFor[[]writeShapeRecursive]()},
		{name: "recursive map value", typ: reflect.TypeFor[map[string]writeShapeRecursive](), want: writeType{opaque: true, parse: true, recursive: true}, typeOf: reflect.TypeFor[map[string]writeShapeRecursive]()},
		{name: "struct holding recursive field", typ: reflect.TypeFor[holdsRecursive](), want: writeType{opaque: true, parse: true, recursive: true, fields: []int{0}}, typeOf: reflect.TypeFor[holdsRecursive]()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := projectedWriteType(tc.typ, make(map[reflect.Type]bool))
			if tc.typeOf != nil && got.typeOf != tc.typeOf {
				t.Fatalf("typeOf = %v, want %v", got.typeOf, tc.typeOf)
			}
			if tc.typeOf == nil && (got.typeOf == nil || got.typeOf.Kind() != reflect.Struct || got.typeOf == tc.typ) {
				t.Fatalf("typeOf = %v, want a synthetic struct", got.typeOf)
			}
			got.typeOf, tc.want.typeOf = nil, nil
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("shape = %+v, want %+v", got, tc.want)
			}
			cached, ok := writeTypes.Load(tc.typ)
			if !ok {
				t.Fatal("shape was not cached")
			}
			again := projectedWriteType(tc.typ, map[reflect.Type]bool{tc.typ: true})
			if !reflect.DeepEqual(again, cached.(writeType)) {
				t.Fatalf("cached shape not returned despite active entry: %+v vs %+v", again, cached)
			}
		})
	}
}

// An active entry for an uncached type is reported as recursive and is never
// cached, so a later top-level lookup classifies the type from its real shape.
func TestProjectedWriteTypeActiveEntryIsNotCached(t *testing.T) {
	type local struct{ Name string }
	typ := reflect.TypeFor[local]()
	got := projectedWriteType(typ, map[reflect.Type]bool{typ: true})
	if !got.opaque || !got.parse || !got.recursive || got.typeOf != typ {
		t.Fatalf("active type = %+v", got)
	}
	if _, ok := writeTypes.Load(typ); ok {
		t.Fatal("recursion marker was cached")
	}
	active := make(map[reflect.Type]bool)
	shape := projectedWriteType(typ, active)
	if shape.opaque || shape.recursive || len(shape.fields) != 1 {
		t.Fatalf("real shape = %+v", shape)
	}
	if len(active) != 0 {
		t.Fatalf("active set not cleaned up: %v", active)
	}
}
