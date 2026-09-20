package commands

import (
	"fmt"
	"sort"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/ops"
)

// RepairAcceptanceCommitsCommand restores acceptance evidence whose commits an
// integration-branch rewrite orphaned, and prints an audit of what it changed
// and what it refused.
//
// Replacements are derived by content identity inside ops; this command takes
// no commit arguments on purpose. Accepting one would let any commit be
// declared reviewed.
func RepairAcceptanceCommitsCommand(projectRoot, statePath, integrationRef string, dryRun bool) error {
	bb := db.For(statePath)

	if integrationRef == "" {
		state, err := bb.Read()
		if err != nil {
			return fmt.Errorf("read state: %w", err)
		}
		integrationRef = state.Config.IntegrationBranch
		if integrationRef == "" {
			return fmt.Errorf("no integration branch configured; pass --integration")
		}
	}

	if dryRun {
		result, err := ops.PlanAcceptanceCommitRepair(bb, projectRoot, integrationRef)
		if err != nil {
			return fmt.Errorf("plan acceptance commit repair: %w", err)
		}
		printAcceptanceCommitRepair(result, true)
		return nil
	}

	result, err := ops.RepairAcceptanceCommits(bb, projectRoot, integrationRef)
	if err != nil {
		return fmt.Errorf("repair acceptance commits: %w", err)
	}
	printAcceptanceCommitRepair(result, false)
	return nil
}

func printAcceptanceCommitRepair(result ops.RepairAcceptanceCommitsResult, dryRun bool) {
	verb := "Repaired"
	if dryRun {
		verb = "Would repair"
	}

	if len(result.Repaired) == 0 && len(result.Skipped) == 0 {
		fmt.Println("No orphaned acceptance evidence: every merged planning parent is reachable from integration.")
		return
	}

	for _, remap := range result.Repaired {
		fmt.Printf("%s %s (%d history entries)\n", verb, remap.TaskID, remap.HistoryEntries)
		olds := make([]string, 0, len(remap.Replaced))
		for old := range remap.Replaced {
			olds = append(olds, old)
		}
		sort.Strings(olds)
		for _, old := range olds {
			fmt.Printf("    %s -> %s\n", ops.ShortCommit(old), ops.ShortCommit(remap.Replaced[old]))
		}
	}

	if len(result.Skipped) > 0 {
		ids := make([]string, 0, len(result.Skipped))
		for id := range result.Skipped {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		fmt.Println("Skipped:")
		for _, id := range ids {
			fmt.Printf("    %s: %s\n", id, result.Skipped[id])
		}
	}
}
