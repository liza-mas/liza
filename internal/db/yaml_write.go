package db

import (
	"encoding"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

// writeString prevents yaml.v3's literal-block emitter from discarding leading
// whitespace, or producing invalid indentation, before that text is lost.
// Ordinary strings retain the library's existing style and type resolution.
type writeString string

func (s writeString) MarshalYAML() (any, error) {
	text := string(s)
	if strings.Contains(text, "\n") && len(text) > 0 && strings.ContainsRune(" \t\r\n", rune(text[0])) && utf8.ValidString(text) {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: text}, nil
	}
	return text, nil // the emitter retains its binary encoding for non-UTF-8 strings
}

type writeType struct {
	typeOf     reflect.Type
	opaque     bool // preserve method dispatch or an unsupported reflected shape
	parse      bool // exceptional output still needs the legacy fail-closed check
	recursive  bool
	fields     []int // exported, serialized fields; avoid repeated reflected field allocations
	inlineMaps map[int]bool
	ignored    []int // yaml:"-" fields still contribute to their parent's omitempty check
}

var (
	writeTypes            sync.Map // source reflect.Type -> writeType; independent of field values
	writeStringType       = reflect.TypeOf(writeString(""))
	yamlMarshaler         = reflect.TypeFor[yaml.Marshaler]()
	yamlUnmarshaler       = reflect.TypeFor[yaml.Unmarshaler]()
	textMarshaler         = reflect.TypeFor[encoding.TextMarshaler]()
	yamlZeroer            = reflect.TypeFor[yaml.IsZeroer]()
	yamlNodeType          = reflect.TypeFor[yaml.Node]()
	acceptanceType        = reflect.TypeFor[models.AcceptanceCommandResult]()
	timeType              = reflect.TypeFor[time.Time]()
	lifecycleIdentityType = reflect.TypeFor[models.LifecycleIdentity]()
)

// projectedWriteType preserves tags/inline/omitempty through the same emitter.
// Types with custom methods stay intact; recursive/embedded shapes fall back
// rather than inventing a new serialization contract. Cache by source shape so
// changing which strings need quoting cannot grow the reflection type cache.
func projectedWriteType(t reflect.Type, active map[reflect.Type]bool) writeType {
	if cached, ok := writeTypes.Load(t); ok {
		return cached.(writeType)
	}
	result := writeType{typeOf: t}
	base := t
	if base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	switch {
	case base == timeType || base == acceptanceType:
		result.opaque = true // audited library/model marshalers, including quoted execution text
	case base == yamlNodeType || t.Implements(yamlMarshaler) || t.Implements(textMarshaler) || t.Implements(yamlZeroer) || t.Implements(yamlUnmarshaler) || reflect.PointerTo(t).Implements(yamlUnmarshaler):
		// yaml.v3 also uses pointer Unmarshaler methods when deciding which
		// inline fields to emit. Preserve those types even on the write path.
		result.opaque, result.parse = true, true
	case active[t]:
		return writeType{typeOf: t, opaque: true, parse: true, recursive: true}
	default:
		active[t] = true
		defer delete(active, t)
		switch t.Kind() {
		case reflect.String:
			result.typeOf = writeStringType
		case reflect.Pointer, reflect.Slice, reflect.Array:
			child := projectedWriteType(t.Elem(), active)
			if child.recursive {
				result.opaque, result.parse, result.recursive = true, true, true
				break
			}
			if child.typeOf == t.Elem() {
				break
			}
			switch t.Kind() {
			case reflect.Pointer:
				result.typeOf = reflect.PointerTo(child.typeOf)
			case reflect.Slice:
				result.typeOf = reflect.SliceOf(child.typeOf)
			case reflect.Array:
				result.typeOf = reflect.ArrayOf(t.Len(), child.typeOf)
			}
		case reflect.Map:
			if t.Key().Kind() != reflect.String {
				result.opaque, result.parse = true, true
				break
			}
			key := projectedWriteType(t.Key(), active)
			value := projectedWriteType(t.Elem(), active)
			if value.recursive {
				result.opaque, result.parse, result.recursive = true, true, true
				break
			}
			result.typeOf = reflect.MapOf(key.typeOf, value.typeOf)
		case reflect.Interface:
			if t.NumMethod() > 0 {
				result.opaque, result.parse = true, true
			}
		case reflect.Struct:
			fields := make([]reflect.StructField, t.NumField())
			for i := range fields {
				fields[i] = t.Field(i)
				if fields[i].Anonymous && fields[i].Type == lifecycleIdentityType && strings.Contains(","+fields[i].Tag.Get("yaml")+",", ",inline,") {
					// This model has only scalar fields and no YAML methods. Its
					// explicit inline tag preserves flattening without embedding
					// an anonymous synthetic type (unsupported by StructOf).
					fields[i].Anonymous = false
				}
				if fields[i].Anonymous {
					result.opaque, result.parse = true, true
					break
				}
				if fields[i].PkgPath == "" && fields[i].Tag.Get("yaml") == "-" {
					result.ignored = append(result.ignored, i)
				}
				if fields[i].PkgPath == "" && fields[i].Tag.Get("yaml") != "-" {
					child := projectedWriteType(fields[i].Type, active)
					if child.recursive {
						result.opaque, result.parse, result.recursive = true, true, true
						break
					}
					fields[i].Type = child.typeOf
					if fields[i].Type.Kind() == reflect.Map && strings.Contains(","+fields[i].Tag.Get("yaml")+",", ",inline,") {
						// yaml.v3 requires the exact built-in string type for inline
						// keys; ordinary map keys can use writeString directly.
						fields[i].Type = reflect.MapOf(reflect.TypeFor[string](), child.typeOf.Elem())
						if result.inlineMaps == nil {
							result.inlineMaps = make(map[int]bool)
						}
						result.inlineMaps[i] = true
					}
					result.fields = append(result.fields, i)
				}
			}
			if !result.opaque {
				result.typeOf = reflect.StructOf(fields)
			}
		}
	}
	writeTypes.LoadOrStore(t, result)
	return result
}

// projectWriteValue copies only the serialization view. Original pointers,
// maps and structs are never changed. The flag is collected in this traversal,
// avoiding a second walk just to detect exceptional custom YAML output.
func projectWriteValue(v reflect.Value, needsParse *bool) reflect.Value {
	shape := projectedWriteType(v.Type(), make(map[reflect.Type]bool))
	if shape.opaque {
		*needsParse = *needsParse || shape.parse
		return v
	}
	switch v.Kind() {
	case reflect.String:
		return v.Convert(writeStringType)
	case reflect.Interface:
		out := reflect.New(shape.typeOf).Elem()
		if !v.IsNil() {
			// yaml.v3 dispatches interface methods before dereferencing.
			// A typed nil *string must not acquire writeString's value
			// marshaler and panic before the emitter reaches its nil check.
			if v.Elem().Kind() == reflect.Pointer && v.Elem().IsNil() {
				out.Set(v.Elem())
				return out
			}
			out.Set(projectWriteValue(v.Elem(), needsParse))
		}
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(shape.typeOf)
		}
		out := reflect.New(shape.typeOf.Elem())
		out.Elem().Set(projectWriteValue(v.Elem(), needsParse))
		return out
	case reflect.Slice, reflect.Array:
		var out reflect.Value
		if v.Kind() == reflect.Slice {
			if v.IsNil() {
				return reflect.Zero(shape.typeOf)
			}
			out = reflect.MakeSlice(shape.typeOf, v.Len(), v.Len())
		} else {
			out = reflect.New(shape.typeOf).Elem()
		}
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(projectWriteValue(v.Index(i), needsParse))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(shape.typeOf)
		}
		out := reflect.MakeMapWithSize(shape.typeOf, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key := projectWriteValue(iter.Key(), needsParse)
			value := projectWriteValue(iter.Value(), needsParse)
			out.SetMapIndex(key, value)
		}
		return out
	case reflect.Struct:
		out := reflect.New(shape.typeOf).Elem()
		for _, i := range shape.ignored {
			out.Field(i).Set(v.Field(i))
		}
		for _, i := range shape.fields {
			value := projectWriteValue(v.Field(i), needsParse)
			if shape.inlineMaps[i] && !value.IsNil() {
				inline := reflect.MakeMapWithSize(out.Field(i).Type(), value.Len())
				iter := value.MapRange()
				for iter.Next() {
					// Inline maps require built-in string keys, so they cannot
					// use writeString. Multiline keys keep the old safety path;
					// changing their serialization requires an emitter fix.
					*needsParse = *needsParse || strings.Contains(iter.Key().String(), "\n")
					inline.SetMapIndex(iter.Key().Convert(reflect.TypeFor[string]()), iter.Value())
				}
				value = inline
			} else if shape.inlineMaps[i] {
				value = reflect.Zero(out.Field(i).Type())
			}
			out.Field(i).Set(value)
		}
		return out
	default:
		return v
	}
}
