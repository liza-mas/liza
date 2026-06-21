package brand

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var (
	NameLower       = "liza"
	NameUpper       = "LIZA"
	NameTitle       = "Liza"
	Repo            = "liza-mas/liza"
	BinaryName      = "liza"
	GlobalDirName   = ".liza"
	ProjectDirName  = ".liza"
	EnvPrefix       = "LIZA"
	ArchivePrefix   = "liza"
	ReleaseRepo     = "liza-mas/liza"
	ReleaseBaseURL  = "https://github.com/liza-mas/liza/releases/download"
	ChecksumBaseURL = "https://github.com/liza-mas/liza/releases/download"
)

const (
	legacyEnvPrefix      = "LIZA"
	defaultMistralPrompt = "liza"
)

var (
	lowerRE       = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	upperRE       = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	tokenRE       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	ownerRepoRE   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	dirNameSafeRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	shellSafeText = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._/-]*$`)
)

type Values struct {
	NameLower       string
	NameUpper       string
	NameTitle       string
	Repo            string
	BinaryName      string
	GlobalDirName   string
	ProjectDirName  string
	EnvPrefix       string
	MistralPromptID string
	ArchivePrefix   string
	ReleaseRepo     string
	ReleaseBaseURL  string
	ChecksumBaseURL string
	InstallRepo     string
}

type EnvLookup struct {
	Value   string
	Source  string
	Warning string
}

func RuntimeValues() Values {
	values := Values{
		NameLower:       NameLower,
		NameUpper:       NameUpper,
		NameTitle:       NameTitle,
		Repo:            Repo,
		BinaryName:      BinaryName,
		GlobalDirName:   GlobalDirName,
		ProjectDirName:  ProjectDirName,
		EnvPrefix:       EnvPrefix,
		MistralPromptID: defaultMistralPrompt,
		ArchivePrefix:   ArchivePrefix,
		ReleaseRepo:     ReleaseRepo,
		ReleaseBaseURL:  ReleaseBaseURL,
		ChecksumBaseURL: ChecksumBaseURL,
		InstallRepo:     Repo,
	}
	return values.withDerivedDefaults()
}

func ValuesFromEnv(getenv func(string) string) Values {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	values := Values{
		NameLower: envOr(getenv, "BRAND_NAME_LOWER", "liza"),
		NameUpper: envOr(getenv, "BRAND_NAME_UPPER", "LIZA"),
		NameTitle: envOr(getenv, "BRAND_NAME_TITLE", "Liza"),
		Repo:      envOr(getenv, "BRAND_REPO", "liza-mas/liza"),
	}
	values.BinaryName = envOr(getenv, "BRAND_BINARY_NAME", values.NameLower)
	values.GlobalDirName = envOr(getenv, "BRAND_GLOBAL_DIRNAME", "."+values.NameLower)
	values.ProjectDirName = envOr(getenv, "BRAND_PROJECT_DIRNAME", "."+values.NameLower)
	values.EnvPrefix = envOr(getenv, "BRAND_ENV_PREFIX", values.NameUpper)
	values.MistralPromptID = envOr(getenv, "BRAND_MISTRAL_PROMPT_ID", values.NameLower)
	values.ArchivePrefix = envOr(getenv, "BRAND_ARCHIVE_PREFIX", values.BinaryName)
	values.ReleaseRepo = envOr(getenv, "BRAND_RELEASE_REPO", values.Repo)
	values.ReleaseBaseURL = envOr(getenv, "BRAND_RELEASE_BASE_URL", "https://github.com/"+values.ReleaseRepo+"/releases/download")
	values.ChecksumBaseURL = envOr(getenv, "BRAND_CHECKSUM_BASE_URL", values.ReleaseBaseURL)
	values.InstallRepo = envOr(getenv, "BRAND_INSTALL_REPO", values.Repo)
	return values.withDerivedDefaults()
}

func (v Values) withDerivedDefaults() Values {
	if v.BinaryName == "" {
		v.BinaryName = v.NameLower
	}
	if v.GlobalDirName == "" && v.NameLower != "" {
		v.GlobalDirName = "." + v.NameLower
	}
	if v.ProjectDirName == "" && v.NameLower != "" {
		v.ProjectDirName = "." + v.NameLower
	}
	if v.EnvPrefix == "" {
		v.EnvPrefix = v.NameUpper
	}
	if v.MistralPromptID == "" {
		v.MistralPromptID = v.NameLower
	}
	if v.ArchivePrefix == "" {
		v.ArchivePrefix = v.BinaryName
	}
	if v.ReleaseRepo == "" {
		v.ReleaseRepo = v.Repo
	}
	if v.ReleaseBaseURL == "" && v.ReleaseRepo != "" {
		v.ReleaseBaseURL = "https://github.com/" + v.ReleaseRepo + "/releases/download"
	}
	if v.ChecksumBaseURL == "" {
		v.ChecksumBaseURL = v.ReleaseBaseURL
	}
	if v.InstallRepo == "" {
		v.InstallRepo = v.Repo
	}
	return v
}

func Normalize(v Values) Values {
	return v.withDerivedDefaults()
}

func envOr(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func Validate(v Values) error {
	v = v.withDerivedDefaults()
	checks := []struct {
		name  string
		value string
		valid bool
		why   string
	}{
		{"BRAND_NAME_LOWER", v.NameLower, lowerRE.MatchString(v.NameLower), "must match ^[a-z][a-z0-9-]*$"},
		{"BRAND_NAME_UPPER", v.NameUpper, upperRE.MatchString(v.NameUpper), "must match ^[A-Z][A-Z0-9_]*$"},
		{"BRAND_ENV_PREFIX", v.EnvPrefix, upperRE.MatchString(v.EnvPrefix), "must match ^[A-Z][A-Z0-9_]*$"},
		{"BRAND_BINARY_NAME", v.BinaryName, tokenRE.MatchString(v.BinaryName), "must match ^[A-Za-z0-9][A-Za-z0-9._-]*$"},
		{"BRAND_ARCHIVE_PREFIX", v.ArchivePrefix, tokenRE.MatchString(v.ArchivePrefix), "must match ^[A-Za-z0-9][A-Za-z0-9._-]*$"},
		{"BRAND_MISTRAL_PROMPT_ID", v.MistralPromptID, tokenRE.MatchString(v.MistralPromptID), "must match ^[A-Za-z0-9][A-Za-z0-9._-]*$"},
		{"BRAND_REPO", v.Repo, ownerRepoRE.MatchString(v.Repo), "must use owner/repo form with no URL scheme"},
		{"BRAND_RELEASE_REPO", v.ReleaseRepo, ownerRepoRE.MatchString(v.ReleaseRepo), "must use owner/repo form with no URL scheme"},
		{"BRAND_INSTALL_REPO", v.InstallRepo, ownerRepoRE.MatchString(v.InstallRepo), "must use owner/repo form with no URL scheme"},
	}
	for _, check := range checks {
		if !check.valid {
			return fmt.Errorf("%s invalid: %s", check.name, check.why)
		}
	}
	if err := validatePrintable("BRAND_NAME_TITLE", v.NameTitle); err != nil {
		return err
	}
	if err := validateShellSafe("BRAND_NAME_TITLE", v.NameTitle); err != nil {
		return err
	}
	for name, dir := range map[string]string{
		"BRAND_GLOBAL_DIRNAME":  v.GlobalDirName,
		"BRAND_PROJECT_DIRNAME": v.ProjectDirName,
	} {
		if err := validateDirName(name, dir); err != nil {
			return err
		}
	}
	for name, rawURL := range map[string]string{
		"BRAND_RELEASE_BASE_URL":  v.ReleaseBaseURL,
		"BRAND_CHECKSUM_BASE_URL": v.ChecksumBaseURL,
	} {
		if err := validateHTTPSURL(name, rawURL); err != nil {
			return err
		}
	}
	return nil
}

func validatePrintable(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s invalid: must be non-empty printable text with no newline", name)
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || !unicode.IsPrint(r) {
			return fmt.Errorf("%s invalid: must be non-empty printable text with no newline", name)
		}
	}
	return nil
}

func validateShellSafe(name, value string) error {
	if !shellSafeText.MatchString(value) {
		return fmt.Errorf("%s invalid: contains characters unsafe for generated shell/config snippets", name)
	}
	return nil
}

func validateDirName(name, value string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("%s invalid: must be a single directory name", name)
	}
	if strings.ContainsAny(value, `/\`+"\x00") {
		return fmt.Errorf("%s invalid: must not contain path separators or NUL", name)
	}
	if !dirNameSafeRE.MatchString(value) {
		return fmt.Errorf("%s invalid: contains characters unsafe for generated shell/config snippets", name)
	}
	return nil
}

func validateHTTPSURL(name, raw string) error {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return fmt.Errorf("%s invalid: must be an absolute https:// URL with no whitespace", name)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || !parsed.IsAbs() {
		return fmt.Errorf("%s invalid: must be an absolute https:// URL with no whitespace", name)
	}
	if strings.ContainsAny(raw, "\"'`$\\") {
		return fmt.Errorf("%s invalid: contains characters unsafe for generated shell/config snippets", name)
	}
	return nil
}

func MacroMap(v Values) map[string]string {
	v = v.withDerivedDefaults()
	return map[string]string{
		"BRAND_NAME_LOWER":        v.NameLower,
		"BRAND_NAME_UPPER":        v.NameUpper,
		"BRAND_NAME_TITLE":        v.NameTitle,
		"BRAND_REPO":              v.Repo,
		"BRAND_BINARY_NAME":       v.BinaryName,
		"BRAND_GLOBAL_DIRNAME":    v.GlobalDirName,
		"BRAND_PROJECT_DIRNAME":   v.ProjectDirName,
		"BRAND_ENV_PREFIX":        v.EnvPrefix,
		"BRAND_MISTRAL_PROMPT_ID": v.MistralPromptID,
		"BRAND_ARCHIVE_PREFIX":    v.ArchivePrefix,
		"BRAND_RELEASE_REPO":      v.ReleaseRepo,
		"BRAND_RELEASE_BASE_URL":  v.ReleaseBaseURL,
		"BRAND_CHECKSUM_BASE_URL": v.ChecksumBaseURL,
		"BRAND_INSTALL_REPO":      v.InstallRepo,
	}
}

func EnvName(suffix string) string {
	return RuntimeValues().EnvName(suffix)
}

func (v Values) EnvName(suffix string) string {
	suffix = strings.TrimPrefix(suffix, "_")
	return v.withDerivedDefaults().EnvPrefix + "_" + suffix
}

func LookupEnv(getenv func(string) string, suffix string) EnvLookup {
	return RuntimeValues().LookupEnv(getenv, suffix)
}

func (v Values) LookupEnv(getenv func(string) string, suffix string) EnvLookup {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	v = v.withDerivedDefaults()
	brandedName := v.EnvName(suffix)
	legacyName := legacyEnvPrefix + "_" + strings.TrimPrefix(suffix, "_")
	branded := strings.TrimSpace(getenv(brandedName))
	legacy := strings.TrimSpace(getenv(legacyName))
	if branded != "" {
		out := EnvLookup{Value: branded, Source: brandedName}
		if legacyName != brandedName && legacy != "" && legacy != branded {
			out.Warning = fmt.Sprintf("%s and %s are both set; using %s", brandedName, legacyName, brandedName)
		}
		return out
	}
	if legacy != "" {
		return EnvLookup{Value: legacy, Source: legacyName}
	}
	return EnvLookup{}
}
