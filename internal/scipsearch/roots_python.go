package scipsearch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func pythonIndexPlans(targetRoot string, inputs []pythonCommandInputs) []RuntimeCommandPlan {
	plans := make([]RuntimeCommandPlan, 0, len(inputs))
	for _, input := range inputs {
		// scip-python records <cwd>/<target-only> as the index project root.
		aggregateRoot, ok := aggregateRootFor(targetRoot, filepath.Join(input.Cwd, input.TargetOnly))
		if !ok {
			continue
		}
		args := []string{"index", "--cwd", input.Cwd, "--output", outputPathPlaceholder}
		if input.TargetOnly != "" {
			args = append(args, "--target-only="+input.TargetOnly)
		}
		plans = append(plans, RuntimeCommandPlan{
			Language:   "python",
			Name:       languageIndexers["python"],
			Args:       args,
			Dir:        targetRoot,
			OutputPath: outputPathPlaceholder,
			Root:       aggregateRoot,
		})
	}
	return plans
}

func pairingPythonCommandInputs(targetRoot string, files []string) []pythonCommandInputs {
	pythonFiles := pythonTrackedFiles(targetRoot, files)
	if len(pythonFiles) == 0 {
		return nil
	}

	roots := pythonProjectRootCandidates(targetRoot, files)
	if len(roots) == 0 {
		return []pythonCommandInputs{{Cwd: targetRoot}}
	}

	var candidates []pythonCommandInputs
	for _, root := range roots {
		eligible := eligiblePythonFilesForRoot(root, roots, pythonFiles)
		if len(eligible) == 0 {
			continue
		}
		inputs := pythonCommandInputs{Cwd: root}
		if pythonFilesAllUnderSrc(root, eligible) {
			inputs.TargetOnly = "src"
		}
		candidates = append(candidates, inputs)
	}
	return candidates
}

type pythonCommandInputs struct {
	Cwd        string
	TargetOnly string
}

var pythonProjectMarkerPriority = map[string]int{
	"pyproject.toml":   0,
	"setup.cfg":        1,
	"setup.py":         2,
	"requirements.txt": 3,
}

func pythonTrackedFiles(targetRoot string, files []string) []string {
	var out []string
	for _, file := range files {
		if filepath.Ext(file) != ".py" {
			continue
		}
		path, ok := cleanTargetRelativePath(targetRoot, file)
		if ok {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func pythonProjectRootCandidates(targetRoot string, files []string) []string {
	seen := map[string]int{}
	for _, file := range files {
		base := filepath.Base(file)
		priority, ok := pythonProjectMarkerPriority[base]
		if !ok {
			continue
		}
		path, ok := cleanTargetRelativePath(targetRoot, file)
		if !ok {
			continue
		}
		root := filepath.Dir(path)
		if base == "pyproject.toml" && !pyprojectDefinesPythonProject(path) {
			continue
		}
		if previous, exists := seen[root]; !exists || priority < previous {
			seen[root] = priority
		}
	}

	roots := make([]string, 0, len(seen))
	for root := range seen {
		roots = append(roots, root)
	}
	sort.Slice(roots, func(i, j int) bool {
		iDepth := pathDepthWithin(targetRoot, roots[i])
		jDepth := pathDepthWithin(targetRoot, roots[j])
		if iDepth != jDepth {
			return iDepth < jDepth
		}
		return roots[i] < roots[j]
	})
	return roots
}

func pyprojectDefinesPythonProject(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch line {
		case "[project]", "[tool.poetry]", "[tool.pdm]":
			return true
		}
	}
	return false
}

func eligiblePythonFilesForRoot(root string, candidateRoots, pythonFiles []string) []string {
	descendantRoots := make([]string, 0)
	for _, candidateRoot := range candidateRoots {
		if candidateRoot != root && pathWithin(root, candidateRoot) {
			descendantRoots = append(descendantRoots, candidateRoot)
		}
	}

	var eligible []string
	for _, file := range pythonFiles {
		if !pathWithin(root, file) {
			continue
		}
		if pathWithinAny(descendantRoots, file) {
			continue
		}
		eligible = append(eligible, file)
	}
	return eligible
}

func pythonFilesAllUnderSrc(root string, files []string) bool {
	srcRoot := filepath.Join(root, "src")
	for _, file := range files {
		if !pathWithin(srcRoot, file) {
			return false
		}
	}
	return len(files) > 0
}

func pythonCandidateRoots(candidates []pythonCommandInputs) []string {
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		roots = append(roots, candidate.Cwd)
	}
	return dedupePaths(roots)
}
