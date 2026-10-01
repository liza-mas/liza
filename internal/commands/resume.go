package commands

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/ops"
)

// ResumeCommand resumes the Liza system and prints the result to stdout.
// Delegates business logic to ops.Resume.
func ResumeCommand(projectRoot, changedBy string) error {
	result, err := ops.Resume(projectRoot, changedBy)
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}

	if result.SystemRemainsStopped {
		fmt.Println("HALT response acknowledged")
		fmt.Println("  System remains STOPPED")
	} else {
		fmt.Println("System resumed")
		fmt.Printf("  Resumed from: %s\n", result.ResumedFrom)
	}
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

	// Clear provider-scoped stop signals so restarted agents aren't immediately
	// blocked; a quota block whose expiry has not passed stays.
	sweep := agent.ClearExpiredQuotaSignals(projectRoot)
	for _, provider := range sweep.Cleared {
		fmt.Printf("  Cleared expired quota signal for provider: %s\n", provider)
	}
	for _, held := range sweep.Held {
		fmt.Printf("  Kept quota signal for provider: %s until %s; delete %s to lift it earlier\n", held.Provider, held.Until.UTC().Format(time.RFC3339), held.File)
	}
	clearErrors := sweep.Failures
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
	if result.SystemRemainsStopped {
		fmt.Printf("Run %q to enter RUNNING mode, then restart agent processes.\n", brand.Command("start"))
	} else {
		fmt.Println("Agents will resume at their next check.")
	}
	return nil
}
