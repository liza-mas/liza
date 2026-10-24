package pairingindex

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/scipsearch"
)

var defaultLifecycleHooks = []string{"post-commit", "post-checkout", "post-merge", "post-rewrite"}

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
// setup for project-root refresh, including Semble-only installations.
type InstallActivationOptions struct {
	RepoRoot       string
	Hooks          []string
	EnableStacklit bool
	// Semble preparation runs in the Go coordinator under its runtime gate.
	EnableSemble             bool
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
	// HookActionInstalled means the tool wrote a missing managed hook wrapper.
	HookActionInstalled HookAction = "installed"
	// HookActionVerified means an existing wrapper already matched the managed content.
	HookActionVerified HookAction = "verified"
	// HookActionUpdated means an existing managed wrapper was refreshed.
	HookActionUpdated HookAction = "updated"
)

// HookCollision identifies an existing unmanaged hook that must not be overwritten.
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

// HookCollisionError reports all unmanaged lifecycle hook collisions found during preflight.
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
	renderOpts := opts.renderOptions()
	if renderOpts.EnableFunctionalClusters {
		if err := ensureFunctionalClustersArtifactCleanliness(opts.RepoRoot); err != nil {
			return result, err
		}
	}

	scriptPath := filepath.Join(hooksDir, scriptName())
	content, err := renderIndexScript(renderOpts)
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
