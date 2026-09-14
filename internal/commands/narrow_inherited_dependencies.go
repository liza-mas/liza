package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// NarrowInheritedDependenciesWithAuthorityAndOptionsCommand narrows one
// producer's generated children using generation-fenced authority and prints
// the per-child outcome to stdout.
func NarrowInheritedDependenciesWithAuthorityAndOptionsCommand(projectRoot, producerID, transition string, selections []ops.NarrowSelection, reason string, authority models.AgentAuthority, request ops.LifecycleRequestOptions) error {
	result, err := ops.NarrowInheritedDependenciesWithAuthorityAndOptions(projectRoot, producerID, transition, selections, reason, authority, request)
	return printNarrowInheritedDependenciesResult(result, err)
}

func printNarrowInheritedDependenciesResult(result *ops.NarrowInheritedDependenciesResult, err error) error {
	if err != nil {
		return fmt.Errorf("narrow inherited dependencies: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("Narrowed inherited dependencies for %s (%s): narrowed %d, unchanged %d, skipped %d\n",
		result.ProducerID, result.Transition, result.Narrowed, result.Unchanged, result.Skipped)
	for _, child := range result.Children {
		switch child.Action {
		case "narrowed":
			fmt.Printf("  %s: -%d +%d -> %d dependencies\n", child.TaskID, len(child.RemovedDependencies), len(child.AddedDependencies), len(child.CanonicalDependencies))
			if len(child.RemovedDependencies) > 0 {
				fmt.Printf("    removed: %s\n", strings.Join(child.RemovedDependencies, ", "))
			}
			if len(child.AddedDependencies) > 0 {
				fmt.Printf("    added: %s\n", strings.Join(child.AddedDependencies, ", "))
			}
		case "skipped":
			fmt.Printf("  %s: skipped (%s)\n", child.TaskID, child.SkipReason)
		default:
			fmt.Printf("  %s: unchanged\n", child.TaskID)
		}
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	return nil
}
