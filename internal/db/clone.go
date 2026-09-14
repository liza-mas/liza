package db

import (
	"reflect"
	"sync"

	"github.com/liza-mas/liza/internal/models"
)

// cloneState returns a deep copy of state.
//
// The parsed-state cache hands the same *models.State to every ReadCached
// caller, and ReadCached promises a result the caller may mutate freely.
// Copying the parsed structure is an order of magnitude cheaper than
// re-parsing the YAML, which is what the cache exists to avoid.
func cloneState(state *models.State) *models.State {
	if state == nil {
		return nil
	}
	clone := new(models.State)
	deepCopyValue(reflect.ValueOf(state).Elem(), reflect.ValueOf(clone).Elem())
	return clone
}

// deepCopyValue copies src into dst, duplicating every pointer, slice, map and
// interface it reaches. It is generic by reflection rather than generated per
// model so that adding a field to models.State needs no matching copy code.
//
// The one escape hatch is the struct branch below, which copies structs with
// unexported fields by assignment. That is sound only while such types hold no
// caller-mutable references; TestStateModelShapeIsCloneable enforces it by
// walking the models.State type graph, so a new type that breaks the property
// fails there rather than aliasing the cache silently.
func deepCopyValue(src, dst reflect.Value) {
	if !needsDeepCopy(src.Type()) {
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
		deepCopyValue(src.Elem(), elem.Elem())
		dst.Set(elem)

	case reflect.Interface:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		inner := src.Elem()
		copied := reflect.New(inner.Type()).Elem()
		deepCopyValue(inner, copied)
		dst.Set(copied)

	case reflect.Slice:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		out := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		for i := 0; i < src.Len(); i++ {
			deepCopyValue(src.Index(i), out.Index(i))
		}
		dst.Set(out)

	case reflect.Array:
		for i := 0; i < src.Len(); i++ {
			deepCopyValue(src.Index(i), dst.Index(i))
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
			deepCopyValue(iter.Key(), key)
			val := reflect.New(iter.Value().Type()).Elem()
			deepCopyValue(iter.Value(), val)
			out.SetMapIndex(key, val)
		}
		dst.Set(out)

	case reflect.Struct:
		// A struct with unexported fields (time.Time being the one that occurs
		// in the state model) cannot be rebuilt field by field. State types are
		// YAML-serializable, so unexported fields carry no caller-mutable data
		// and a shallow assignment is sound. Guarded by
		// TestStateModelShapeIsCloneable.
		if hasUnexportedFields(src.Type()) {
			dst.Set(src)
			return
		}
		for i := 0; i < src.NumField(); i++ {
			deepCopyValue(src.Field(i), dst.Field(i))
		}

	default:
		dst.Set(src)
	}
}

var deepCopyNeeded sync.Map // reflect.Type -> bool

// needsDeepCopy reports whether values of t can alias shared memory. Types that
// cannot (scalars, and structs built only from them) are copied by assignment.
func needsDeepCopy(t reflect.Type) bool {
	if cached, ok := deepCopyNeeded.Load(t); ok {
		return cached.(bool)
	}
	result := computeNeedsDeepCopy(t)
	deepCopyNeeded.Store(t, result)
	return result
}

func computeNeedsDeepCopy(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return needsDeepCopy(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if needsDeepCopy(t.Field(i).Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func hasUnexportedFields(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() {
			return true
		}
	}
	return false
}
