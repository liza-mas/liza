package pairingindex

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

// ActivationPlanOptions selects which repo-root indexes the lifecycle hooks
// refresh. Callers pass the env gates they resolved; this package does not
// read them.
type ActivationPlanOptions struct {
	RepoRoot                 string
	EnableStacklit           bool
	EnableScip               bool
	EnableFunctionalClusters bool
	// ScipLanguages restricts SCIP planning to explicit languages. A listed
	// language without an indexable root is then an error. Pairing passes
	// --scip-search here.
	ScipLanguages []string
	// ScipPlanOverrides are raw --scip-search-plan values.
	ScipPlanOverrides []string
	// ScipLanguageFilter keeps only these detected languages. A filtered
	// language without an indexable root is reported in
	// ActivationPlan.Unindexable instead of failing, because MAS languages come
	// from config and a greenfield repository may not have sources yet. MAS
	// passes its configured scip_search languages here.
	ScipLanguageFilter []string
}

// MASActivationOptions resolves the process env gates into plan options for a
// MAS project whose configured scip_search languages are scipLanguages. MAS
// init and the orchestrator's startup repair share it so they plan the same
// script.
func MASActivationOptions(repoRoot string, scipLanguages []string) ActivationPlanOptions {
	return ActivationPlanOptions{
		RepoRoot:                 repoRoot,
		EnableStacklit:           stacklit.RuntimeEnabled(),
		EnableScip:               scipsearch.RuntimeEnabled(scipLanguages),
		EnableFunctionalClusters: functionalclusters.RuntimeEnabled(),
		ScipLanguageFilter:       scipLanguages,
	}
}

// ActivationPlan is what InstallActivation should install for a repository.
type ActivationPlan struct {
	Install InstallActivationOptions
	// Skips lists detected languages without an indexable root. It is empty
	// when ScipLanguageFilter is set; Unindexable reports those instead.
	Skips []scipsearch.PairingPlanSkip
	// Unindexable lists ScipLanguageFilter languages that no plan covers.
	Unindexable []string
}

// Active reports whether the plan refreshes any index.
func (p ActivationPlan) Active() bool {
	return p.Install.EnableStacklit || len(p.Install.ScipPlans) > 0
}

// PlanActivation resolves the repo-root index script inputs shared by Pairing
// init, MAS init and the orchestrator's startup activation check.
func PlanActivation(opts ActivationPlanOptions) (ActivationPlan, error) {
	plan := ActivationPlan{Install: InstallActivationOptions{
		RepoRoot:                 opts.RepoRoot,
		EnableStacklit:           opts.EnableStacklit,
		EnableFunctionalClusters: opts.EnableFunctionalClusters && opts.EnableStacklit,
	}}
	if !opts.EnableScip {
		return plan, nil
	}

	overrides, err := scipsearch.ParsePairingCommandOverrides(opts.RepoRoot, opts.ScipPlanOverrides)
	if err != nil {
		return ActivationPlan{}, fmt.Errorf("scip-search pairing plan failed: %w", err)
	}
	result, err := scipsearch.PlanPairingCommands(scipsearch.PairingPlanOptions{
		ProjectRoot:       opts.RepoRoot,
		ExplicitLanguages: opts.ScipLanguages,
		CommandOverrides:  overrides,
		SkipUnresolved:    len(opts.ScipLanguages) == 0,
	})
	if err != nil {
		return ActivationPlan{}, fmt.Errorf("scip-search pairing plan failed: %w", err)
	}
	if opts.ScipLanguageFilter == nil {
		plan.Skips = result.Skips
		plan.Install.ScipPlans = result.Plans
		return plan, nil
	}

	for _, scipPlan := range result.Plans {
		if slices.Contains(opts.ScipLanguageFilter, scipPlan.Language) {
			plan.Install.ScipPlans = append(plan.Install.ScipPlans, scipPlan)
		}
	}
	for _, language := range opts.ScipLanguageFilter {
		covered := slices.ContainsFunc(plan.Install.ScipPlans, func(p scipsearch.LanguageAggregatePlan) bool {
			return p.Language == language
		})
		if !covered {
			plan.Unindexable = append(plan.Unindexable, language)
		}
	}
	return plan, nil
}

// ActivationStatus describes installed lifecycle hooks against a plan.
type ActivationStatus string

const (
	// ActivationCurrent means the installed script, dispatcher and hooks match.
	ActivationCurrent ActivationStatus = "current"
	// ActivationMissing means the script, the dispatcher or a managed hook is absent.
	ActivationMissing ActivationStatus = "missing"
	// ActivationDrifted means an installed file differs from what the plan renders.
	ActivationDrifted ActivationStatus = "drifted"
)

// CheckActivation compares the installed activation with opts without
// changing anything. A drifted script means the hooks refresh a different set
// of indexes than the current configuration and repository layout call for.
func CheckActivation(opts InstallActivationOptions) (ActivationStatus, error) {
	hooks := opts.Hooks
	if len(hooks) == 0 {
		hooks = DefaultLifecycleHooks()
	}
	hooksDir, err := ResolveEffectiveHooksDir(opts.RepoRoot)
	if err != nil {
		return "", err
	}

	script, err := os.ReadFile(filepath.Join(hooksDir, scriptName()))
	if os.IsNotExist(err) {
		return ActivationMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", scriptName(), err)
	}
	dispatcher, err := os.ReadFile(filepath.Join(hooksDir, hookDispatcherName()))
	if os.IsNotExist(err) {
		return ActivationMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", hookDispatcherName(), err)
	}
	for _, hook := range hooks {
		managed, err := managedLifecycleHookExists(filepath.Join(hooksDir, hook))
		if os.IsNotExist(err) || (err == nil && !managed) {
			return ActivationMissing, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect %s hook: %w", hook, err)
		}
	}

	want, err := renderIndexScript(opts.renderOptions())
	if err != nil {
		return "", err
	}
	if string(script) != want || !dispatcherCurrent(string(dispatcher)) {
		return ActivationDrifted, nil
	}
	executable, err := managedFilesExecutable(hooksDir, hooks)
	if err != nil {
		return "", err
	}
	if !executable {
		return ActivationDrifted, nil
	}
	return ActivationCurrent, nil
}

// dispatcherCurrent accepts any baked coordinator path that still exists as an
// executable, rather than only the running binary's. A supervisor started from
// another path than init's, a go run build or a versioned install directory,
// would otherwise see drift and rewrite the hooks on every start.
func dispatcherCurrent(content string) bool {
	baked, ok := bakedIndexBinary(content)
	if !ok || content != managedHookDispatcherContent(baked) {
		return false
	}
	if baked == "" {
		// Nothing baked is only current when there is still nothing to bake.
		return indexBinary() == ""
	}
	info, err := os.Stat(baked)
	return err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0)
}

// managedFilesExecutable reports whether the script, the dispatcher and the
// hooks keep their execute bits, which a copy or restore can drop. Git skips a
// non-executable hook and the dispatcher a non-executable script, so matching
// content alone is not a working install. A symlinked hook is covered by its
// target, the dispatcher. Windows has no execute bits; Git for Windows runs
// hooks through sh.
func managedFilesExecutable(hooksDir string, hooks []string) (bool, error) {
	if runtime.GOOS == "windows" {
		return true, nil
	}
	for _, name := range append([]string{scriptName(), hookDispatcherName()}, hooks...) {
		info, err := os.Stat(filepath.Join(hooksDir, name))
		if err != nil {
			return false, fmt.Errorf("inspect %s: %w", name, err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			return false, nil
		}
	}
	return true, nil
}

// renderOptions derives the script inputs, enabling Functional Clusters only
// when both of its inputs, Stacklit and at least one SCIP index, are refreshed.
func (opts InstallActivationOptions) renderOptions() renderIndexScriptOptions {
	return renderIndexScriptOptions{
		RepoRoot:                 opts.RepoRoot,
		EnableStacklit:           opts.EnableStacklit,
		EnableFunctionalClusters: opts.EnableFunctionalClusters && opts.EnableStacklit && len(opts.ScipPlans) > 0,
		ScipPlans:                opts.ScipPlans,
	}
}
