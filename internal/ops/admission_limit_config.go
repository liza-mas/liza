package ops

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
)

// AdmissionLimitsConfigKey is where admission limits are stored and read
// (ADR-0209).
const AdmissionLimitsConfigKey = "config.admission_limits"

// SetAdmissionLimitInput describes an operator write of one class.
type SetAdmissionLimitInput struct {
	Limit   models.AdmissionLimit
	Replace bool
	Reason  string
}

// SetAdmissionLimit stores one class under the shared config rule: compare and
// write in one transaction; replacing a different definition requires
// --replace with --reason. A new max_concurrent also reaches waiters already
// queued within about admissionLimitRefreshInterval; current holders keep
// their slots, and a queued waiter keeps the class it matched.
func SetAdmissionLimit(projectRoot string, input SetAdmissionLimitInput) (*ConfigSetResult, error) {
	if err := models.ValidateAdmissionLimit(input.Limit); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if input.Replace && strings.TrimSpace(input.Reason) == "" {
		return nil, &PreconditionError{Reason: "--replace requires a non-empty --reason"}
	}
	key := AdmissionLimitsConfigKey + "." + input.Limit.Class
	result := &ConfigSetResult{Key: key}
	err := db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
		limits := state.Config.AdmissionLimits
		i := slices.IndexFunc(limits, func(l models.AdmissionLimit) bool { return l.Class == input.Limit.Class })
		switch {
		case i < 0:
			state.Config.AdmissionLimits = append(limits, input.Limit)
			result.Outcome = "set"
		case limits[i].MaxConcurrent == input.Limit.MaxConcurrent && slices.Equal(limits[i].Match, input.Limit.Match):
			result.Outcome = "unchanged"
		case !input.Replace:
			result.Outcome = "conflict"
			return &PreconditionError{
				Reason:  key + " is already set differently; use --replace with --reason to replace it",
				Details: map[string]any{"key": key, "conflict": "existing_value"},
			}
		default:
			limits[i] = input.Limit
			result.Outcome = "replaced"
		}
		return nil
	})
	return finishAdmissionLimitWrite(result, input.Limit, input.Reason, err)
}

// RemoveAdmissionLimit deletes one class; removing an absent class is
// unchanged. Waiters already queued keep waiting for the slots they knew.
func RemoveAdmissionLimit(projectRoot, class, reason string) (*ConfigSetResult, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, &PreconditionError{Reason: "removing an admission limit requires a non-empty --reason"}
	}
	result := &ConfigSetResult{Key: AdmissionLimitsConfigKey + "." + class, Outcome: "unchanged"}
	err := db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
		before := len(state.Config.AdmissionLimits)
		state.Config.AdmissionLimits = slices.DeleteFunc(state.Config.AdmissionLimits, func(l models.AdmissionLimit) bool { return l.Class == class })
		if len(state.Config.AdmissionLimits) == 0 {
			state.Config.AdmissionLimits = nil
		}
		if len(state.Config.AdmissionLimits) != before {
			result.Outcome = "removed"
		}
		return nil
	})
	return finishAdmissionLimitWrite(result, models.AdmissionLimit{Class: class}, reason, err)
}

func finishAdmissionLimitWrite(result *ConfigSetResult, limit models.AdmissionLimit, reason string, err error) (*ConfigSetResult, error) {
	if err != nil && result.Outcome != "conflict" {
		result.Outcome = "failed"
	}
	mask := secretmask.New()
	log.Printf("config_set key=%s outcome=%s actor=%q max_concurrent=%d match=%q reason=%q",
		result.Key, result.Outcome, "operator", limit.MaxConcurrent, mask.MaskText(strings.Join(limit.Match, "\x1f")), boundPostWorktreeCmd(mask.MaskText(reason)))
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", result.Key, err)
	}
	return result, nil
}
