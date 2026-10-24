package scipsearch

import (
	"path/filepath"
)

func goIndexPlans(targetRoot string, roots []string) []RuntimeCommandPlan {
	plans := make([]RuntimeCommandPlan, 0, len(roots))
	for _, root := range roots {
		aggregateRoot, ok := aggregateRootFor(targetRoot, root)
		if !ok {
			continue
		}
		plans = append(plans, RuntimeCommandPlan{
			Language:   "go",
			Name:       languageIndexers["go"],
			Args:       []string{"index", "--module-root", root, "--output", outputPathPlaceholder},
			Dir:        targetRoot,
			OutputPath: outputPathPlaceholder,
			Root:       aggregateRoot,
		})
	}
	return plans
}

func goRootCandidates(targetRoot string, files []string) []string {
	seen := map[string]bool{}
	hasGoFile := false
	for _, file := range files {
		path, ok := cleanTargetRelativePath(targetRoot, file)
		if !ok {
			continue
		}
		switch {
		case filepath.Base(path) == "go.mod":
			seen[filepath.Dir(path)] = true
		case filepath.Ext(path) == ".go":
			hasGoFile = true
		}
	}
	if len(seen) == 0 && hasGoFile {
		seen[targetRoot] = true
	}
	return sortedMapKeys(seen)
}
