package providers

import (
	"os/exec"
)

type DetectionResult struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Installed   bool   `json:"installed"`
	Executable  string `json:"executable,omitempty"`
	Error       string `json:"error,omitempty"`
}

func Detect(cat Catalog, lookPath func(string) (string, error)) []DetectionResult {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	results := make([]DetectionResult, 0, len(cat.Providers))
	for _, p := range cat.ProvidersSorted() {
		binaries := p.Detection.Binaries
		if len(binaries) == 0 {
			binaries = []string{p.Runtime.Executable}
		}
		result := DetectionResult{ID: p.ID, DisplayName: p.DisplayName}
		for _, bin := range binaries {
			path, err := lookPath(bin)
			if err == nil {
				result.Installed = true
				result.Executable = path
				break
			}
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results
}
