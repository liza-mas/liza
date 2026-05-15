package commands

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/ops"
)

// ResumeCommand resumes the Liza system and prints the result to stdout.
// Delegates business logic to ops.Resume.
func ResumeCommand(projectRoot, changedBy string) error {
	result, err := ops.Resume(projectRoot, changedBy)
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}

	fmt.Println("System resumed")
	fmt.Printf("  Resumed from: %s\n", result.ResumedFrom)
	fmt.Printf("  Changed by: %s\n", result.ChangedBy)

	if sa := result.SprintAdvanced; sa != nil {
		fmt.Println()
		fmt.Printf("  Sprint advanced: %s → %s\n", sa.ArchivedSprintID, sa.NewSprintID)
		if len(sa.CarriedTasks) > 0 {
			fmt.Printf("  Carried tasks: %v\n", sa.CarriedTasks)
		}
	}
	if result.TransitionsExecuted > 0 {
		fmt.Printf("  Transitions executed: %d (child tasks created)\n", result.TransitionsExecuted)
	}
	if result.TransitionError != "" {
		fmt.Printf("  ⚠️  Transition error: %s\n", result.TransitionError)
	}

	// Clear any provider-scoped stop signals so restarted agents aren't immediately blocked.
	var clearErrors []string
	if matches, err := filepath.Glob(agent.QuotaSignalGlob(projectRoot)); err == nil {
		for _, m := range matches {
			provider := agent.ProviderFromSignalFile(m)
			if clearErr := agent.ClearQuotaSignal(projectRoot, provider); clearErr == nil {
				fmt.Printf("  Cleared quota signal for provider: %s\n", provider)
			} else {
				clearErrors = append(clearErrors, fmt.Sprintf("quota/%s: %v", provider, clearErr))
			}
		}
	}
	if matches, err := filepath.Glob(agent.ProviderUnavailableSignalGlob(projectRoot)); err == nil {
		for _, m := range matches {
			provider := agent.ProviderFromUnavailableSignalFile(m)
			if clearErr := agent.ClearProviderUnavailableSignal(projectRoot, provider); clearErr == nil {
				fmt.Printf("  Cleared provider-unavailable signal for provider: %s\n", provider)
			} else {
				clearErrors = append(clearErrors, fmt.Sprintf("unavailable/%s: %v", provider, clearErr))
			}
		}
	}
	if len(clearErrors) > 0 {
		fmt.Printf("  Warning: failed to clear provider signals: %s\n", strings.Join(clearErrors, "; "))
	}

	fmt.Println()
	fmt.Println("Agents will resume at their next check.")
	return nil
}
