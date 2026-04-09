package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

func TestNewKeyMap_BindingsMatchExpectedKeys(t *testing.T) {
	km := NewKeyMap()

	tests := []struct {
		name     string
		binding  key.Binding
		wantKeys []string
	}{
		{"Spawn", km.Spawn, []string{"s"}},
		{"SpawnWith", km.SpawnWith, []string{"S"}},
		{"Terminate", km.Terminate, []string{"t"}},
		{"Pause", km.Pause, []string{"p"}},
		{"Resume", km.Resume, []string{"r"}},
		{"AddTask", km.AddTask, []string{"a"}},
		{"Checkpoint", km.Checkpoint, []string{"c"}},
		{"Yolo", km.Yolo, []string{"y"}},
		{"Help", km.Help, []string{"?"}},
		{"Quit", km.Quit, []string{"q"}},
		{"Stop", km.Stop, []string{"Q"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.binding.Keys()
			if len(got) != len(tt.wantKeys) {
				t.Fatalf("got %d keys, want %d", len(got), len(tt.wantKeys))
			}
			for i, k := range got {
				if k != tt.wantKeys[i] {
					t.Errorf("key[%d] = %q, want %q", i, k, tt.wantKeys[i])
				}
			}
		})
	}
}

func TestNewKeyMap_HelpTextMatchesSpec(t *testing.T) {
	km := NewKeyMap()

	// Spec §Footer Bar: [s] spawn  [p] pause  [r] resume  [a] add  [c] checkpoint  [?] help  [q] quit  [Q] stop
	tests := []struct {
		name     string
		binding  key.Binding
		wantKey  string
		wantDesc string
	}{
		{"Spawn", km.Spawn, "s", "spawn (default cli)"},
		{"SpawnWith", km.SpawnWith, "S", "spawn (pick cli)"},
		{"Terminate", km.Terminate, "t", "terminate"},
		{"Pause", km.Pause, "p", "pause"},
		{"Resume", km.Resume, "r", "resume"},
		{"AddTask", km.AddTask, "a", "add"},
		{"Checkpoint", km.Checkpoint, "c", "checkpoint"},
		{"Yolo", km.Yolo, "y", "yolo"},
		{"Help", km.Help, "?", "help"},
		{"Quit", km.Quit, "q", "quit"},
		{"Stop", km.Stop, "Q", "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.binding.Help()
			if h.Key != tt.wantKey {
				t.Errorf("help key = %q, want %q", h.Key, tt.wantKey)
			}
			if h.Desc != tt.wantDesc {
				t.Errorf("help desc = %q, want %q", h.Desc, tt.wantDesc)
			}
		})
	}
}

func TestShortHelp_ReturnsExpectedBindings(t *testing.T) {
	km := NewKeyMap()
	bindings := km.ShortHelp()

	// Verify footer order: s, S, t, p, r, a, c, y, ?, q, Q
	expectedKeys := []string{"s", "S", "t", "p", "r", "a", "c", "y", "?", "q", "Q"}
	if len(bindings) != len(expectedKeys) {
		t.Fatalf("ShortHelp() returned %d bindings, want %d", len(bindings), len(expectedKeys))
	}
	for i, b := range bindings {
		keys := b.Keys()
		if len(keys) == 0 {
			t.Fatalf("binding[%d] has no keys", i)
		}
		if keys[0] != expectedKeys[i] {
			t.Errorf("ShortHelp()[%d] key = %q, want %q", i, keys[0], expectedKeys[i])
		}
	}
}

func TestFullHelp_ReturnsNonEmptyGroups(t *testing.T) {
	km := NewKeyMap()
	groups := km.FullHelp()

	if len(groups) == 0 {
		t.Fatal("FullHelp() returned 0 groups")
	}

	// Verify each group is non-empty
	for i, group := range groups {
		if len(group) == 0 {
			t.Errorf("FullHelp()[%d] is empty", i)
		}
	}

	// Verify grouped by category: actions, system, navigation
	if len(groups) != 3 {
		t.Fatalf("FullHelp() returned %d groups, want 3 (actions, system, navigation)", len(groups))
	}

	// Total bindings across all groups must match ShortHelp count
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	shortCount := len(km.ShortHelp())
	if total != shortCount {
		t.Errorf("FullHelp() total bindings = %d, want %d (same as ShortHelp)", total, shortCount)
	}
}
