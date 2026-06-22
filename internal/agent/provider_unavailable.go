package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

type providerUnavailablePattern struct {
	Provider string
	Needles  []string
}

var providerUnavailablePatterns = []providerUnavailablePattern{
	{
		Provider: "codex",
		Needles:  []string{"Codex cannot access session files", ".codex/sessions", "permission denied"},
	},
	{
		Provider: "codex",
		Needles:  []string{"thread/start failed", "error creating thread", ".codex/sessions"},
	},
}

// ProviderUnavailable holds details about a provider startup/readiness failure.
type ProviderUnavailable struct {
	Provider string
	Message  string
}

// DetectProviderUnavailable scans agent output for provider startup failures
// that make the provider unable to create a usable agent session.
func DetectProviderUnavailable(output, cliName string) *ProviderUnavailable {
	for _, p := range providerUnavailablePatterns {
		if p.Provider != cliName {
			continue
		}
		matched := true
		for _, needle := range p.Needles {
			if !strings.Contains(output, needle) {
				matched = false
				break
			}
		}
		if matched {
			return &ProviderUnavailable{
				Provider: p.Provider,
				Message:  representativeProviderFailureLine(output, p.Needles),
			}
		}
	}
	return nil
}

func representativeProviderFailureLine(output string, needles []string) string {
	for _, line := range strings.Split(output, "\n") {
		for _, needle := range needles {
			if strings.Contains(line, needle) {
				return line
			}
		}
	}
	return strings.TrimSpace(output)
}

const providerUnavailableSignalPrefix = "provider-unavailable-"

// ProviderUnavailableSignalPath returns the path to the provider-unavailable signal file.
func ProviderUnavailableSignalPath(projectRoot, provider string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), providerUnavailableSignalPrefix+provider)
}

// ProviderUnavailableSignalGlob returns a glob matching provider-unavailable signal files.
func ProviderUnavailableSignalGlob(projectRoot string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), providerUnavailableSignalPrefix+"*")
}

// ProviderFromUnavailableSignalFile extracts the provider name from a provider-unavailable signal path.
func ProviderFromUnavailableSignalFile(path string) string {
	return filepath.Base(path)[len(providerUnavailableSignalPrefix):]
}

// WriteProviderUnavailableSignal creates a signal file telling all supervisors
// for this provider to stop until the provider environment is repaired.
func WriteProviderUnavailableSignal(projectRoot, provider, message string) error {
	signalPath := ProviderUnavailableSignalPath(projectRoot, provider)
	content := fmt.Sprintf("provider: %s\ndetected: %s\nmessage: %s\n",
		provider,
		time.Now().UTC().Format(time.RFC3339),
		message,
	)
	return os.WriteFile(signalPath, []byte(content), 0644)
}

// CheckProviderUnavailableSignal returns true if a provider-unavailable signal exists.
func CheckProviderUnavailableSignal(projectRoot, provider string) bool {
	_, err := os.Stat(ProviderUnavailableSignalPath(projectRoot, provider))
	return err == nil
}

// ClearProviderUnavailableSignal removes a provider-unavailable signal file.
func ClearProviderUnavailableSignal(projectRoot, provider string) error {
	err := os.Remove(ProviderUnavailableSignalPath(projectRoot, provider))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// LogProviderUnavailableAlert appends a provider-unavailable alert to alerts.log.
func LogProviderUnavailableAlert(projectRoot string, pu *ProviderUnavailable) error {
	return LogAlert(projectRoot, "🚨", "PROVIDER UNAVAILABLE", pu.Provider+": "+pu.Message)
}

// LogProviderUnavailableSpawnBlockedAlert appends an alert when a provider-unavailable signal blocks spawn.
func LogProviderUnavailableSpawnBlockedAlert(projectRoot, provider, role string) error {
	message := fmt.Sprintf("%s: refused to spawn %s while provider-unavailable signal is set; repair the provider, then delete the flag file or run %s then %s before spawning again", provider, role, brand.Command("pause"), brand.Command("resume"))
	return LogAlert(projectRoot, "🚨", "PROVIDER UNAVAILABLE SPAWN BLOCKED", message)
}
