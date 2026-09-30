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
	return firstError(ValidationPrerequisiteViolations(commands, prerequisites))
}

// ValidationPrerequisiteViolations returns every defect
// ValidateValidationPrerequisites checks, in the order it checks them, so the
// first is the error it returns.
func ValidationPrerequisiteViolations(commands []string, prerequisites []ValidationPrerequisite) []error {
	if len(prerequisites) == 0 {
		return nil
	}
	var errs []error
	// Two independent constraints. No commands with declarations present is a
	// count mismatch, so it needs no check of its own.
	if len(prerequisites) != len(commands) {
		errs = append(errs, fmt.Errorf("validation_prerequisites requires one declaration per validation command (maximum 64)"))
	}
	if len(commands) > 64 {
		errs = append(errs, fmt.Errorf("validation_prerequisites requires one declaration per validation command (maximum 64): validation has more than 64 commands"))
	}
	errs = append(errs, ValidationCommandViolations("validation", commands)...)
	identities := make(map[string]bool, len(commands))
	for i, command := range commands {
		if len(command) > 4096 || strings.ContainsRune(command, 0) {
			errs = append(errs, fmt.Errorf("validation[%d] exceeds bounds or contains NUL", i))
		}
		if _, exists := identities[command]; exists {
			errs = append(errs, fmt.Errorf("validation[%d] duplicates a canonical command", i))
		}
		identities[command] = false
	}
	bytes := 0
	bytesReported := false
	for i, prerequisite := range prerequisites {
		seen, exists := identities[prerequisite.Command]
		if !exists || seen {
			errs = append(errs, fmt.Errorf("validation_prerequisites[%d].command must identify a distinct canonical command", i))
		}
		if exists {
			identities[prerequisite.Command] = true
		}
		bytes += len(prerequisite.Command)
		if len(prerequisite.Env)+len(prerequisite.Executables)+len(prerequisite.Probes) == 0 {
			errs = append(errs, fmt.Errorf("validation_prerequisites[%d] requires at least one check", i))
		}
		for _, list := range []struct {
			name string
			size int
		}{
			{"env", len(prerequisite.Env)},
			{"executables", len(prerequisite.Executables)},
			{"probes", len(prerequisite.Probes)},
		} {
			if list.size > 64 {
				errs = append(errs, fmt.Errorf("validation_prerequisites[%d] exceeds the 64 checks per list limit (%s)", i, list.name))
			}
		}
		for j, name := range prerequisite.Env {
			bytes += len(name)
			if len(name) > 256 || !prerequisiteEnvName.MatchString(name) {
				errs = append(errs, fmt.Errorf("validation_prerequisites[%d].env[%d] must be a variable identifier of at most 256 bytes", i, j))
			}
		}
		for j, executable := range prerequisite.Executables {
			bytes += len(executable)
			if !validPrerequisiteString(executable) || strings.TrimSpace(executable) == "" {
				errs = append(errs, fmt.Errorf("validation_prerequisites[%d].executables[%d] is empty or invalid", i, j))
			}
		}
		for j, probe := range prerequisite.Probes {
			if len(probe) == 0 || len(probe) > 32 {
				errs = append(errs, fmt.Errorf("validation_prerequisites[%d].probes[%d] requires 1 to 32 argv entries", i, j))
			}
			for k, argument := range probe {
				bytes += len(argument)
				if !validPrerequisiteString(argument) || (k == 0 && strings.TrimSpace(argument) == "") {
					errs = append(errs, fmt.Errorf("validation_prerequisites[%d].probes[%d][%d] is invalid", i, j, k))
				}
			}
		}
		// Reported where the running total first crosses the limit, which is
		// where the fail-first order met it.
		if bytes > 65536 && !bytesReported {
			errs = append(errs, fmt.Errorf("validation_prerequisites exceeds the 65536 byte contract limit"))
			bytesReported = true
		}
	}
	return errs
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
