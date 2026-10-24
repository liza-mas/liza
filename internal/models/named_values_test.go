package models

import (
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
	check := func(prerequisite ValidationPrerequisite) []error {
		return ValidationPrerequisiteViolations([]string{"check"}, []ValidationPrerequisite{prerequisite})
	}
	envs := func(n int) []string {
		names := make([]string, n)
		for i := range names {
			names[i] = "VAR"
		}
		return names
	}
	for _, tc := range []struct {
		name         string
		prerequisite ValidationPrerequisite
		wantErr      string
	}{
		{"64 env names", ValidationPrerequisite{Command: "check", Env: envs(64)}, ""},
		{"65 env names", ValidationPrerequisite{Command: "check", Env: envs(65)}, "exceeds the 64 checks per list limit (env)"},
		{"256-byte env name", ValidationPrerequisite{Command: "check", Env: []string{strings.Repeat("A", 256)}}, ""},
		{"257-byte env name", ValidationPrerequisite{Command: "check", Env: []string{strings.Repeat("A", 257)}}, "at most 256 bytes"},
		{"32 argv entries", ValidationPrerequisite{Command: "check", Probes: [][]string{append([]string{"tool"}, envs(31)...)}}, ""},
		{"33 argv entries", ValidationPrerequisite{Command: "check", Probes: [][]string{append([]string{"tool"}, envs(32)...)}}, "requires 1 to 32 argv entries"},
		{"4096-byte argument", ValidationPrerequisite{Command: "check", Probes: [][]string{{"tool", strings.Repeat("a", 4096)}}}, ""},
		{"4097-byte argument", ValidationPrerequisite{Command: "check", Probes: [][]string{{"tool", strings.Repeat("a", 4097)}}}, "is invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			joined := ""
			for _, err := range check(tc.prerequisite) {
				joined += err.Error() + "\n"
			}
			if tc.wantErr == "" && joined != "" {
				t.Fatalf("unexpected violations: %s", joined)
			}
			if tc.wantErr != "" && !strings.Contains(joined, tc.wantErr) {
				t.Fatalf("violations %q lack %q", joined, tc.wantErr)
			}
		})
	}
}

func TestValidationPrerequisiteCountAndByteEdges(t *testing.T) {
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
	if errs := ValidationPrerequisiteViolations(commands(64), declare(commands(64))); len(errs) != 0 {
		t.Fatalf("64 commands refused: %v", errs)
	}
	var found bool
	for _, err := range ValidationPrerequisiteViolations(commands(65), declare(commands(65))) {
		found = found || strings.Contains(err.Error(), "validation has more than 64 commands")
	}
	if !found {
		t.Fatal("65 commands accepted")
	}

	// Counted bytes: 15 commands of 4096, a final command of n bytes and one
	// 1-byte env name per declaration (16): 61440 + n + 16.
	byteTotal := func(n int) []error {
		var list []string
		for i := 0; i < 15; i++ {
			list = append(list, strings.Repeat("x", 4095)+string(rune('a'+i)))
		}
		list = append(list, strings.Repeat("y", n))
		prerequisites := make([]ValidationPrerequisite, len(list))
		for i, command := range list {
			prerequisites[i] = ValidationPrerequisite{Command: command, Env: []string{"V"}}
		}
		return ValidationPrerequisiteViolations(list, prerequisites)
	}
	if errs := byteTotal(4080); len(errs) != 0 {
		t.Fatalf("65536 counted bytes refused: %v", errs)
	}
	var over bool
	for _, err := range byteTotal(4081) {
		over = over || strings.Contains(err.Error(), "exceeds the 65536 byte contract limit")
	}
	if !over {
		t.Fatal("65537 counted bytes accepted")
	}
}
