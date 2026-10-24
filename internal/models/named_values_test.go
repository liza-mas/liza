package models

import (
	"slices"
	"strings"
	"testing"
)

// The expectations below are written as literals, independent of the named
// constants they pin, so a changed constant value fails here.

func TestLegacyAttemptedListCapsAttemptAtTwo(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attempted []any
		want      int
	}{
		{"one prior attempt", []any{"a"}, 2},
		{"two prior attempts", []any{"a", "b"}, 2},
		{"five prior attempts", []any{"a", "b", "c", "d", "e"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			effective := &Task{Extra: map[string]any{"attempted": tc.attempted}}
			if got := effective.EffectiveAttempt(); got != tc.want {
				t.Fatalf("EffectiveAttempt = %d, want %d", got, tc.want)
			}
			migrated := &Task{Extra: map[string]any{"attempted": tc.attempted}}
			if !migrated.MigrateAttemptedField() || migrated.Attempt != tc.want {
				t.Fatalf("MigrateAttemptedField set Attempt %d, want %d", migrated.Attempt, tc.want)
			}
		})
	}
}

func TestProviderDiversityNeedsTwoApprovals(t *testing.T) {
	one := &Task{Approvals: []Approval{{Provider: "claude"}}}
	if one.HasProviderDiversity() {
		t.Fatal("one approval cannot be diverse")
	}
	two := &Task{Approvals: []Approval{{Provider: "claude"}, {Provider: "codex"}}}
	if !two.HasProviderDiversity() {
		t.Fatal("two distinct providers are diverse")
	}
}

func TestIsFullReviewCommitLengths(t *testing.T) {
	for length, want := range map[int]bool{39: false, 40: true, 41: false, 63: false, 64: true, 65: false} {
		if got := IsFullReviewCommit(strings.Repeat("a", length)); got != want {
			t.Errorf("IsFullReviewCommit(%d hex chars) = %v, want %v", length, got, want)
		}
	}
}

func TestValidationPrerequisiteBoundEdges(t *testing.T) {
	check := func(prerequisite ValidationPrerequisite) []string {
		var messages []string
		for _, err := range ValidationPrerequisiteViolations([]string{"check"}, []ValidationPrerequisite{prerequisite}) {
			messages = append(messages, err.Error())
		}
		return messages
	}
	repeated := func(n int, value string) []string {
		list := make([]string, n)
		for i := range list {
			list[i] = value
		}
		return list
	}
	for _, tc := range []struct {
		name         string
		prerequisite ValidationPrerequisite
		want         []string // complete ordered diagnostics
	}{
		{"64 env names", ValidationPrerequisite{Command: "check", Env: repeated(64, "VAR")}, nil},
		{"65 env names", ValidationPrerequisite{Command: "check", Env: repeated(65, "VAR")},
			[]string{"validation_prerequisites[0] exceeds the 64 checks per list limit (env)"}},
		{"256-byte env name", ValidationPrerequisite{Command: "check", Env: []string{strings.Repeat("A", 256)}}, nil},
		{"257-byte env name", ValidationPrerequisite{Command: "check", Env: []string{strings.Repeat("A", 257)}},
			[]string{"validation_prerequisites[0].env[0] must be a variable identifier of at most 256 bytes"}},
		{"32 argv entries", ValidationPrerequisite{Command: "check", Probes: [][]string{repeated(32, "a")}}, nil},
		{"33 argv entries", ValidationPrerequisite{Command: "check", Probes: [][]string{repeated(33, "a")}},
			[]string{"validation_prerequisites[0].probes[0] requires 1 to 32 argv entries"}},
		{"4096-byte argument", ValidationPrerequisite{Command: "check", Probes: [][]string{{"tool", strings.Repeat("a", 4096)}}}, nil},
		{"4097-byte argument", ValidationPrerequisite{Command: "check", Probes: [][]string{{"tool", strings.Repeat("a", 4097)}}},
			[]string{"validation_prerequisites[0].probes[0][1] is invalid"}},
		// Every bound broken at once pins the emission order: list size, env
		// name, then probe size and argument.
		{"all bounds broken", ValidationPrerequisite{
			Command: "check",
			Env:     append(repeated(65, "VAR"), strings.Repeat("A", 257)),
			Probes:  [][]string{repeated(33, "a"), {"tool", strings.Repeat("a", 4097)}},
		}, []string{
			"validation_prerequisites[0] exceeds the 64 checks per list limit (env)",
			"validation_prerequisites[0].env[65] must be a variable identifier of at most 256 bytes",
			"validation_prerequisites[0].probes[0] requires 1 to 32 argv entries",
			"validation_prerequisites[0].probes[1][1] is invalid",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := check(tc.prerequisite); !slices.Equal(got, tc.want) {
				t.Fatalf("diagnostics = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidationPrerequisiteCountAndByteEdges(t *testing.T) {
	messages := func(commands []string, prerequisites []ValidationPrerequisite) []string {
		var out []string
		for _, err := range ValidationPrerequisiteViolations(commands, prerequisites) {
			out = append(out, err.Error())
		}
		return out
	}
	commands := func(n int) []string {
		list := make([]string, n)
		for i := range list {
			list[i] = strings.Repeat("c", 10) + string(rune('A'+i/26)) + string(rune('a'+i%26))
		}
		return list
	}
	declare := func(list []string) []ValidationPrerequisite {
		out := make([]ValidationPrerequisite, len(list))
		for i, command := range list {
			out[i] = ValidationPrerequisite{Command: command, Env: []string{"VAR"}}
		}
		return out
	}
	if got := messages(commands(64), declare(commands(64))); got != nil {
		t.Fatalf("64 commands: diagnostics = %q, want none", got)
	}
	if got, want := messages(commands(65), declare(commands(65))), []string{
		"validation_prerequisites requires one declaration per validation command (maximum 64): validation has more than 64 commands",
	}; !slices.Equal(got, want) {
		t.Fatalf("65 commands: diagnostics = %q, want %q", got, want)
	}
	// A count mismatch and the cap break together: the mismatch is reported first.
	if got, want := messages(commands(65), declare(commands(1))), []string{
		"validation_prerequisites requires one declaration per validation command (maximum 64)",
		"validation_prerequisites requires one declaration per validation command (maximum 64): validation has more than 64 commands",
	}; !slices.Equal(got, want) {
		t.Fatalf("65 commands, 1 declaration: diagnostics = %q, want %q", got, want)
	}

	// Counted bytes: 15 commands of 4096, a final command of n bytes and one
	// 1-byte env name per declaration (16): 61440 + n + 16.
	byteTotal := func(n int) []string {
		var list []string
		for i := 0; i < 15; i++ {
			list = append(list, strings.Repeat("x", 4095)+string(rune('a'+i)))
		}
		list = append(list, strings.Repeat("y", n))
		prerequisites := make([]ValidationPrerequisite, len(list))
		for i, command := range list {
			prerequisites[i] = ValidationPrerequisite{Command: command, Env: []string{"V"}}
		}
		return messages(list, prerequisites)
	}
	if got := byteTotal(4080); got != nil {
		t.Fatalf("65536 counted bytes: diagnostics = %q, want none", got)
	}
	if got, want := byteTotal(4081), []string{"validation_prerequisites exceeds the 65536 byte contract limit"}; !slices.Equal(got, want) {
		t.Fatalf("65537 counted bytes: diagnostics = %q, want %q", got, want)
	}
}
