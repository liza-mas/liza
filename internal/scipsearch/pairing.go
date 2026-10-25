package scipsearch

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PairingPlanOptions configures project-root SCIP planning for pairing init
// hook generation. ExplicitLanguages is the repeated --scip-search language
// allowlist; it restricts languages but does not select monorepo roots.
type PairingPlanOptions struct {
	ProjectRoot       string
	ExplicitLanguages []string
	CommandOverrides  []PairingCommandOverride
	GitFiles          GitFilesFunc
	SkipUnresolved    bool // best-effort only; ignored when ExplicitLanguages is non-empty
}

// PairingPlanResult reports concrete pairing SCIP aggregate plans plus optional
// skipped languages when planning is allowed to be best-effort.
type PairingPlanResult struct {
	Plans []LanguageAggregatePlan
	Skips []PairingPlanSkip
}

// PairingPlanSkipReason identifies why a pairing SCIP language was skipped.
type PairingPlanSkipReason string

const (
	PairingPlanSkipNoCandidates PairingPlanSkipReason = "no_candidates"
)

// PairingPlanSkip carries planner facts for presentation by callers.
type PairingPlanSkip struct {
	Language   Language
	Candidates []string
	Reason     PairingPlanSkipReason
}

// PairingCommandOverride adds one explicit pairing-mode language root to the
// aggregate plan for that language when autodetection should be replaced.
type PairingCommandOverride struct {
	Language              Language
	GoModuleRoot          string
	TypeScriptCwd         string
	TypeScriptProjectRoot string
	PythonCwd             string
	PythonTargetOnly      string
}

// PairingRuntimeInferenceNote states the shared aggregate planning contract used
// by pairing hooks and MAS runtime refresh planning.
const PairingRuntimeInferenceNote = "Pairing and MAS runtime SCIP planning use the same multi-root aggregate model; every final language index is rewritten relative to the target root."

// PlanPairingCommands returns concrete repo-root SCIP aggregate plans for
// pairing init hook generation. It aggregates all detected roots into one final
// per-language index.
func PlanPairingCommands(opts PairingPlanOptions) (PairingPlanResult, error) {
	projectRoot, err := filepath.Abs(opts.ProjectRoot)
	if err != nil {
		return PairingPlanResult{}, fmt.Errorf("resolve pairing scip-search project root: %w", err)
	}

	gitFiles := opts.GitFiles
	if gitFiles == nil {
		gitFiles = listGitFiles
	}
	files, err := gitFiles(projectRoot)
	if err != nil {
		return PairingPlanResult{}, fmt.Errorf("detect pairing scip-search languages: %w", err)
	}

	languages := detectLanguages(files)
	explicitFilter := len(opts.ExplicitLanguages) > 0
	skipUnresolved := opts.SkipUnresolved && !explicitFilter
	if len(opts.ExplicitLanguages) > 0 {
		languages, err = canonicalizeExplicit(opts.ExplicitLanguages)
		if err != nil {
			return PairingPlanResult{}, err
		}
	}

	overrides, err := pairingOverrideMap(opts.CommandOverrides)
	if err != nil {
		return PairingPlanResult{}, err
	}
	if explicitFilter {
		if err := ensureOverridesAllowedByExplicitLanguages(languages, overrides); err != nil {
			return PairingPlanResult{}, err
		}
	} else {
		languages = mergePairingLanguages(languages, overrides)
	}

	result := PairingPlanResult{
		Plans: make([]LanguageAggregatePlan, 0, len(languages)),
	}
	for _, language := range languages {
		if override, ok := overrides[language]; ok {
			plan, err := pairingCommandOverridePlan(projectRoot, override)
			if err != nil {
				return PairingPlanResult{}, err
			}
			result.Plans = append(result.Plans, plan)
			continue
		}
		plan, candidates, ok := pairingCommandPlan(projectRoot, language, files)
		if !ok {
			if skipUnresolved {
				result.Skips = append(result.Skips, pairingPlanSkip(language, candidates))
				continue
			}
			return PairingPlanResult{}, pairingRootSelectionError{language: language}
		}
		result.Plans = append(result.Plans, plan)
	}
	return result, nil
}

// ParsePairingCommandOverrides parses repeatable pairing SCIP override specs.
//
// Supported forms:
//   - go=<module-root>
//   - typescript=<cwd>,<project-root>
//   - python=<cwd>
//   - python=<cwd>,<target-only>
func ParsePairingCommandOverrides(projectRoot string, raw []string) ([]PairingCommandOverride, error) {
	projectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve pairing scip-search project root: %w", err)
	}

	overrides := make([]PairingCommandOverride, 0, len(raw))
	for _, spec := range raw {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		rawLanguage, value, ok := strings.Cut(spec, "=")
		if !ok {
			return nil, fmt.Errorf("invalid scip-search pairing plan %q: expected language=value", spec)
		}
		language := Language(strings.TrimSpace(rawLanguage))
		value = strings.TrimSpace(value)
		if err := validateLanguage(language); err != nil {
			return nil, err
		}
		if value == "" {
			return nil, fmt.Errorf("invalid scip-search pairing plan %q: value is empty", spec)
		}
		parts := splitPairingOverrideValue(value)
		override := PairingCommandOverride{Language: language}
		switch language {
		case LanguageGo:
			if len(parts) != 1 {
				return nil, fmt.Errorf("invalid go scip-search pairing plan %q: expected go=<module-root>", spec)
			}
			override.GoModuleRoot, err = resolvePairingOverridePath(projectRoot, parts[0])
		case LanguageTypeScript:
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid typescript scip-search pairing plan %q: expected typescript=<cwd>,<project-root>", spec)
			}
			override.TypeScriptCwd, err = resolvePairingOverridePath(projectRoot, parts[0])
			if err == nil {
				override.TypeScriptProjectRoot, err = resolvePairingOverridePath(projectRoot, parts[1])
			}
		case LanguagePython:
			if len(parts) < 1 || len(parts) > 2 {
				return nil, fmt.Errorf("invalid python scip-search pairing plan %q: expected python=<cwd> or python=<cwd>,<target-only>", spec)
			}
			override.PythonCwd, err = resolvePairingOverridePath(projectRoot, parts[0])
			if err == nil && len(parts) == 2 {
				override.PythonTargetOnly, err = cleanPairingTargetOnly(parts[1])
			}
		}
		if err != nil {
			return nil, fmt.Errorf("invalid scip-search pairing plan %q: %w", spec, err)
		}
		overrides = append(overrides, override)
	}
	return overrides, nil
}

type pairingRootSelectionError struct {
	language Language
}

func (err pairingRootSelectionError) Error() string {
	return fmt.Sprintf("unresolved scip-search language %s: no candidate roots found", err.language)
}

func pairingPlanSkip(language Language, candidates []string) PairingPlanSkip {
	return PairingPlanSkip{
		Language:   language,
		Candidates: append([]string(nil), candidates...),
		Reason:     PairingPlanSkipNoCandidates,
	}
}

func pairingCommandPlan(projectRoot string, language Language, files []string) (LanguageAggregatePlan, []string, bool) {
	outputPath := projectRootIndexPath(projectRoot, language)
	plan := LanguageAggregatePlan{
		Language:    language,
		OutputPath:  outputPath,
		ProjectRoot: projectRoot,
	}

	switch language {
	case LanguageGo:
		roots := goRootCandidates(projectRoot, files)
		if len(roots) == 0 {
			return LanguageAggregatePlan{}, roots, false
		}
		plan.IndexPlans = goIndexPlans(projectRoot, roots)
	case LanguageTypeScript:
		candidates := pairingTypeScriptCommandInputs(projectRoot, files)
		if len(candidates) == 0 {
			return LanguageAggregatePlan{}, typeScriptCandidateRoots(candidates), false
		}
		plan.IndexPlans = typeScriptIndexPlans(projectRoot, candidates)
	case LanguagePython:
		candidates := pairingPythonCommandInputs(projectRoot, files)
		if len(candidates) == 0 {
			return LanguageAggregatePlan{}, pythonCandidateRoots(candidates), false
		}
		plan.IndexPlans = pythonIndexPlans(projectRoot, candidates)
	default:
		return LanguageAggregatePlan{}, nil, false
	}

	plan.IndexPlans = dedupeIndexPlans(plan.IndexPlans)
	return plan, nil, true
}

func pairingCommandOverridePlan(projectRoot string, overrides []PairingCommandOverride) (LanguageAggregatePlan, error) {
	if len(overrides) == 0 {
		return LanguageAggregatePlan{}, fmt.Errorf("empty scip-search pairing overrides")
	}
	language := overrides[0].Language
	outputPath := projectRootIndexPath(projectRoot, language)
	plan := LanguageAggregatePlan{
		Language:    language,
		ProjectRoot: projectRoot,
		OutputPath:  outputPath,
	}
	for _, override := range overrides {
		if override.Language != language {
			return LanguageAggregatePlan{}, fmt.Errorf("mixed scip-search override languages in %s plan", language)
		}
		switch language {
		case LanguageGo:
			if override.GoModuleRoot == "" {
				return LanguageAggregatePlan{}, fmt.Errorf("invalid go scip-search pairing override: module root is empty")
			}
			if err := ensurePairingOverridePathWithin(projectRoot, override.GoModuleRoot, "module root"); err != nil {
				return LanguageAggregatePlan{}, err
			}
			plan.IndexPlans = append(plan.IndexPlans, goIndexPlans(projectRoot, []string{override.GoModuleRoot})...)
		case LanguageTypeScript:
			if override.TypeScriptCwd == "" || override.TypeScriptProjectRoot == "" {
				return LanguageAggregatePlan{}, fmt.Errorf("invalid typescript scip-search pairing override: cwd and project root are required")
			}
			if err := ensurePairingOverridePathWithin(projectRoot, override.TypeScriptCwd, "typescript cwd"); err != nil {
				return LanguageAggregatePlan{}, err
			}
			if err := ensurePairingOverridePathWithin(projectRoot, override.TypeScriptProjectRoot, "typescript project root"); err != nil {
				return LanguageAggregatePlan{}, err
			}
			plan.IndexPlans = append(plan.IndexPlans, typeScriptIndexPlans(projectRoot, []typeScriptCommandInputs{{
				Cwd:         override.TypeScriptCwd,
				ProjectRoot: override.TypeScriptProjectRoot,
			}})...)
		case LanguagePython:
			if override.PythonCwd == "" {
				return LanguageAggregatePlan{}, fmt.Errorf("invalid python scip-search pairing override: cwd is empty")
			}
			if err := ensurePairingOverridePathWithin(projectRoot, override.PythonCwd, "python cwd"); err != nil {
				return LanguageAggregatePlan{}, err
			}
			if override.PythonTargetOnly != "" {
				targetOnly, err := cleanPairingTargetOnly(override.PythonTargetOnly)
				if err != nil {
					return LanguageAggregatePlan{}, err
				}
				override.PythonTargetOnly = targetOnly
			}
			plan.IndexPlans = append(plan.IndexPlans, pythonIndexPlans(projectRoot, []pythonCommandInputs{{
				Cwd:        override.PythonCwd,
				TargetOnly: override.PythonTargetOnly,
			}})...)
		default:
			return LanguageAggregatePlan{}, validateLanguage(language)
		}
	}
	plan.IndexPlans = dedupeIndexPlans(plan.IndexPlans)
	return plan, nil
}

func pairingOverrideMap(overrides []PairingCommandOverride) (map[Language][]PairingCommandOverride, error) {
	out := make(map[Language][]PairingCommandOverride, len(overrides))
	for _, override := range overrides {
		if err := validateLanguage(override.Language); err != nil {
			return nil, err
		}
		out[override.Language] = append(out[override.Language], override)
	}
	return out, nil
}

func mergePairingLanguages(languages []Language, overrides map[Language][]PairingCommandOverride) []Language {
	seen := make(map[Language]bool, len(languages)+len(overrides))
	for _, language := range languages {
		seen[language] = true
	}
	for language := range overrides {
		seen[language] = true
	}

	var out []Language
	for _, language := range supportedLanguages {
		if seen[language] {
			out = append(out, language)
		}
	}
	return out
}

func ensureOverridesAllowedByExplicitLanguages(languages []Language, overrides map[Language][]PairingCommandOverride) error {
	allowed := make(map[Language]bool, len(languages))
	for _, language := range languages {
		allowed[language] = true
	}
	for language := range overrides {
		if !allowed[language] {
			return fmt.Errorf("scip-search pairing plan for %s is outside explicit --scip-search allowlist", language)
		}
	}
	return nil
}

func splitPairingOverrideValue(value string) []string {
	raw := strings.Split(value, ",")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func resolvePairingOverridePath(projectRoot, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path is empty")
	}
	path := filepath.Clean(filepath.FromSlash(value))
	if !filepath.IsAbs(path) {
		path = filepath.Join(projectRoot, path)
	}
	path = filepath.Clean(path)
	if !pathWithin(projectRoot, path) {
		return "", fmt.Errorf("path %q is outside repository root %q", value, projectRoot)
	}
	return path, nil
}

func ensurePairingOverridePathWithin(projectRoot, path, label string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("invalid scip-search pairing override: %s %q must be absolute", label, path)
	}
	if !pathWithin(projectRoot, path) {
		return fmt.Errorf("invalid scip-search pairing override: %s %q is outside repository root %q", label, path, projectRoot)
	}
	return nil
}

func cleanPairingTargetOnly(value string) (string, error) {
	value = strings.TrimSpace(filepath.ToSlash(value))
	if value == "" {
		return "", fmt.Errorf("target-only is empty")
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("target-only %q must be relative to the python cwd", value)
	}
	return cleaned, nil
}
