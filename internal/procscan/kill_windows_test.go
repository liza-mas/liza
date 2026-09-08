//go:build windows

package procscan

import (
	"reflect"
	"testing"
	"time"
)

// TestOrderProcessTree_BreaksCycleWithUnreadableCreationTimes covers the case
// the creation-time recycled-PID check cannot resolve on its own: a cycle in
// childrenByParent (stale parent pointers plus PID recycling) where every
// creation time is unreadable. Without the visited set, this would grow
// order without bound and never return.
func TestOrderProcessTree_BreaksCycleWithUnreadableCreationTimes(t *testing.T) {
	childrenByParent := map[uint32][]uint32{
		1: {2},
		2: {3},
		3: {1, 4}, // cycles back to root, and also reaches a real descendant
	}
	unreadable := func(uint32) (int64, bool) { return 0, false }

	done := make(chan []uint32, 1)
	go func() { done <- orderProcessTree(1, childrenByParent, unreadable) }()

	select {
	case order := <-done:
		want := []uint32{1, 2, 3, 4}
		if !reflect.DeepEqual(order, want) {
			t.Fatalf("orderProcessTree() = %v, want %v", order, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("orderProcessTree did not return: the cycle was not broken")
	}
}

// TestOrderProcessTree_RecycledPIDIsExcludedByCreationTime covers the
// existing recycled-PID heuristic still working once creation times are
// readable: a "child" that started before its recorded parent cannot really
// be its descendant.
func TestOrderProcessTree_RecycledPIDIsExcludedByCreationTime(t *testing.T) {
	childrenByParent := map[uint32][]uint32{
		1: {2, 3},
	}
	creationTime := func(pid uint32) (int64, bool) {
		switch pid {
		case 1:
			return 100, true
		case 2:
			return 150, true // started after the root: a real child
		case 3:
			return 50, true // started before the root: a recycled PID
		default:
			return 0, false
		}
	}

	order := orderProcessTree(1, childrenByParent, creationTime)
	want := []uint32{1, 2}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("orderProcessTree() = %v, want %v", order, want)
	}
}
