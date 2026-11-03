package db

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// decodeState memoizes by content: both the decode that filled the memo and a
// copy served from it belong to the caller, and modifying either must not reach
// the next decode of the same bytes.
func TestDecodeStateMemoIsolatesCallers(t *testing.T) {
	data := []byte(marshalState(t, buildNestedState(t)))
	decode := func() *models.State {
		t.Helper()
		state, err := decodeState(data, "memo test")
		if err != nil {
			t.Fatalf("decodeState: %v", err)
		}
		return state
	}
	miss := decode()
	want := CloneState(miss)
	hit := decode()
	mutateEverything(miss)
	mutateEverything(hit)
	if !reflect.DeepEqual(decode(), want) {
		t.Fatal("modifying a decoded state changed what the next decode of the same bytes returns")
	}
}

// A new publication is a new key: changed bytes are decoded, never served from
// the memo of the previous ones.
func TestDecodeStateMemoKeysOnContent(t *testing.T) {
	data := marshalState(t, buildNestedState(t))
	if _, err := decodeState([]byte(data), "memo test"); err != nil {
		t.Fatalf("decodeState: %v", err)
	}
	invalid := strings.Replace(data, "\n", "\n\t: not yaml\n", 1)
	if _, err := decodeState([]byte(invalid), "memo test"); err == nil {
		t.Fatal("changed bytes were served from the memo instead of being decoded")
	}
}
