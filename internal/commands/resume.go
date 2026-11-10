package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/providersignal"
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

	// Report quota signals without removing any: an unexpired block stays, and
	// an expired one stays until its recovery probe proves the provider back.
	// Clear provider-unavailable signals so restarted agents aren't blocked.
	report := providersignal.ReportQuotaSignals(projectRoot)
	for _, held := range report.Held {
		fmt.Printf("  Kept quota signal for provider: %s until %s; delete %s to lift it earlier\n", held.Provider, held.Until.UTC().Format(time.RFC3339), held.File)
	}
	for _, recovering := range report.Recovering {
		fmt.Printf("  %s\n", recovering.Describe())
	}
	var clearErrors []string
	for _, cleared := range providersignal.ClearProviderUnavailableSignals(projectRoot) {
		if cleared.Err == nil {
			fmt.Printf("  Cleared provider-unavailable signal for provider: %s\n", cleared.Provider)
		} else {
			clearErrors = append(clearErrors, fmt.Sprintf("unavailable/%s: %v", cleared.Provider, cleared.Err))
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
