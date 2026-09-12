package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidationPrerequisitesCommandIdentity(t *testing.T) {
	commands := []string{"check first", "check other"}
	prerequisites := []ValidationPrerequisite{
		{Command: commands[0], Env: []string{"DATABASE_URL"}},
		{Command: commands[1], Executables: []string{"checker"}},
	}
	for _, ordered := range [][]string{commands, {commands[1], commands[0]}} {
		if err := ValidateValidationPrerequisites(ordered, prerequisites); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateValidationPrerequisites([]string{"check third", commands[1]}, prerequisites); err == nil {
		t.Fatal("same-length command edit retained a stale prerequisite association")
	}
	digest := ValidationPrerequisiteDigest(commands, prerequisites)
	changed := CloneValidationPrerequisites(prerequisites)
	changed[0].Env[0] = "OTHER_URL"
	if digest == ValidationPrerequisiteDigest(commands, changed) {
		t.Fatal("changed requirement retained evidence digest")
	}
	if digest != ValidationPrerequisiteDigest(commands, CloneValidationPrerequisites(prerequisites)) {
		t.Fatal("identical contract digest is unstable")
	}
}

func TestValidationPrerequisitesRejectMalformedContracts(t *testing.T) {
	cases := []struct {
		name     string
		commands []string
		checks   []ValidationPrerequisite
	}{
		{"missing command", nil, []ValidationPrerequisite{{Command: "check", Env: []string{"URL"}}}},
		{"missing declaration", []string{"check", "other"}, []ValidationPrerequisite{{Command: "check", Env: []string{"URL"}}}},
		{"duplicate canonical", []string{"check", "check"}, []ValidationPrerequisite{{Command: "check", Env: []string{"URL"}}, {Command: "check", Env: []string{"URL"}}}},
		{"duplicate declaration", []string{"check", "other"}, []ValidationPrerequisite{{Command: "check", Env: []string{"URL"}}, {Command: "check", Env: []string{"URL"}}}},
		{"unknown command", []string{"check"}, []ValidationPrerequisite{{Command: "other", Env: []string{"URL"}}}},
		{"vacuous", []string{"check"}, []ValidationPrerequisite{{Command: "check"}}},
		{"variable value", []string{"check"}, []ValidationPrerequisite{{Command: "check", Env: []string{"URL=SECRET_CANARY"}}}},
		{"variable injection", []string{"check"}, []ValidationPrerequisite{{Command: "check", Env: []string{"URL\nSECRET_CANARY"}}}},
		{"empty executable", []string{"check"}, []ValidationPrerequisite{{Command: "check", Executables: []string{""}}}},
		{"empty argv", []string{"check"}, []ValidationPrerequisite{{Command: "check", Probes: [][]string{{}}}}},
		{"empty argv0", []string{"check"}, []ValidationPrerequisite{{Command: "check", Probes: [][]string{{"", "SECRET_CANARY"}}}}},
		{"NUL argument", []string{"check"}, []ValidationPrerequisite{{Command: "check", Probes: [][]string{{"tool", "SECRET_CANARY\x00"}}}}},
		{"long argument", []string{"check"}, []ValidationPrerequisite{{Command: "check", Probes: [][]string{{"tool", strings.Repeat("a", 4097)}}}}},
		{"many checks", []string{"check"}, []ValidationPrerequisite{{Command: "check", Env: make([]string, 65)}}},
		{"many args", []string{"check"}, []ValidationPrerequisite{{Command: "check", Probes: [][]string{make([]string, 33)}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateValidationPrerequisites(tc.commands, tc.checks)
			if err == nil {
				t.Fatal("malformed contract accepted")
			}
			if strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatal("diagnostic exposed supplied content")
			}
		})
	}
	if err := ValidateValidationPrerequisites([]string{"check", "check"}, nil); err != nil {
		t.Fatalf("legacy compatibility: %v", err)
	}
	if err := ValidateValidationPrerequisites([]string{"check"}, []ValidationPrerequisite{{Command: "check", Probes: [][]string{{"tool", ""}}}}); err != nil {
		t.Fatalf("empty non-executable argv is valid: %v", err)
	}
}

func TestValidationPrerequisitesRoundTrip(t *testing.T) {
	checks := []ValidationPrerequisite{{Command: "check", Env: []string{"URL"}, Executables: []string{"tool"}, Probes: [][]string{{"tool", "--check"}}}}
	original := Task{Validation: []string{"check"}, ValidationPrerequisites: checks}
	for _, codec := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		{"yaml", yaml.Marshal, yaml.Unmarshal}, {"json", json.Marshal, json.Unmarshal},
	} {
		t.Run(codec.name, func(t *testing.T) {
			data, err := codec.marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Task
			if err := codec.unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded.ValidationPrerequisites, checks) {
				t.Fatal("prerequisites lost during serialization")
			}
			if _, exists := decoded.Extra["validation_prerequisites"]; exists {
				t.Fatal("typed contract stored in Extra")
			}
		})
	}
}

func TestValidationPrerequisitesLimits(t *testing.T) {
	checks := []ValidationPrerequisite{{Command: "check", Probes: [][]string{{"tool", strings.Repeat("a", 4096)}}}}
	if err := ValidateValidationPrerequisites([]string{"check"}, checks); err != nil {
		t.Fatalf("maximum-sized argument rejected: %v", err)
	}
	checks[0].Probes = [][]string{append([]string{"tool"}, make([]string, 31)...)}
	if err := ValidateValidationPrerequisites([]string{"check"}, checks); err != nil {
		t.Fatalf("maximum argv count rejected: %v", err)
	}
	checks[0].Probes = nil
	checks[0].Executables = make([]string, 16)
	for i := range checks[0].Executables {
		checks[0].Executables[i] = strings.Repeat("a", 4096)
	}
	if err := ValidateValidationPrerequisites([]string{"check"}, checks); err == nil || !strings.Contains(err.Error(), "65536 byte") {
		t.Fatalf("oversized aggregate contract accepted: %v", err)
	}
	commands := make([]string, 65)
	checks = make([]ValidationPrerequisite, 65)
	if err := ValidateValidationPrerequisites(commands, checks); err == nil || !strings.Contains(err.Error(), "maximum 64") {
		t.Fatalf("oversized command bundle accepted: %v", err)
	}
}

func TestValidationPrerequisitesCloneIsIndependent(t *testing.T) {
	original := []ValidationPrerequisite{{Command: "check", Env: []string{"URL"}, Executables: []string{"tool"}, Probes: [][]string{{"tool", "--check"}}}}
	cloned := CloneValidationPrerequisites(original)
	cloned[0].Env[0] = "OTHER"
	cloned[0].Executables[0] = "other"
	cloned[0].Probes[0][0] = "other"
	cloned[0].Probes = append(cloned[0].Probes, []string{"extra"})
	if original[0].Env[0] != "URL" || original[0].Executables[0] != "tool" || original[0].Probes[0][0] != "tool" || len(original[0].Probes) != 1 {
		t.Fatal("cloned contract aliases mutable source checks")
	}
	if CloneValidationPrerequisites(nil) != nil {
		t.Fatal("clone changed an undeclared contract")
	}
}
