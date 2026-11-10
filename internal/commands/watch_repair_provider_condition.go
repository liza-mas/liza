package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/atomicfile"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
)

// A provider condition is one continuous period in which a provider's quota
// signal blocks auto repair from starting agents that some watcher had work
// for (issue #115). Its record under .liza/ is shared by every watcher of the
// project and survives watcher restarts and the signal's removal, so the
// condition is announced once per quota event (NO USABLE PROVIDER) and its end
// once (PROVIDER BLOCK LIFTED), with its start, duration and the roles it held.
//
// Quota events are generations of the signal file
// (providersignal.QuotaSignalEvent), not its expiry: a re-raise announcing the
// same reset is a new event, and a recovery attempt that changes when starts
// may resume is not. The record is written only on transitions (open, new
// event, newly held work, close), never on a steady tick.

// providerConditionFilePrefix names the record files in the project's .liza
// directory; it matches neither quota nor provider-unavailable signal globs.
const providerConditionFilePrefix = "auto-repair-provider-blocked-"

// providerConditionLockTimeout bounds a watch tick's wait on one record. Past
// it the provider is left for the next tick: nothing is alerted and nothing is
// lost. A variable only so tests can shorten it.
var providerConditionLockTimeout = 2 * time.Second

// autoRepairProviderConditionWarningKey holds the last record warning printed,
// so a persistent failure warns once rather than on every tick.
const autoRepairProviderConditionWarningKey = "auto-repair-provider-condition-warning:"

// autoRepairQuotaEvent reads a provider's current quota event. A variable so
// tests can script what a watcher observes under the record lock.
var autoRepairQuotaEvent = providersignal.QuotaSignalEvent

// providerCondition is the durable record of one open provider condition.
type providerCondition struct {
	Provider string `json:"provider"`
	// Condition identifies the condition: when it was opened.
	Condition string `json:"condition"`
	// Started is the first observed event's detection, else the open time.
	Started time.Time `json:"started"`
	// Event is the latest observed quota event's generation.
	Event         string    `json:"event"`
	EventDetected time.Time `json:"event_detected"`
	Message       string    `json:"message,omitempty"`
	// Events counts the quota events observed during the condition. A watcher
	// can miss intermediate rewrites, so it is a lower bound.
	Events int `json:"events"`
	// Held lists the work starts were refused for, as role[#item]@cli.
	Held []string `json:"held"`
}

// providerConditions is what one tick's reconciliation decided.
type providerConditions struct {
	// open holds the condition each provider is in after this tick.
	open map[string]providerCondition
	// until holds, for each open provider, until when it blocks starts as read
	// under the record lock (zero: unknown), so an alert keyed to the observed
	// event states that event's deadline, not the pre-lock snapshot's.
	until map[string]time.Time
	// skipped providers could not be reconciled this tick; their alert state
	// is left as it was.
	skipped map[string]bool
	// lifted are the PROVIDER BLOCK LIFTED alerts, already written.
	lifted   []Alert
	warnings []string
}

// quotaProvider is the provider a CLI's quota signal is filed under.
func quotaProvider(cli string) string {
	return agentlaunch.ACPXAgentName(cli)
}

func providerConditionPath(projectRoot, provider string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), providerConditionFilePrefix+provider+".json")
}

// providerConditionProviders returns the providers that have a record file.
// The glob also matches the record lock's owner file
// (<record>.lock.owner.json); provider names have no dot, so it is skipped.
func providerConditionProviders(projectRoot string) []string {
	matches, _ := filepath.Glob(filepath.Join(paths.New(projectRoot).LizaDir(), providerConditionFilePrefix+"*.json")) // the pattern is well formed
	providers := make([]string, 0, len(matches))
	for _, match := range matches {
		provider := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(match), providerConditionFilePrefix), ".json")
		if provider != "" && !strings.Contains(provider, ".") {
			providers = append(providers, provider)
		}
	}
	return providers
}

// heldWork names blocked work in a record.
func heldWork(work ProviderBlockedRoleWork) string {
	name := work.Role
	if work.Item > 0 {
		name += "#" + strconv.Itoa(work.Item)
	}
	return name + "@" + work.CLI
}

// reconcileProviderConditions brings every provider's record up to date.
// candidates is this tick's blocked work, computed before any record lock: it
// names the providers to look at and the work they hold, but whether a provider
// blocks is re-read under its record lock, so a stale snapshot can neither
// reopen a condition another watcher closed nor close one it renewed. Records of
// providers absent from candidates are checked too, so a condition ends even
// when no work is waiting any more, or the watcher that opened it is gone.
//
// A lift is written to alertsLog before its record is removed, under the
// record lock: a crash between the two leaves the record, and the next tick
// rewrites the alert (its OnceKey suppresses the duplicate) and removes it.
func reconcileProviderConditions(projectRoot string, candidates []ProviderBlockedRoleWork, block ProviderBlock, alertsLog string, now time.Time) providerConditions {
	result := providerConditions{open: map[string]providerCondition{}, until: map[string]time.Time{}, skipped: map[string]bool{}}
	held := map[string][]string{}
	var providers []string
	for _, work := range candidates {
		provider := quotaProvider(work.CLI)
		if _, seen := held[provider]; !seen {
			providers = append(providers, provider)
		}
		if name := heldWork(work); !slices.Contains(held[provider], name) {
			held[provider] = append(held[provider], name)
		}
	}
	for _, provider := range providerConditionProviders(projectRoot) {
		if !slices.Contains(providers, provider) {
			providers = append(providers, provider)
		}
	}
	slices.Sort(providers)
	for _, provider := range providers {
		path := providerConditionPath(projectRoot, provider)
		err := filelock.New(path).WithTimeout(providerConditionLockTimeout).WithLockOperation("auto-repair-provider-condition", func() error {
			return reconcileProviderCondition(projectRoot, provider, path, held[provider], block, alertsLog, now, &result)
		})
		if err != nil {
			result.skipped[provider] = true
			result.warnings = append(result.warnings, fmt.Sprintf("auto repair provider condition for %s left for the next tick: %v (delete %s if it stays unreadable)", provider, err, filepath.Join(paths.ProjectDirName(), filepath.Base(path))))
		}
	}
	return result
}

// reconcileProviderCondition decides one provider's transition from what it
// observes under the record lock.
func reconcileProviderCondition(projectRoot, provider, path string, held []string, block ProviderBlock, alertsLog string, now time.Time, result *providerConditions) error {
	record, exists, err := readProviderCondition(path)
	if err != nil {
		return err
	}
	until, blocked := block(provider)
	if !blocked {
		if !exists {
			return nil
		}
		lifted := providerBlockLiftedAlert(record, now)
		var ledgerErr *alerts.LedgerUnavailableError
		if err := alerts.Write(alertsLog, lifted); err != nil && !errors.As(err, &ledgerErr) {
			return fmt.Errorf("write %s: %w", alerts.CategoryProviderBlockLifted, err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		result.lifted = append(result.lifted, lifted)
		return nil
	}
	event, present := autoRepairQuotaEvent(projectRoot, provider)
	if !present {
		// Removed since the block was read: decided on the next tick.
		if exists {
			result.open[provider], result.until[provider] = record, until
		}
		return nil
	}
	next, changed := record, false
	switch {
	case !exists:
		next = providerCondition{Provider: provider, Condition: now.Format(time.RFC3339Nano), Started: now, Events: 1}
		if !event.Detected.IsZero() {
			next.Started = event.Detected
		}
		next.Event, next.EventDetected, next.Message = event.Generation, event.Detected, event.Message
		changed = true
	case record.Event != event.Generation:
		next.Event, next.EventDetected, next.Message = event.Generation, event.Detected, event.Message
		next.Events++
		changed = true
	}
	next.Held = slices.Clone(next.Held)
	for _, name := range held {
		if !slices.Contains(next.Held, name) {
			next.Held = append(next.Held, name)
			changed = true
		}
	}
	slices.Sort(next.Held)
	if changed {
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return err
		}
		if err := atomicfile.Write(path, append(data, '\n')); err != nil {
			return err
		}
	}
	result.open[provider], result.until[provider] = next, until
	return nil
}

func readProviderCondition(path string) (providerCondition, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return providerCondition{}, false, nil
	}
	if err != nil {
		return providerCondition{}, false, err
	}
	var record providerCondition
	if err := json.Unmarshal(data, &record); err != nil {
		return providerCondition{}, false, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	if record.Condition == "" || record.Event == "" {
		return providerCondition{}, false, fmt.Errorf("parse %s: missing condition or event", filepath.Base(path))
	}
	return record, true, nil
}

// providerBlockLiftedAlert announces the end of a condition: auto repair may
// try to start agents on the provider again, which does not prove it has
// capacity (a recovery probe can still fail and open a new condition).
func providerBlockLiftedAlert(record providerCondition, now time.Time) Alert {
	message := fmt.Sprintf("%s: no longer blocks auto repair starts (observed %s); auto repair may start agents on it again, which does not prove the provider has capacity; blocked since %s (%s), %d quota event(s) observed, held %s",
		record.Provider, now.Format(time.RFC3339), record.Started.UTC().Format(time.RFC3339), now.Sub(record.Started).Truncate(time.Second), record.Events, strings.Join(record.Held, ", "))
	if record.Message != "" {
		message += "; latest: " + record.Message
	}
	return Alert{
		Timestamp: now,
		Level:     AlertLevelWarning,
		Category:  alerts.CategoryProviderBlockLifted.String(),
		Message:   message,
		OnceKey:   alerts.CategoryProviderBlockLifted.String() + "|" + record.Provider + "|" + record.Condition,
	}
}

// warnProviderConditions prints each distinct set of record warnings once.
func warnProviderConditions(config WatchConfig, warnings []string, now time.Time) {
	key := autoRepairProviderConditionWarningKey + strings.Join(warnings, "\n")
	if _, seen := config.StateCache[key]; seen && len(warnings) > 0 {
		return
	}
	for cached := range config.StateCache {
		if strings.HasPrefix(cached, autoRepairProviderConditionWarningKey) {
			delete(config.StateCache, cached)
		}
	}
	if len(warnings) == 0 {
		return
	}
	config.StateCache[key] = now
	for _, warning := range warnings {
		fmt.Fprintf(config.WarnWriter, "WARNING: %s\n", warning)
	}
}
