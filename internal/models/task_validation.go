package models

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/taskkind"
)

// validKinds is the registry of non-empty OutputEntry.Kind / Task.Kind values.
// Empty kind remains valid and behaviorally inert for backward compatibility.
var validKinds = map[string]struct{}{
	taskkind.PreCommitBootstrap: {},
}

// DestructiveDBAllowMarker is the legacy break-glass marker accepted for
// persisted state compatibility.
const DestructiveDBAllowMarker = "LIZA_ALLOW_DESTRUCTIVE_DB=1"

// CurrentDestructiveDBAllowMarker returns the active brand's destructive DB
// break-glass marker. The legacy LIZA marker remains accepted for existing
// state files and commands.
func CurrentDestructiveDBAllowMarker() string {
	return brand.EnvName("ALLOW_DESTRUCTIVE_DB") + "=1"
}

// ValidateKind rejects unknown non-empty Kind values before they can bypass
// kind-based consumers such as task deduplication.
func ValidateKind(kind string) error {
	if kind == "" {
		return nil
	}
	if _, ok := validKinds[kind]; ok {
		return nil
	}
	return fmt.Errorf("unknown kind %q", kind)
}

// ValidateValidationCommands rejects malformed task-declared canonical
// validation commands before they enter state or prompts.
func ValidateValidationCommands(field string, commands []string) error {
	return firstError(ValidationCommandViolations(field, commands))
}

// ValidationCommandViolations returns every defect ValidateValidationCommands
// checks, in the order it checks them, so the first is the error it returns.
func ValidationCommandViolations(field string, commands []string) []error {
	var errs []error
	for i, command := range commands {
		trimmed := strings.TrimSpace(command)
		if trimmed == "" {
			// The shape checks below have nothing to judge.
			errs = append(errs, fmt.Errorf("%s[%d] must not be empty", field, i))
			continue
		}
		if command != trimmed {
			errs = append(errs, fmt.Errorf("%s[%d] must not have leading or trailing whitespace", field, i))
		}
		if strings.ContainsAny(command, "\r\n") {
			errs = append(errs, fmt.Errorf("%s[%d] must be a single-line command", field, i))
		}
	}
	return errs
}

func firstError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}

// HasLeadingDestructiveDBAllowMarker reports whether command begins with the
// explicit break-glass environment assignment required for destructive DB work.
func HasLeadingDestructiveDBAllowMarker(command string) bool {
	for _, marker := range acceptedDestructiveDBAllowMarkers() {
		if hasLeadingDestructiveDBAllowMarker(command, marker) ||
			hasLeadingDestructiveDBAllowMarker(command, "env "+marker) {
			return true
		}
	}
	return false
}

func acceptedDestructiveDBAllowMarkers() []string {
	current := CurrentDestructiveDBAllowMarker()
	if current == DestructiveDBAllowMarker {
		return []string{DestructiveDBAllowMarker}
	}
	// Keep accepting the legacy marker for existing state files and saved
	// commands, but advertise only the current brand marker in prompts/errors.
	return []string{current, DestructiveDBAllowMarker}
}

func hasLeadingDestructiveDBAllowMarker(command, prefix string) bool {
	if !strings.HasPrefix(command, prefix) {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(command, prefix))
	return rest != "" &&
		!strings.HasPrefix(rest, "#") &&
		!strings.ContainsAny(rest[:1], ";&|") &&
		len(command) > len(prefix) &&
		(command[len(prefix)] == ' ' || command[len(prefix)] == '\t')
}

// ValidateValidationSafety rejects unsafe validation metadata combinations
// before destructive DB work can be persisted into state or downstream prompts.
func ValidateValidationSafety(field string, commands []string, destructiveDB bool) error {
	return firstError(ValidationSafetyViolations(field, commands, destructiveDB))
}

// ValidationSafetyViolations returns every defect ValidateValidationSafety
// checks, in the order it checks them, so the first is the error it returns.
func ValidationSafetyViolations(field string, commands []string, destructiveDB bool) []error {
	errs := ValidationCommandViolations(field, commands)
	if !destructiveDB {
		return errs
	}
	if len(commands) == 0 {
		return append(errs, fmt.Errorf("%s destructive_db requires at least one validation command", field))
	}
	for i, command := range commands {
		if !HasLeadingDestructiveDBAllowMarker(command) {
			marker := CurrentDestructiveDBAllowMarker()
			errs = append(errs, fmt.Errorf("%s[%d] destructive_db requires command to start with %s or env %s", field, i, marker, marker))
		}
	}
	return errs
}
