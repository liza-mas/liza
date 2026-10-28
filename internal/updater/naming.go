package updater

import (
	"fmt"

	"github.com/liza-mas/liza/internal/brand"
)

const canonicalRepo = "liza-mas/liza"

func cliPrefix() string {
	return nameLower()
}

func binaryName() string {
	return brand.RuntimeValues().BinaryName
}

func nameLower() string {
	return brand.RuntimeValues().NameLower
}

func nameTitle() string {
	return brand.RuntimeValues().NameTitle
}

func releaseRepo() string {
	if brand.ReleaseRepo != "" {
		return brand.ReleaseRepo
	}
	return canonicalRepo
}

func sourceRepo() string {
	if brand.Repo != "" {
		return brand.Repo
	}
	return canonicalRepo
}

func githubLatestReleaseURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", releaseRepo())
}

func githubMainCommitURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/commits/main", sourceRepo())
}

func githubCompareURL(current, latest string) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/compare/%s...%s", sourceRepo(), current, latest)
}

func sourceCloneURL() string {
	return fmt.Sprintf("https://github.com/%s.git", sourceRepo())
}

func sourceCheckoutDirName() string {
	return nameLower()
}

func sourceInstallMakeArgs(repoDir, installDir string) []string {
	values := brand.RuntimeValues()
	return []string{
		"-C", repoDir,
		"install",
		"INSTALL_DIR=" + installDir,
		"BRAND_NAME_TITLE=" + values.NameTitle,
		"BRAND_NAME_LOWER=" + values.NameLower,
		"BRAND_NAME_UPPER=" + values.NameUpper,
		"BRAND_REPO=" + values.Repo,
		"BRAND_BINARY_NAME=" + values.BinaryName,
		"BRAND_GLOBAL_DIRNAME=" + values.GlobalDirName,
		"BRAND_PROJECT_DIRNAME=" + values.ProjectDirName,
		"BRAND_ENV_PREFIX=" + values.EnvPrefix,
		"BRAND_ARCHIVE_PREFIX=" + values.ArchivePrefix,
		"BRAND_RELEASE_REPO=" + values.ReleaseRepo,
		"BRAND_RELEASE_BASE_URL=" + values.ReleaseBaseURL,
		"BRAND_CHECKSUM_BASE_URL=" + values.ChecksumBaseURL,
	}
}
