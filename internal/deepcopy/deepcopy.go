// Package deepcopy duplicates values by reflection, so that a parsed structure
// can be handed out as a copy the caller may mutate freely. Copying a parsed
// structure is an order of magnitude cheaper than parsing its source again.
//
// It is generic by reflection rather than generated per type so that adding a
// field to a copied type needs no matching copy code. The one escape hatch is a
// struct with unexported fields, which is copied by assignment: that is sound
// only while such structs hold no caller-mutable references. ShallowAliases
// finds the types that break this, for a guard test on each copied root type.
package deepcopy

import (
	"reflect"
	"sync"
	"time"
)

// Of returns a deep copy of *src, or nil when src is nil.
func Of[T any](src *T) *T {
	if src == nil {
		return nil
	}
	clone := new(T)
	Into(reflect.ValueOf(src).Elem(), reflect.ValueOf(clone).Elem())
	return clone
}

// Into copies src into dst, duplicating every pointer, slice, map and
// interface it reaches.
func Into(src, dst reflect.Value) {
	if !Needed(src.Type()) {
		dst.Set(src)
		return
	}

	switch src.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		elem := reflect.New(src.Type().Elem())
		Into(src.Elem(), elem.Elem())
		dst.Set(elem)

	case reflect.Interface:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		inner := src.Elem()
		copied := reflect.New(inner.Type()).Elem()
		Into(inner, copied)
		dst.Set(copied)

	case reflect.Slice:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		out := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		for i := 0; i < src.Len(); i++ {
			Into(src.Index(i), out.Index(i))
		}
		dst.Set(out)

	case reflect.Array:
		for i := 0; i < src.Len(); i++ {
			Into(src.Index(i), dst.Index(i))
		}

	case reflect.Map:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		out := reflect.MakeMapWithSize(src.Type(), src.Len())
		iter := src.MapRange()
		for iter.Next() {
			key := reflect.New(iter.Key().Type()).Elem()
			Into(iter.Key(), key)
			val := reflect.New(iter.Value().Type()).Elem()
			Into(iter.Value(), val)
			out.SetMapIndex(key, val)
		}
		dst.Set(out)

	case reflect.Struct:
		// A struct with unexported fields (time.Time being the sanctioned case)
		// cannot be rebuilt field by field. The copied types are serializable,
		// so unexported fields carry no caller-mutable data and a shallow
		// assignment is sound. Guarded by ShallowAliases.
		if HasUnexportedFields(src.Type()) {
			dst.Set(src)
			return
		}
		for i := 0; i < src.NumField(); i++ {
			Into(src.Field(i), dst.Field(i))
		}

	default:
		dst.Set(src)
	}
}

var needed sync.Map // reflect.Type -> bool

// Needed reports whether values of t can alias shared memory. Types that
// cannot (scalars, and structs built only from them) are copied by assignment.
func Needed(t reflect.Type) bool {
	if cached, ok := needed.Load(t); ok {
		return cached.(bool)
	}
	result := computeNeeded(t)
	needed.Store(t, result)
	return result
}

func computeNeeded(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return Needed(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if Needed(t.Field(i).Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// HasUnexportedFields reports whether struct type t has an unexported field.
func HasUnexportedFields(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

// ShallowAliases walks the type graph of root and describes each struct that
// Into would copy shallowly (it has unexported fields) although it carries a
// reference-bearing exported field, so a copy would alias the original.
// time.Time is the sanctioned case. A guard test asserts it returns nothing.
func ShallowAliases(root reflect.Type, name string) []string {
	var found []string
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(t reflect.Type, path string) {
		if seen[t] {
			return
		}
		seen[t] = true

		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(t.Elem(), path+"[]")
		case reflect.Map:
			walk(t.Key(), path+".key")
			walk(t.Elem(), path+".value")
		case reflect.Struct:
			if t == reflect.TypeFor[time.Time]() {
				return
			}
			if HasUnexportedFields(t) {
				for i := 0; i < t.NumField(); i++ {
					field := t.Field(i)
					if field.IsExported() && Needed(field.Type) {
						found = append(found, path+" ("+t.String()+") has unexported fields and reference-bearing field "+field.Name)
					}
				}
				return
			}
			for i := 0; i < t.NumField(); i++ {
				walk(t.Field(i).Type, path+"."+t.Field(i).Name)
			}
		}
	}
	walk(root, name)
	return found
}
