package models

import (
	"reflect"
	"testing"
)

func TestTransitionVocabularyConstantValues(t *testing.T) {
	t.Parallel()

	cardinalities := map[TransitionCardinality]string{
		CardinalityPerSubtask: "per-subtask",
		CardinalityOneToOne:   "one-to-one",
		CardinalityManyToOne:  "many-to-one",
	}
	for got, want := range cardinalities {
		if string(got) != want {
			t.Errorf("cardinality constant = %q, want %q", got, want)
		}
	}
	triggers := map[TransitionTrigger]string{TransitionTriggerManual: "manual", TransitionTriggerAuto: "auto"}
	for got, want := range triggers {
		if string(got) != want {
			t.Errorf("trigger constant = %q, want %q", got, want)
		}
	}
	routes := map[AllocationRoute]string{
		AllocationRouteNone:               "",
		AllocationRouteScopeDecomposition: "scope-decomposition",
		AllocationRouteCodingAllocation:   "coding-allocation",
	}
	for got, want := range routes {
		if string(got) != want {
			t.Errorf("allocation route constant = %q, want %q", got, want)
		}
	}
}

func TestTransitionVocabularyTypesDeclareNoSerializationMethods(t *testing.T) {
	t.Parallel()

	forbidden := []string{"MarshalYAML", "UnmarshalYAML", "MarshalText", "UnmarshalText", "MarshalJSON", "UnmarshalJSON", "IsZero"}
	types := []reflect.Type{
		reflect.TypeOf(TransitionCardinality("")),
		reflect.TypeOf(TransitionTrigger("")),
		reflect.TypeOf(AllocationRoute("")),
	}
	for _, typ := range types {
		for _, candidate := range []reflect.Type{typ, reflect.PointerTo(typ)} {
			for _, name := range forbidden {
				if _, found := candidate.MethodByName(name); found {
					t.Errorf("%s declares %s", candidate, name)
				}
			}
		}
	}
}
