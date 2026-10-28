package a8

import "github.com/liza-mas/liza/internal/brand"

// useAcmeBrand switches the runtime brand to a non-default one and returns the
// function restoring the previous values. Callers must not run in parallel.
func useAcmeBrand() (restore func()) {
	previous := [...]string{
		brand.NameLower, brand.NameUpper, brand.NameTitle, brand.Repo, brand.BinaryName,
		brand.GlobalDirName, brand.ProjectDirName, brand.EnvPrefix, brand.ArchivePrefix,
		brand.ReleaseRepo, brand.ReleaseBaseURL, brand.ChecksumBaseURL,
	}
	brand.NameLower, brand.NameUpper, brand.NameTitle = "acme", "ACME", "Acme"
	brand.Repo, brand.BinaryName = "acme-org/acme", "acme"
	brand.GlobalDirName, brand.ProjectDirName = ".acme", ".acme"
	brand.EnvPrefix, brand.ArchivePrefix = "ACME", "acme"
	brand.ReleaseRepo = "acme-org/acme"
	brand.ReleaseBaseURL = "https://example.invalid/acme-org/acme/releases/download"
	brand.ChecksumBaseURL = "https://example.invalid/acme-org/acme/releases/download"
	return func() {
		brand.NameLower, brand.NameUpper, brand.NameTitle, brand.Repo, brand.BinaryName,
			brand.GlobalDirName, brand.ProjectDirName, brand.EnvPrefix, brand.ArchivePrefix,
			brand.ReleaseRepo, brand.ReleaseBaseURL, brand.ChecksumBaseURL =
			previous[0], previous[1], previous[2], previous[3], previous[4],
			previous[5], previous[6], previous[7], previous[8],
			previous[9], previous[10], previous[11]
	}
}
