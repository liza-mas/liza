package pairingindex

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

// ManagedHookMarker identifies hook files owned by pairing index plumbing.
const ManagedHookMarker = "# PAIRING-INDEX-HOOK: managed"

// ManagedIndexScriptMarker identifies pairing index scripts owned by this tool.
const ManagedIndexScriptMarker = "# PAIRING-INDEX-SCRIPT: managed"

const legacyManagedIndexScriptMarker = "# LIZA-PAIRING-INDEX-SCRIPT: managed"

// hookNameEnvVar carries the git hook name from the wrapper to the dispatcher.
//
// When the hook is a symlink to the dispatcher, the dispatcher reads the name
// from $0. The wrapper installed where symlinks are unavailable execs the
// dispatcher by its own name, so $0 says "liza-index-hook.sh" and the
// post-checkout file-checkout short-circuit would never fire. The wrapper
// therefore states the name explicitly.
const hookNameEnvVar = "PAIRING_INDEX_HOOK_NAME"

const stacklitArtifactName = "stacklit.json"
const stacklitInsightsArtifactName = "stacklit-insights.json"
const stacklitArchitectureArtifactName = "stacklit-architecture.json"
const functionalClustersArtifactName = "functional-clusters.json"
const defaultScriptName = "liza-index.sh"
const defaultHookDispatcherName = "liza-index-hook.sh"

var defaultLifecycleHooks = []string{"post-commit", "post-checkout", "post-merge", "post-rewrite"}
var stacklitArtifactExcludeMu sync.Mutex

func scriptName() string {
	binaryName := brand.RuntimeValues().BinaryName
	if binaryName == "liza" {
		return defaultScriptName
	}
	return binaryName + "-index.sh"
}

func hookDispatcherName() string {
	binaryName := brand.RuntimeValues().BinaryName
	if binaryName == "liza" {
		return defaultHookDispatcherName
	}
	return binaryName + "-index-hook.sh"
}

// DefaultLifecycleHooks returns the Git lifecycle hooks used for pairing index refresh.
func DefaultLifecycleHooks() []string {
	return append([]string(nil), defaultLifecycleHooks...)
}

// InstallHooksOptions configures lifecycle hook installation for one repository.
type InstallHooksOptions struct {
	RepoRoot string
	Hooks    []string
}

// InstallActivationOptions configures the combined pairing index activation hook
// setup for Stacklit and SCIP project-root refresh.
type InstallActivationOptions struct {
	RepoRoot                 string
	Hooks                    []string
	EnableStacklit           bool
	EnableFunctionalClusters bool
	ScipPlans                []scipsearch.LanguageAggregatePlan
}

// InstallActivationResult reports the installed script and lifecycle hooks.
type InstallActivationResult struct {
	HooksDir string
	Script   InstallIndexScriptResult
	Hooks    []HookInstallResult
}

// InstallHooksResult reports the effective hook directory and per-hook actions.
type InstallHooksResult struct {
	HooksDir string
	Hooks    []HookInstallResult
}

// HookInstallResult reports one installed or verified lifecycle hook wrapper.
type HookInstallResult struct {
	Hook   string
	Path   string
	Action HookAction
}

// HookAction describes what InstallLifecycleHooks did to one hook file.
type HookAction string

const (
	// HookActionInstalled means Liza wrote a missing managed hook wrapper.
	HookActionInstalled HookAction = "installed"
	// HookActionVerified means an existing wrapper already matched Liza's content.
	HookActionVerified HookAction = "verified"
	// HookActionUpdated means an existing Liza-managed wrapper was refreshed.
	HookActionUpdated HookAction = "updated"
)

// HookCollision identifies an existing non-Liza hook that must not be overwritten.
type HookCollision struct {
	Hook string
	Path string
}

// InstallIndexScriptOptions configures pairing index script installation for one repository.
type InstallIndexScriptOptions struct {
	RepoRoot                 string
	DisableStacklit          bool
	EnableFunctionalClusters bool
	ScipPlans                []scipsearch.LanguageAggregatePlan
}

// InstallIndexScriptResult reports the generated script location and action.
type InstallIndexScriptResult struct {
	Path   string
	Action HookAction
}

// HookCollisionError reports all non-Liza lifecycle hook collisions found during preflight.
type HookCollisionError struct {
	Collisions []HookCollision
}

func (e *HookCollisionError) Error() string {
	if e == nil || len(e.Collisions) == 0 {
		return fmt.Sprintf("%s-managed pairing index hook collision", brand.NameTitle)
	}

	parts := make([]string, 0, len(e.Collisions))
	for _, collision := range e.Collisions {
		parts = append(parts, fmt.Sprintf("%s at %s already exists and is not %s-managed", collision.Hook, collision.Path, brand.NameTitle))
	}
	return fmt.Sprintf("%s-managed pairing index hook collision: %s", brand.NameTitle, strings.Join(parts, "; "))
}

// InstallActivation installs or verifies the managed pairing index entrypoint
// and lifecycle hooks after preflighting collisions.
func InstallActivation(opts InstallActivationOptions) (InstallActivationResult, error) {
	hooks := opts.Hooks
	if len(hooks) == 0 {
		hooks = DefaultLifecycleHooks()
	}
	hooksDir, err := ResolveEffectiveHooksDir(opts.RepoRoot)
	if err != nil {
		return InstallActivationResult{}, err
	}
	result := InstallActivationResult{HooksDir: hooksDir}
	if err := ensureHooksDir(hooksDir); err != nil {
		return result, err
	}
	if err := rejectHookCollisions(hooksDir, hooks); err != nil {
		return result, err
	}
	if opts.EnableStacklit {
		if err := ensureStacklitArtifactCleanliness(opts.RepoRoot); err != nil {
			return result, err
		}
	}
	if err := ensureScipArtifactCleanliness(opts.RepoRoot, opts.ScipPlans); err != nil {
		return result, err
	}
	enableFunctionalClusters := opts.EnableFunctionalClusters && opts.EnableStacklit && len(opts.ScipPlans) > 0
	if enableFunctionalClusters {
		if err := ensureFunctionalClustersArtifactCleanliness(opts.RepoRoot); err != nil {
			return result, err
		}
	}

	scriptPath := filepath.Join(hooksDir, scriptName())
	content, err := renderIndexScript(renderIndexScriptOptions{
		RepoRoot:                 opts.RepoRoot,
		EnableStacklit:           opts.EnableStacklit,
		EnableFunctionalClusters: enableFunctionalClusters,
		ScipPlans:                opts.ScipPlans,
	})
	if err != nil {
		return result, err
	}
	action, err := installManagedIndexScript(scriptPath, content)
	if err != nil {
		return result, err
	}
	result.Script = InstallIndexScriptResult{Path: scriptPath, Action: action}
	if _, err := installManagedHookDispatcher(filepath.Join(hooksDir, hookDispatcherName())); err != nil {
		return result, err
	}

	for _, hook := range hooks {
		hookPath := filepath.Join(hooksDir, hook)
		action, err := installManagedHook(hookPath, hook)
		if err != nil {
			return result, err
		}
		result.Hooks = append(result.Hooks, HookInstallResult{
			Hook:   hook,
			Path:   hookPath,
			Action: action,
		})
	}
	return result, nil
}

// ResolveEffectiveHooksDir returns the hooks directory Git will use for repoRoot.
func ResolveEffectiveHooksDir(repoRoot string) (string, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}

	output, err := gitenv.Output(repoRoot, "rev-parse", "--git-path", "hooks/post-commit")
	if err != nil {
		return "", fmt.Errorf("resolve effective Git hook path: %w%s", err, outputSuffix(string(output)))
	}

	hookPath := strings.TrimSpace(string(output))
	if hookPath == "" {
		return "", fmt.Errorf("resolve effective Git hook path: git returned an empty path")
	}
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(repoRoot, hookPath)
	}
	return filepath.Clean(filepath.Dir(hookPath)), nil
}

// InstallLifecycleHooks installs or verifies Liza-managed pairing index wrappers.
func InstallLifecycleHooks(opts InstallHooksOptions) (InstallHooksResult, error) {
	hooks := opts.Hooks
	if len(hooks) == 0 {
		hooks = DefaultLifecycleHooks()
	}

	hooksDir, err := ResolveEffectiveHooksDir(opts.RepoRoot)
	if err != nil {
		return InstallHooksResult{}, err
	}
	result := InstallHooksResult{HooksDir: hooksDir}

	if err := ensureHooksDir(hooksDir); err != nil {
		return result, err
	}
	if err := rejectHookCollisions(hooksDir, hooks); err != nil {
		return result, err
	}
	if _, err := installManagedHookDispatcher(filepath.Join(hooksDir, hookDispatcherName())); err != nil {
		return result, err
	}

	for _, hook := range hooks {
		hookPath := filepath.Join(hooksDir, hook)
		action, err := installManagedHook(hookPath, hook)
		if err != nil {
			return result, err
		}
		result.Hooks = append(result.Hooks, HookInstallResult{
			Hook:   hook,
			Path:   hookPath,
			Action: action,
		})
	}
	return result, nil
}

// RenderIndexScript returns the managed Stacklit index script content for repoRoot.
func RenderIndexScript(repoRoot string) (string, error) {
	return renderIndexScript(renderIndexScriptOptions{RepoRoot: repoRoot, EnableStacklit: true})
}

type renderIndexScriptOptions struct {
	RepoRoot                 string
	EnableStacklit           bool
	EnableFunctionalClusters bool
	ScipPlans                []scipsearch.LanguageAggregatePlan
}

func renderIndexScript(opts renderIndexScriptOptions) (string, error) {
	var stacklitPlan stacklit.RuntimeCommandPlan
	if opts.EnableStacklit {
		plan, err := stacklit.PlanRuntimeCommand(opts.RepoRoot)
		if err != nil {
			return "", err
		}
		stacklitPlan = plan
	} else {
		repoRoot, err := filepath.Abs(opts.RepoRoot)
		if err != nil {
			return "", fmt.Errorf("resolve repository root: %w", err)
		}
		stacklitPlan.Dir = repoRoot
	}
	body := strings.Builder{}
	body.WriteString("#!/bin/sh\n")
	body.WriteString(ManagedIndexScriptMarker)
	body.WriteString("\nset -eu\n")
	body.WriteString("repo_root=")
	body.WriteString(shellQuote(stacklitPlan.Dir))
	body.WriteString("\n")
	body.WriteString(`run_ai=0
if [ "${1:-}" = "ai" ]; then
	run_ai=1
	shift
fi
`)
	for _, plan := range opts.ScipPlans {
		body.WriteString(renderScipCommand(plan))
	}
	if opts.EnableStacklit {
		stacklitGenerateCommand := shellCommand(stacklitPlan.Name, append(stacklitPlan.Args, "--parse-workers", "3"))
		body.WriteString(fmt.Sprintf(`if ! command -v %s >/dev/null 2>&1; then
	echo "%s: %s not found; skipping Stacklit refresh" >&2
else
	cd "$repo_root"
	code=0
	stacklit_refresh=1
	if [ -f stacklit.json ]; then
		%s diff -i stacklit.json >/dev/null || code=$?
		case "$code" in
			0) [ "$run_ai" = "1" ] || stacklit_refresh=0 ;;
			1) ;;
			*) echo "stacklit diff failed"; exit "$code" ;;
		esac
	fi
	if [ "$stacklit_refresh" = "1" ]; then
		echo "Stacklit Indexing..."
		insights_before=""
		if [ -f stacklit-insights.json ]; then
			insights_before="$(cksum stacklit-insights.json | awk '{print $1 ":" $2}')"
		fi
		%s
		%s init-insights -i stacklit.json -o stacklit-insights.json
		if [ "$run_ai" = "1" ]; then
			echo "Adding AI summary..."
			%s ai-summary
		fi
		insights_after=""
		if [ -f stacklit-insights.json ]; then
			insights_after="$(cksum stacklit-insights.json | awk '{print $1 ":" $2}')"
		fi
		if [ "$insights_before" != "$insights_after" ]; then
			%s
		fi
		echo "Wrote stacklit.json"
	fi
fi
`, shellWord(stacklitPlan.Name), scriptName(), stacklitPlan.Name, shellWord(stacklitPlan.Name), stacklitGenerateCommand, shellWord(stacklitPlan.Name), shellWord(stacklitPlan.Name), stacklitGenerateCommand))
	}
	if opts.EnableFunctionalClusters && opts.EnableStacklit && len(opts.ScipPlans) > 0 {
		body.WriteString(renderFunctionalClustersCommand(stacklitPlan.Dir, opts.ScipPlans))
	}
	return body.String(), nil
}

// InstallIndexScript installs or verifies the managed pairing index entrypoint.
func InstallIndexScript(opts InstallIndexScriptOptions) (InstallIndexScriptResult, error) {
	if !opts.DisableStacklit {
		if err := ensureStacklitArtifactCleanliness(opts.RepoRoot); err != nil {
			return InstallIndexScriptResult{}, err
		}
	}
	if err := ensureScipArtifactCleanliness(opts.RepoRoot, opts.ScipPlans); err != nil {
		return InstallIndexScriptResult{}, err
	}
	enableFunctionalClusters := opts.EnableFunctionalClusters && !opts.DisableStacklit && len(opts.ScipPlans) > 0
	if enableFunctionalClusters {
		if err := ensureFunctionalClustersArtifactCleanliness(opts.RepoRoot); err != nil {
			return InstallIndexScriptResult{}, err
		}
	}

	hooksDir, err := ResolveEffectiveHooksDir(opts.RepoRoot)
	if err != nil {
		return InstallIndexScriptResult{}, err
	}
	if err := ensureHooksDir(hooksDir); err != nil {
		return InstallIndexScriptResult{}, err
	}

	scriptPath := filepath.Join(hooksDir, scriptName())
	content, err := renderIndexScript(renderIndexScriptOptions{
		RepoRoot:                 opts.RepoRoot,
		EnableStacklit:           !opts.DisableStacklit,
		EnableFunctionalClusters: enableFunctionalClusters,
		ScipPlans:                opts.ScipPlans,
	})
	if err != nil {
		return InstallIndexScriptResult{}, err
	}
	action, err := installManagedIndexScript(scriptPath, content)
	if err != nil {
		return InstallIndexScriptResult{Path: scriptPath}, err
	}
	return InstallIndexScriptResult{Path: scriptPath, Action: action}, nil
}

func renderScipCommand(plan scipsearch.LanguageAggregatePlan) string {
	needsVar := "needs_" + shellIdentifier(plan.Language) + "_scip"
	missingVar := "missing_" + shellIdentifier(plan.Language) + "_scip"
	tmpVar := "tmp_" + shellIdentifier(plan.Language) + "_scip"
	cleanupFunc := "cleanup_" + shellIdentifier(plan.Language) + "_scip"
	aggregateFunc := "aggregate_" + shellIdentifier(plan.Language) + "_scip"
	indexedVar := "indexed_" + shellIdentifier(plan.Language) + "_roots"

	var freshness strings.Builder
	fmt.Fprintf(&freshness, `%s=0
if [ ! -f %s ]; then
	%s=1
`, needsVar, shellQuote(plan.OutputPath), needsVar)
	for _, indexPlan := range plan.IndexPlans {
		sourceExpr := scipFindSourceExpression(indexPlan.Language)
		if sourceExpr == "" {
			sourceExpr = "-type f"
		}
		sourceRoot := scipSourceRoot(indexPlan)
		exclusions := scipFindExclusions(indexPlan.Language, sourceRoot)
		fmt.Fprintf(&freshness, `elif find %s %s %s -newer %s -print -quit | grep -q .; then
	%s=1
`, shellQuote(sourceRoot), sourceExpr, exclusions, shellQuote(plan.OutputPath), needsVar)
	}
	freshness.WriteString("fi\n")

	var commandChecks strings.Builder
	checked := map[string]bool{"scip-search": true}
	fmt.Fprintf(&commandChecks, `%s=0
if [ "$%s" -eq 1 ] && ! command -v scip-search >/dev/null 2>&1; then
	echo "%s: scip-search not found; skipping %s SCIP refresh" >&2
	%s=1
fi
`, missingVar, needsVar, scriptName(), plan.Language, missingVar)
	for _, indexPlan := range plan.IndexPlans {
		if checked[indexPlan.Name] {
			continue
		}
		checked[indexPlan.Name] = true
		fmt.Fprintf(&commandChecks, `if [ "$%s" -eq 1 ] && ! command -v %s >/dev/null 2>&1; then
	echo "%s: %s not found; skipping %s SCIP refresh" >&2
	%s=1
fi
`, needsVar, shellWord(indexPlan.Name), scriptName(), indexPlan.Name, plan.Language, missingVar)
	}

	var commands strings.Builder
	fmt.Fprintf(&commands, "\t\t%s() {\n", aggregateFunc)
	fmt.Fprintf(&commands, "\t\t\tset -- scip-search aggregate-index --project-root %s\n", shellWord(plan.ProjectRoot))
	fmt.Fprintf(&commands, "\t\t\t%s=0\n", indexedVar)
	for i, indexPlan := range plan.IndexPlans {
		outputExpr := fmt.Sprintf("\"$%s/%s-%d.scip\"", tmpVar, plan.Language, i)
		fmt.Fprintf(&commands, "\t\t\tif %s; then\n", shellCommandWithOutput(indexPlan, outputExpr))
		fmt.Fprintf(&commands, "\t\t\t\tset -- \"$@\" --root %s --index %s\n", shellWord(indexPlan.Root), outputExpr)
		fmt.Fprintf(&commands, "\t\t\t\t%s=$((%s + 1))\n", indexedVar, indexedVar)
		fmt.Fprintf(&commands, "\t\t\telse\n\t\t\t\techo \"%s: failed to index %s SCIP root %s; skipping it\" >&2\n\t\t\tfi\n", scriptName(), plan.Language, shellWord(indexPlan.Root))
	}
	fmt.Fprintf(&commands, "\t\t\tif [ \"$%s\" -gt 0 ]; then\n", indexedVar)
	fmt.Fprintf(&commands, "\t\t\t\t\"$@\" --out %s\n", shellWord(plan.OutputPath))
	fmt.Fprintf(&commands, "\t\t\telif [ -f %s ]; then\n", shellQuote(plan.OutputPath))
	fmt.Fprintf(&commands, "\t\t\t\techo \"%s: no %s SCIP roots indexed; retaining existing index\" >&2\n", scriptName(), plan.Language)
	fmt.Fprintf(&commands, "\t\t\telse\n\t\t\t\techo \"%s: no %s SCIP roots indexed; no index produced\" >&2\n\t\t\tfi\n", scriptName(), plan.Language)
	commands.WriteString("\t\t}\n")
	fmt.Fprintf(&commands, "\t\t%s\n", aggregateFunc)

	return fmt.Sprintf(`%s%sif [ "$%s" -eq 1 ] && [ "$%s" -eq 0 ]; then
	%s="$(mktemp -d "${TMPDIR:-/tmp}/%s-scip-%s.XXXXXX")"
	%s() { rm -rf "$%s"; }
	trap %s EXIT HUP INT TERM
	cd %s
%s	%s
	trap - EXIT HUP INT TERM
fi
`, freshness.String(), commandChecks.String(), needsVar, missingVar, tmpVar, brand.RuntimeValues().BinaryName, shellIdentifier(plan.Language), cleanupFunc, tmpVar, cleanupFunc, shellQuote(plan.ProjectRoot), commands.String(), cleanupFunc)
}

func renderFunctionalClustersCommand(repoRoot string, plans []scipsearch.LanguageAggregatePlan) string {
	needsVar := "needs_functional_clusters"
	missingVar := "missing_functional_clusters"
	tmpVar := "tmp_functional_clusters"
	cleanupFunc := "cleanup_functional_clusters"

	var freshness strings.Builder
	fmt.Fprintf(&freshness, `%s=0
if [ ! -f %s ]; then
	%s=1
elif [ -f %s ] && [ %s -nt %s ]; then
	%s=1
`, needsVar, shellQuote(functionalClustersArtifactName), needsVar, shellQuote(stacklitArtifactName), shellQuote(stacklitArtifactName), shellQuote(functionalClustersArtifactName), needsVar)
	for _, plan := range plans {
		fmt.Fprintf(&freshness, `elif [ -f %s ] && [ %s -nt %s ]; then
	%s=1
`, shellQuote(plan.OutputPath), shellQuote(plan.OutputPath), shellQuote(functionalClustersArtifactName), needsVar)
	}
	freshness.WriteString("fi\n")

	var prerequisites strings.Builder
	fmt.Fprintf(&prerequisites, `%s=0
if [ "$%s" -eq 1 ] && ! command -v functional-clusters >/dev/null 2>&1; then
	echo "%s: functional-clusters not found; skipping Functional Clusters refresh" >&2
	%s=1
fi
if [ "$%s" -eq 1 ] && ! command -v stacklit >/dev/null 2>&1; then
	echo "%s: stacklit not found; skipping Functional Clusters refresh" >&2
	%s=1
fi
if [ "$%s" -eq 1 ] && ! command -v scip-search >/dev/null 2>&1; then
	echo "%s: scip-search not found; skipping Functional Clusters refresh" >&2
	%s=1
fi
if [ "$%s" -eq 1 ] && [ ! -f %s ]; then
	echo "%s: stacklit.json missing; skipping Functional Clusters refresh" >&2
	%s=1
fi
`, missingVar, needsVar, scriptName(), missingVar, needsVar, scriptName(), missingVar, needsVar, scriptName(), missingVar, needsVar, shellQuote(stacklitArtifactName), scriptName(), missingVar)
	for _, plan := range plans {
		fmt.Fprintf(&prerequisites, `if [ "$%s" -eq 1 ] && [ ! -f %s ]; then
	echo "%s: %s SCIP index missing; skipping Functional Clusters refresh" >&2
	%s=1
fi
`, needsVar, shellQuote(plan.OutputPath), scriptName(), plan.Language, missingVar)
	}

	var commands strings.Builder
	fmt.Fprintf(&commands, "\tstacklit export-architecture -i %s -o \"$%s/%s\"\n", shellQuote(stacklitArtifactName), tmpVar, stacklitArchitectureArtifactName)
	graphPathExprs := make([]string, 0, len(plans))
	for _, plan := range plans {
		graphPathExpr := fmt.Sprintf("\"$%s/%s-scip-graph.json\"", tmpVar, shellIdentifier(plan.Language))
		graphPathExprs = append(graphPathExprs, graphPathExpr)
		fmt.Fprintf(&commands, "\tscip-search graph-export --index %s -o %s\n", shellQuote(plan.OutputPath), graphPathExpr)
	}
	commands.WriteString("\tfunctional-clusters build")
	for _, graphPathExpr := range graphPathExprs {
		fmt.Fprintf(&commands, " --scip-graph %s", graphPathExpr)
	}
	fmt.Fprintf(&commands, " --stacklit-architecture \"$%s/%s\" -o %s\n", tmpVar, stacklitArchitectureArtifactName, shellQuote(functionalClustersArtifactName))

	return fmt.Sprintf(`cd %s
%s%sif [ "$%s" -eq 1 ] && [ "$%s" -eq 0 ]; then
	%s="$(mktemp -d "${TMPDIR:-/tmp}/%s-functional-clusters.XXXXXX")"
	%s() { rm -rf "$%s"; }
	trap %s EXIT HUP INT TERM
	echo "Functional Clusters Indexing..."
%s	echo "Wrote %s"
	trap - EXIT HUP INT TERM
	%s
fi
`, shellQuote(repoRoot), freshness.String(), prerequisites.String(), needsVar, missingVar, tmpVar, brand.RuntimeValues().BinaryName, cleanupFunc, tmpVar, cleanupFunc, commands.String(), functionalClustersArtifactName, cleanupFunc)
}

func scipFindSourceExpression(language string) string {
	switch language {
	case "go":
		return `-type f -name '*.go'`
	case "python":
		return `-type f -name '*.py'`
	case "typescript":
		return `-type f \( -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' \)`
	default:
		return ""
	}
}

func scipSourceRoot(plan scipsearch.RuntimeCommandPlan) string {
	switch plan.Language {
	case "go":
		if value, ok := argValueAfter(plan.Args, "--module-root"); ok {
			return value
		}
	case "python", "typescript":
		if value, ok := argValueAfter(plan.Args, "--cwd"); ok {
			return value
		}
	}
	return plan.Dir
}

func argValueAfter(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func scipFindExclusions(language, sourceRoot string) string {
	paths := []string{
		filepath.Join(sourceRoot, ".git", "*"),
		filepath.Join(sourceRoot, ".worktrees", "*"),
	}
	switch language {
	case "typescript":
		paths = append(paths, filepath.Join(sourceRoot, "node_modules", "*"))
	case "python":
		paths = append(paths,
			filepath.Join(sourceRoot, ".venv", "*"),
			filepath.Join(sourceRoot, "venv", "*"),
			filepath.Join(sourceRoot, "__pycache__", "*"),
		)
	}

	parts := make([]string, 0, len(paths)*2)
	for _, path := range paths {
		parts = append(parts, "-not", "-path", shellQuote(path))
	}
	return strings.Join(parts, " ")
}

func shellCommandWithOutput(plan scipsearch.RuntimeCommandPlan, outputExpr string) string {
	parts := []string{shellWord(plan.Name)}
	for _, arg := range plan.Args {
		if arg == plan.OutputPath {
			parts = append(parts, outputExpr)
			continue
		}
		parts = append(parts, shellWord(arg))
	}
	return strings.Join(parts, " ")
}

func shellIdentifier(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "index"
	}
	return b.String()
}

func shellCommand(name string, args []string) string {
	parts := []string{shellWord(name)}
	for _, arg := range args {
		parts = append(parts, shellWord(arg))
	}
	return strings.Join(parts, " ")
}

func shellWord(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>*?!()[]{}") {
		return value
	}
	return shellQuote(value)
}

func ensureStacklitArtifactCleanliness(repoRoot string) error {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}

	if err := ensureGeneratedArtifactCleanliness(repoRoot, stacklitArtifactName); err != nil {
		return err
	}
	return ensureGeneratedArtifactCleanliness(repoRoot, stacklitInsightsArtifactName)
}

func ensureFunctionalClustersArtifactCleanliness(repoRoot string) error {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	return ensureGeneratedArtifactCleanliness(repoRoot, functionalClustersArtifactName)
}

func ensureScipArtifactCleanliness(repoRoot string, plans []scipsearch.LanguageAggregatePlan) error {
	for _, plan := range plans {
		rel, err := filepath.Rel(repoRoot, plan.OutputPath)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
			return fmt.Errorf("scip-search output path %q is outside repository root %q", plan.OutputPath, repoRoot)
		}
		if err := ensureGeneratedArtifactCleanliness(repoRoot, filepath.ToSlash(rel)); err != nil {
			return err
		}
	}
	return nil
}

func ensureGeneratedArtifactCleanliness(repoRoot, artifact string) error {
	tracked, err := artifactTracked(repoRoot, artifact)
	if err != nil {
		return err
	}
	if tracked {
		return nil
	}
	ignored, err := artifactIgnored(repoRoot, artifact)
	if err != nil {
		return err
	}
	if ignored {
		return nil
	}
	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(artifact))); err == nil {
		return fmt.Errorf("%s is untracked and not ignored or privately excluded; commit it, add it to .gitignore, or add it to .git/info/exclude before enabling pairing index activation", artifact)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect repo-root %s: %w", artifact, err)
	}
	return ensureRepoPrivateExclude(repoRoot, artifact)
}

func artifactTracked(repoRoot, artifact string) (bool, error) {
	output, err := gitenv.CombinedOutput(repoRoot, "ls-files", "--error-unmatch", artifact)
	if err == nil {
		return true, nil
	}
	if gitUnmatchedPath(err, output) {
		return false, nil
	}
	return false, fmt.Errorf("inspect repo-root %s tracking: %w%s", artifact, err, outputSuffix(string(output)))
}

func artifactIgnored(repoRoot, artifact string) (bool, error) {
	output, err := gitenv.CombinedOutput(repoRoot, "check-ignore", artifact)
	if err == nil {
		return true, nil
	}
	if gitExitCode(err, 1) {
		return false, nil
	}
	return false, fmt.Errorf("inspect repo-root %s ignore state: %w%s", artifact, err, outputSuffix(string(output)))
}

func ensureRepoPrivateExclude(repoRoot, entry string) error {
	stacklitArtifactExcludeMu.Lock()
	defer stacklitArtifactExcludeMu.Unlock()

	gitDir, err := resolveGitDir(repoRoot)
	if err != nil {
		return err
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	return appendPrivateExcludeEntry(excludePath, entry)
}

func resolveGitDir(repoRoot string) (string, error) {
	output, err := gitenv.Output(repoRoot, "rev-parse", "--git-dir")
	if err != nil {
		return "", fmt.Errorf("resolve repository gitdir: %w%s", err, outputSuffix(string(output)))
	}
	gitDir := strings.TrimSpace(string(output))
	if gitDir == "" {
		return "", fmt.Errorf("resolve repository gitdir: git returned an empty path")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func appendPrivateExcludeEntry(excludePath, entry string) error {
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		return fmt.Errorf("create private exclude directory: %w", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read private exclude: %w", err)
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}

	next := append([]byte(nil), content...)
	if len(next) > 0 && next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	next = append(next, entry...)
	next = append(next, '\n')

	if err := os.WriteFile(excludePath, next, 0644); err != nil {
		return fmt.Errorf("write private exclude: %w", err)
	}
	return nil
}

func ensureHooksDir(hooksDir string) error {
	info, err := os.Stat(hooksDir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("effective Git hooks path %q is not a directory", hooksDir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect effective Git hooks directory %q: %w", hooksDir, err)
	}
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create effective Git hooks directory %q: %w", hooksDir, err)
	}
	return nil
}

func installManagedIndexScript(scriptPath, want string) (HookAction, error) {
	name := scriptName()
	current, err := os.ReadFile(scriptPath)
	if os.IsNotExist(err) {
		if err := os.WriteFile(scriptPath, []byte(want), 0755); err != nil {
			return "", fmt.Errorf("install %s: %w", name, err)
		}
		return HookActionInstalled, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	if !managedIndexScriptOwned(string(current)) {
		if looksLikeLegacyIndexScript(string(current)) {
			return "", fmt.Errorf("%s at %s already exists and appears to be a legacy managed index hook; move it aside and rerun %s: mv %s %s.backup", name, scriptPath, brand.Command("init"), scriptPath, scriptPath)
		}
		return "", fmt.Errorf("%s at %s already exists and is not managed by %s", name, scriptPath, brand.NameTitle)
	}
	if string(current) == want {
		if err := os.Chmod(scriptPath, 0755); err != nil {
			return "", fmt.Errorf("chmod %s: %w", name, err)
		}
		return HookActionVerified, nil
	}
	if err := os.WriteFile(scriptPath, []byte(want), 0755); err != nil {
		return "", fmt.Errorf("update %s: %w", name, err)
	}
	if err := os.Chmod(scriptPath, 0755); err != nil {
		return "", fmt.Errorf("chmod %s: %w", name, err)
	}
	return HookActionUpdated, nil
}

func managedIndexScriptOwned(content string) bool {
	return strings.Contains(content, ManagedIndexScriptMarker) ||
		strings.Contains(content, legacyManagedIndexScriptMarker)
}

func rejectHookCollisions(hooksDir string, hooks []string) error {
	var collisions []HookCollision
	for _, hook := range hooks {
		hookPath := filepath.Join(hooksDir, hook)
		managed, err := managedLifecycleHookExists(hookPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !managed {
			collisions = append(collisions, HookCollision{Hook: hook, Path: hookPath})
		}
	}
	if len(collisions) > 0 {
		return &HookCollisionError{Collisions: collisions}
	}
	return nil
}

func managedLifecycleHookExists(hookPath string) (bool, error) {
	info, err := os.Lstat(hookPath)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(hookPath)
		if err != nil {
			return false, err
		}
		if filepath.Base(target) == hookDispatcherName() {
			return true, nil
		}
	}
	content, err := os.ReadFile(hookPath)
	if err != nil {
		return false, err
	}
	if strings.Contains(string(content), ManagedHookMarker) {
		return true, nil
	}
	return looksLikeLegacyHookDispatcher(string(content)), nil
}

func installManagedHookDispatcher(dispatcherPath string) (HookAction, error) {
	want := managedHookDispatcherContent()
	name := hookDispatcherName()
	current, err := os.ReadFile(dispatcherPath)
	if os.IsNotExist(err) {
		if err := os.WriteFile(dispatcherPath, []byte(want), 0755); err != nil {
			return "", fmt.Errorf("install %s: %w", name, err)
		}
		return HookActionInstalled, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	if string(current) == want {
		if err := os.Chmod(dispatcherPath, 0755); err != nil {
			return "", fmt.Errorf("chmod %s: %w", name, err)
		}
		return HookActionVerified, nil
	}
	if !strings.Contains(string(current), ManagedHookMarker) && !looksLikeLegacyHookDispatcher(string(current)) {
		return "", fmt.Errorf("%s at %s already exists and is not managed by %s", name, dispatcherPath, brand.NameTitle)
	}
	if err := os.WriteFile(dispatcherPath, []byte(want), 0755); err != nil {
		return "", fmt.Errorf("update %s: %w", name, err)
	}
	if err := os.Chmod(dispatcherPath, 0755); err != nil {
		return "", fmt.Errorf("chmod %s: %w", name, err)
	}
	return HookActionUpdated, nil
}

func installManagedHook(hookPath, hook string) (HookAction, error) {
	info, err := os.Lstat(hookPath)
	if os.IsNotExist(err) {
		if err := os.Symlink(hookDispatcherName(), hookPath); err != nil {
			return installManagedHookWrapper(hookPath, hook)
		}
		return HookActionInstalled, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect %s hook: %w", hook, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(hookPath)
		if err != nil {
			return "", fmt.Errorf("read %s hook symlink: %w", hook, err)
		}
		if filepath.Base(target) == hookDispatcherName() {
			return HookActionVerified, nil
		}
	}

	managed, err := managedLifecycleHookExists(hookPath)
	if err != nil {
		return "", fmt.Errorf("read %s hook: %w", hook, err)
	}
	if !managed {
		return "", fmt.Errorf("%s at %s already exists and is not managed by %s", hook, hookPath, brand.NameTitle)
	}

	// Build the symlink beside the hook and move it into place, rather than
	// removing the hook first. os.Symlink cannot overwrite an existing file, but
	// removing before knowing whether the link can be created destroys a working
	// wrapper wherever symlinks are unavailable — Windows without Developer Mode
	// or an elevated shell, and filesystems that do not support them. The
	// wrapper fallback then sees no file, reinstalls from scratch and reports
	// "installed" on every run instead of "verified" or "updated".
	staged := hookPath + ".tmp"
	if err := os.Remove(staged); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("clear staged %s hook: %w", hook, err)
	}
	if err := os.Symlink(hookDispatcherName(), staged); err != nil {
		return installManagedHookWrapper(hookPath, hook)
	}
	if err := os.Rename(staged, hookPath); err != nil {
		if removeErr := os.Remove(staged); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", fmt.Errorf("replace %s hook: %w (and clearing %s failed: %v)", hook, err, staged, removeErr)
		}
		return "", fmt.Errorf("replace %s hook: %w", hook, err)
	}
	return HookActionUpdated, nil
}

func installManagedHookWrapper(hookPath, hook string) (HookAction, error) {
	want := managedHookContent(hook)
	current, err := os.ReadFile(hookPath)
	if os.IsNotExist(err) {
		if err := os.WriteFile(hookPath, []byte(want), 0755); err != nil {
			return "", fmt.Errorf("install %s hook wrapper: %w", hook, err)
		}
		return HookActionInstalled, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s hook wrapper: %w", hook, err)
	}
	if string(current) == want {
		if err := os.Chmod(hookPath, 0755); err != nil {
			return "", fmt.Errorf("chmod %s hook wrapper: %w", hook, err)
		}
		return HookActionVerified, nil
	}
	if err := os.WriteFile(hookPath, []byte(want), 0755); err != nil {
		return "", fmt.Errorf("update %s hook wrapper: %w", hook, err)
	}
	if err := os.Chmod(hookPath, 0755); err != nil {
		return "", fmt.Errorf("chmod %s hook wrapper: %w", hook, err)
	}
	return HookActionUpdated, nil
}

func managedHookContent(hook string) string {
	return fmt.Sprintf(`#!/bin/sh
%s
# Hook: %s
hook_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 0
dispatcher="$hook_dir/%s"
if [ -x "$dispatcher" ]; then
	%s=%s "$dispatcher" "$@"
fi
exit 0
		`, ManagedHookMarker, hook, hookDispatcherName(), hookNameEnvVar, shellQuote(hook))
}

func managedHookDispatcherContent() string {
	return fmt.Sprintf(`#!/bin/sh
%s
set -eu

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$repo_root" ]; then
	exit 0
fi

hook_name="${%s:-$(basename "$0")}"
if [ "$hook_name" = "post-checkout" ] && [ "${3:-}" = "0" ]; then
	exit 0
fi

case "$repo_root" in
	*/.worktrees/*)
		exit 0
		;;
esac

hook_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 0
script="$hook_dir/%s"
if [ ! -x "$script" ]; then
	exit 0
fi

cd "$repo_root"
"$script"
`, ManagedHookMarker, hookNameEnvVar, scriptName())
}

func looksLikeLegacyHookDispatcher(content string) bool {
	return strings.Contains(content, `repo_root="$(git rev-parse --show-toplevel`) &&
		strings.Contains(content, "post-checkout") &&
		strings.Contains(content, ".worktrees") &&
		strings.Contains(content, "liza-index.sh")
}

func looksLikeLegacyIndexScript(content string) bool {
	return strings.Contains(content, "liza pairing mode") ||
		(strings.Contains(content, "scip-") && strings.Contains(content, "stacklit"))
}

func gitUnmatchedPath(err error, output []byte) bool {
	return gitExitCode(err, 1) && strings.Contains(string(output), "did not match any file")
}

func gitExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func outputSuffix(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return ": " + output
}
