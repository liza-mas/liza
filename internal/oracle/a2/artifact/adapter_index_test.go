//go:build linux

package artifact

import "github.com/liza-mas/liza/internal/pairingindex"

func activateIndexes(repo string) error {
	restore := pairingindex.SetIndexBinaryForTest("/fixture/bin/agent-cli")
	defer restore()
	plan, err := pairingindex.PlanActivation(pairingindex.ActivationPlanOptions{
		RepoRoot: repo, EnableStacklit: true, EnableScip: true, EnableFunctionalClusters: true,
	})
	if err != nil {
		return err
	}
	_, err = pairingindex.InstallActivation(plan.Install)
	return err
}

func activateCollision(repo string) error {
	_, err := pairingindex.InstallActivation(pairingindex.InstallActivationOptions{RepoRoot: repo, EnableStacklit: true})
	return err
}
