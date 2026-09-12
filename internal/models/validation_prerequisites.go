package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ValidationPrerequisite binds cheap session checks to one exact canonical command.
// Env contains names only; probes are argv vectors, never implicitly shell-split.
type ValidationPrerequisite struct {
	Command     string     `yaml:"command" json:"command"`
	Env         []string   `yaml:"env,omitempty" json:"env,omitempty"`
	Executables []string   `yaml:"executables,omitempty" json:"executables,omitempty"`
	Probes      [][]string `yaml:"probes,omitempty" json:"probes,omitempty"`
}

var prerequisiteEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateValidationPrerequisites accepts legacy tasks without declarations and
// rejects partial, ambiguous, vacuous, or unbounded protected-task contracts.
// Errors identify fields and indices without echoing command or probe contents.
func ValidateValidationPrerequisites(commands []string, prerequisites []ValidationPrerequisite) error {
	if len(prerequisites) == 0 {
		return nil
	}
	if len(commands) == 0 || len(commands) > 64 || len(prerequisites) != len(commands) {
		return fmt.Errorf("validation_prerequisites requires one declaration per validation command (maximum 64)")
	}
	if err := ValidateValidationCommands("validation", commands); err != nil {
		return err
	}
	identities := make(map[string]bool, len(commands))
	for i, command := range commands {
		if len(command) > 4096 || strings.ContainsRune(command, 0) {
			return fmt.Errorf("validation[%d] exceeds bounds or contains NUL", i)
		}
		if _, exists := identities[command]; exists {
			return fmt.Errorf("validation[%d] duplicates a canonical command", i)
		}
		identities[command] = false
	}
	bytes := 0
	for i, prerequisite := range prerequisites {
		seen, exists := identities[prerequisite.Command]
		if !exists || seen {
			return fmt.Errorf("validation_prerequisites[%d].command must identify a distinct canonical command", i)
		}
		identities[prerequisite.Command] = true
		bytes += len(prerequisite.Command)
		if len(prerequisite.Env)+len(prerequisite.Executables)+len(prerequisite.Probes) == 0 {
			return fmt.Errorf("validation_prerequisites[%d] requires at least one check", i)
		}
		if len(prerequisite.Env) > 64 || len(prerequisite.Executables) > 64 || len(prerequisite.Probes) > 64 {
			return fmt.Errorf("validation_prerequisites[%d] exceeds the 64 checks per list limit", i)
		}
		for j, name := range prerequisite.Env {
			bytes += len(name)
			if len(name) > 256 || !prerequisiteEnvName.MatchString(name) {
				return fmt.Errorf("validation_prerequisites[%d].env[%d] must be a variable identifier of at most 256 bytes", i, j)
			}
		}
		for j, executable := range prerequisite.Executables {
			bytes += len(executable)
			if !validPrerequisiteString(executable) || strings.TrimSpace(executable) == "" {
				return fmt.Errorf("validation_prerequisites[%d].executables[%d] is empty or invalid", i, j)
			}
		}
		for j, probe := range prerequisite.Probes {
			if len(probe) == 0 || len(probe) > 32 {
				return fmt.Errorf("validation_prerequisites[%d].probes[%d] requires 1 to 32 argv entries", i, j)
			}
			for k, argument := range probe {
				bytes += len(argument)
				if !validPrerequisiteString(argument) || (k == 0 && strings.TrimSpace(argument) == "") {
					return fmt.Errorf("validation_prerequisites[%d].probes[%d][%d] is invalid", i, j, k)
				}
			}
		}
		if bytes > 65536 {
			return fmt.Errorf("validation_prerequisites exceeds the 65536 byte contract limit")
		}
	}
	return nil
}

func validPrerequisiteString(value string) bool {
	return len(value) <= 4096 && !strings.ContainsRune(value, 0)
}

// ValidationPrerequisiteDigest binds evidence to the full command and check
// contract. Callers validate first; the JSON encoding is total for these types.
func ValidationPrerequisiteDigest(commands []string, prerequisites []ValidationPrerequisite) string {
	payload, _ := json.Marshal(struct {
		Commands      []string                 `json:"commands"`
		Prerequisites []ValidationPrerequisite `json:"prerequisites"`
	}{commands, prerequisites})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// CloneValidationPrerequisites prevents producers from sharing mutable check
// slices with persisted tasks or generated children.
func CloneValidationPrerequisites(prerequisites []ValidationPrerequisite) []ValidationPrerequisite {
	cloned := slices.Clone(prerequisites)
	for i := range cloned {
		cloned[i].Env = slices.Clone(cloned[i].Env)
		cloned[i].Executables = slices.Clone(cloned[i].Executables)
		cloned[i].Probes = slices.Clone(cloned[i].Probes)
		for j := range cloned[i].Probes {
			cloned[i].Probes[j] = slices.Clone(cloned[i].Probes[j])
		}
	}
	return cloned
}
