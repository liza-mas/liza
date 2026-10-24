package scipsearch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tailscale/hujson"
)

func typeScriptIndexPlans(targetRoot string, inputs []typeScriptCommandInputs) []RuntimeCommandPlan {
	plans := make([]RuntimeCommandPlan, 0, len(inputs))
	for _, input := range inputs {
		aggregateRoot, ok := aggregateRootFor(targetRoot, input.Cwd)
		if !ok {
			continue
		}
		plans = append(plans, RuntimeCommandPlan{
			Language:   "typescript",
			Name:       languageIndexers["typescript"],
			Args:       []string{"index", "--cwd", input.Cwd, "--output", outputPathPlaceholder, input.ProjectRoot},
			Dir:        targetRoot,
			OutputPath: outputPathPlaceholder,
			Root:       aggregateRoot,
		})
	}
	return plans
}

func pairingTypeScriptCommandInputs(targetRoot string, files []string) []typeScriptCommandInputs {
	rootConfigs := typeScriptRootConfigCandidates(files)
	if len(rootConfigs) == 0 {
		return nil
	}

	var candidates []typeScriptCommandInputs
	for _, relConfig := range rootConfigs {
		rootConfig := filepath.Join(targetRoot, filepath.FromSlash(relConfig))
		rootConfig = filepath.Clean(rootConfig)
		if !pathWithin(targetRoot, rootConfig) {
			continue
		}
		projectRoot := filepath.Dir(rootConfig)
		sourceCandidates := typeScriptSourceCandidatesForRoot(targetRoot, rootConfig)
		if len(sourceCandidates) == 0 {
			if _, err := readTSConfig(rootConfig); err != nil {
				continue
			}
			candidates = append(candidates, typeScriptCommandInputs{Cwd: projectRoot, ProjectRoot: projectRoot})
			continue
		}
		for _, candidate := range sourceCandidates {
			candidates = append(candidates, typeScriptCommandInputs{Cwd: candidate.sourceRoot, ProjectRoot: candidate.projectRoot})
		}
	}
	return dedupeTypeScriptInputs(candidates)
}

type typeScriptCommandInputs struct {
	Cwd         string
	ProjectRoot string
}

type tsconfigFile struct {
	Files      []string            `json:"files"`
	Include    []string            `json:"include"`
	References []tsconfigReference `json:"references"`
}

type tsconfigReference struct {
	Path string `json:"path"`
}

type typeScriptSourceCandidate struct {
	configPath  string
	sourceRoot  string
	projectRoot string
}

func typeScriptRootConfigCandidates(files []string) []string {
	candidates := make([]string, 0)
	for _, file := range files {
		file = filepath.ToSlash(filepath.Clean(file))
		if file == "." || strings.HasPrefix(file, "../") || filepath.IsAbs(file) {
			continue
		}
		if filepath.Base(file) == "tsconfig.json" {
			candidates = append(candidates, file)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		iDepth := strings.Count(candidates[i], "/")
		jDepth := strings.Count(candidates[j], "/")
		if iDepth != jDepth {
			return iDepth < jDepth
		}
		return candidates[i] < candidates[j]
	})
	return candidates
}

func typeScriptSourceCandidatesForRoot(targetRoot, rootConfig string) []typeScriptSourceCandidate {
	leafConfigs := collectTypeScriptLeafConfigs(targetRoot, rootConfig, map[string]bool{})
	candidates := make([]typeScriptSourceCandidate, 0, len(leafConfigs))
	for _, leafConfig := range leafConfigs {
		config, err := readTSConfig(leafConfig)
		if err != nil {
			continue
		}
		for _, sourceRoot := range typeScriptSourceRootsForConfig(filepath.Dir(leafConfig), config) {
			if !pathWithin(targetRoot, sourceRoot) {
				continue
			}
			candidates = append(candidates, typeScriptSourceCandidate{
				configPath:  filepath.Clean(leafConfig),
				sourceRoot:  filepath.Clean(sourceRoot),
				projectRoot: filepath.Dir(leafConfig),
			})
		}
	}
	return candidates
}

func collectTypeScriptLeafConfigs(targetRoot, configPath string, visited map[string]bool) []string {
	configPath = filepath.Clean(configPath)
	if visited[configPath] || !pathWithin(targetRoot, configPath) {
		return nil
	}
	visited[configPath] = true

	config, err := readTSConfig(configPath)
	if err != nil {
		return nil
	}
	if len(config.References) == 0 {
		return []string{configPath}
	}

	refs := append([]tsconfigReference(nil), config.References...)
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Path < refs[j].Path
	})

	var leaves []string
	configDir := filepath.Dir(configPath)
	for _, ref := range refs {
		refConfig, ok := resolveTypeScriptReferenceConfig(targetRoot, configDir, ref.Path)
		if !ok {
			continue
		}
		leaves = append(leaves, collectTypeScriptLeafConfigs(targetRoot, refConfig, visited)...)
	}
	return leaves
}

func readTSConfig(path string) (tsconfigFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return tsconfigFile{}, err
	}
	standard, err := hujson.Standardize(content)
	if err != nil {
		return tsconfigFile{}, err
	}
	var config tsconfigFile
	if err := json.Unmarshal(standard, &config); err != nil {
		return tsconfigFile{}, err
	}
	return config, nil
}

func resolveTypeScriptReferenceConfig(targetRoot, configDir, refPath string) (string, bool) {
	refPath = strings.TrimSpace(refPath)
	if refPath == "" {
		return "", false
	}

	base := filepath.Clean(filepath.Join(configDir, filepath.FromSlash(refPath)))
	candidates := typeScriptReferenceConfigCandidates(base)
	for _, candidate := range candidates {
		if !pathWithin(targetRoot, candidate) {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return filepath.Clean(candidate), true
		}
	}
	return "", false
}

func typeScriptReferenceConfigCandidates(base string) []string {
	if filepath.Ext(base) == ".json" {
		return []string{base}
	}
	return dedupePaths([]string{
		base + ".json",
		filepath.Join(base, "tsconfig.json"),
		base,
	})
}

func dedupeTypeScriptInputs(inputs []typeScriptCommandInputs) []typeScriptCommandInputs {
	byProjectRoot := make(map[string]typeScriptCommandInputs, len(inputs))
	for _, input := range inputs {
		existing, ok := byProjectRoot[input.ProjectRoot]
		if !ok || preferTypeScriptInput(input, existing) {
			byProjectRoot[input.ProjectRoot] = input
		}
	}
	out := make([]typeScriptCommandInputs, 0, len(byProjectRoot))
	for _, input := range byProjectRoot {
		out = append(out, input)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ProjectRoot < out[j].ProjectRoot
	})
	return out
}

func preferTypeScriptInput(candidate, existing typeScriptCommandInputs) bool {
	candidateSrc := isTypeScriptProjectSrcInput(candidate)
	existingSrc := isTypeScriptProjectSrcInput(existing)
	if candidateSrc != existingSrc {
		return candidateSrc
	}
	candidateDepth := pathDepthWithin(candidate.ProjectRoot, candidate.Cwd)
	existingDepth := pathDepthWithin(existing.ProjectRoot, existing.Cwd)
	if candidateDepth != existingDepth {
		return candidateDepth < existingDepth
	}
	return candidate.Cwd < existing.Cwd
}

func isTypeScriptProjectSrcInput(input typeScriptCommandInputs) bool {
	return filepath.Clean(input.Cwd) == filepath.Join(filepath.Clean(input.ProjectRoot), "src")
}

func typeScriptCandidateRoots(candidates []typeScriptCommandInputs) []string {
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		roots = append(roots, candidate.Cwd)
	}
	return dedupePaths(roots)
}

func typeScriptSourceRootsForConfig(configDir string, config tsconfigFile) []string {
	var roots []string
	for _, include := range config.Include {
		if root, ok := typeScriptIncludeSourceRoot(configDir, include); ok {
			roots = append(roots, root)
		}
	}
	for _, file := range config.Files {
		if root, ok := typeScriptFileSourceRoot(configDir, file); ok {
			roots = append(roots, root)
		}
	}
	return dedupePaths(roots)
}

func typeScriptIncludeSourceRoot(configDir, include string) (string, bool) {
	include = strings.TrimSpace(include)
	if include == "" {
		return "", false
	}
	prefix := staticGlobDirectoryPrefix(filepath.ToSlash(include))
	root, ok := resolveTypeScriptSourceRoot(configDir, prefix)
	if !ok {
		return "", false
	}
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		return filepath.Dir(root), true
	}
	return root, true
}

func typeScriptFileSourceRoot(configDir, file string) (string, bool) {
	file = strings.TrimSpace(file)
	if file == "" {
		return "", false
	}
	file = filepath.ToSlash(file)
	if hasGlobMeta(file) {
		return "", false
	}
	return resolveTypeScriptSourceRoot(configDir, filepath.Dir(file))
}

func staticGlobDirectoryPrefix(pattern string) string {
	pattern = strings.Trim(pattern, "/")
	if pattern == "" || pattern == "." {
		return "."
	}

	parts := strings.Split(pattern, "/")
	var prefix []string
	for _, part := range parts {
		if hasGlobMeta(part) {
			break
		}
		prefix = append(prefix, part)
	}
	if len(prefix) == 0 {
		return "."
	}
	return strings.Join(prefix, "/")
}

func hasGlobMeta(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

func resolveTypeScriptSourceRoot(configDir, sourceRoot string) (string, bool) {
	sourceRoot = strings.TrimSpace(sourceRoot)
	if sourceRoot == "" {
		return "", false
	}
	if filepath.IsAbs(sourceRoot) {
		return filepath.Clean(sourceRoot), true
	}
	return filepath.Clean(filepath.Join(configDir, filepath.FromSlash(sourceRoot))), true
}
