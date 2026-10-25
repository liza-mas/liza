package scipsearch

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/envgate"
)

// Language identifies a SCIP-indexable language. Configured and explicit
// languages stay plain strings at the input boundary and are converted when
// planning begins; unknown values are refused by the same validation as before.
type Language string

const (
	LanguageGo         Language = "go"
	LanguageTypeScript Language = "typescript"
	LanguagePython     Language = "python"
)

var supportedLanguages = []Language{LanguageGo, LanguageTypeScript, LanguagePython}

var languageIndexers = map[Language]string{
	LanguageGo:         "scip-go",
	LanguageTypeScript: "scip-typescript",
	LanguagePython:     "scip-python",
}

func selectLanguages(opts InitOptions, gitFiles GitFilesFunc) ([]Language, error) {
	if len(opts.ExplicitLanguages) > 0 {
		return canonicalizeExplicit(opts.ExplicitLanguages)
	}
	if !envgate.Truthy(opts.EnvValue) {
		return nil, nil
	}
	files, err := gitFiles(opts.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to auto-detect scip-search languages with git ls-files: %w", err)
	}
	return detectLanguages(files), nil
}

func canonicalizeExplicit(raw []string) ([]Language, error) {
	seen := make(map[Language]bool, len(raw))
	for _, value := range raw {
		language := Language(strings.TrimSpace(value))
		if err := validateLanguage(language); err != nil {
			return nil, err
		}
		seen[language] = true
	}

	var out []Language
	for _, language := range supportedLanguages {
		if seen[language] {
			out = append(out, language)
		}
	}
	return out, nil
}

// validateLanguage refuses a language outside the supported set.
func validateLanguage(language Language) error {
	if slices.Contains(supportedLanguages, language) {
		return nil
	}
	supported := make([]string, len(supportedLanguages))
	for i, name := range supportedLanguages {
		supported[i] = string(name)
	}
	return fmt.Errorf("unsupported scip-search language %q (supported: %s)", language, strings.Join(supported, ", "))
}

func detectLanguages(files []string) []Language {
	detected := map[Language]bool{}
	for _, file := range files {
		base := filepath.Base(file)
		ext := filepath.Ext(file)
		switch {
		case base == "go.mod" || ext == ".go":
			detected[LanguageGo] = true
		case base == "tsconfig.json" || ext == ".ts" || ext == ".tsx":
			detected[LanguageTypeScript] = true
		case base == "pyproject.toml" || ext == ".py":
			detected[LanguagePython] = true
		}
	}

	var out []Language
	for _, language := range supportedLanguages {
		if detected[language] {
			out = append(out, language)
		}
	}
	return out
}

func filterRuntimeLanguages(configuredLanguages []string, detectedLanguages []Language) []Language {
	configured := make(map[Language]bool, len(configuredLanguages))
	for _, language := range configuredLanguages {
		configured[Language(language)] = true
	}
	detected := make(map[Language]bool, len(detectedLanguages))
	for _, language := range detectedLanguages {
		detected[language] = true
	}

	var out []Language
	for _, language := range supportedLanguages {
		if configured[language] && detected[language] {
			out = append(out, language)
		}
	}
	return out
}

func languageDetected(files []string, language Language) bool {
	return slices.Contains(detectLanguages(files), language)
}
