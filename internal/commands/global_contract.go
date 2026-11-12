package commands

import (
	"os"

	"github.com/liza-mas/liza/internal/providers"
)

// activeContract returns where the provider loads the contract from: its repo
// or local link in projectRoot, else its global link, else "". Doctor and the
// session-start probe share it, so they read a location the same way.
func activeContract(homeDir, projectRoot, contractTarget string, contract providers.ContractLinks) string {
	if active := activeProjectContract(projectRoot, contractTarget, contract); active != "" {
		return active
	}
	if contract.GlobalFallback == "" {
		return ""
	}
	if globalPath, err := contract.GlobalPath(homeDir); err == nil && isLizaSymlink(globalPath, contractTarget) {
		return globalPath
	}
	return ""
}

// LoadedContract reports where providerID's CLI loads CORE.md from in
// projectRoot ("" outside a repository): a link that is in place and whose
// target is a regular file. It returns "" when no link loads it, so a link to
// a missing CORE.md does not count. It never writes or fetches: it reads the
// catalog cache, else the embedded catalog.
func LoadedContract(homeDir, projectRoot, providerID string) (string, error) {
	catalog, _, err := providers.ReadCachedCatalog(homeDir)
	if err != nil {
		catalog = providers.EmbeddedCatalog()
	}
	resolved, err := resolveCatalogProviders(catalog, []string{providerID})
	if err != nil {
		return "", err
	}
	contractTarget := globalContractTarget(homeDir)
	active := activeContract(homeDir, projectRoot, contractTarget, resolved[0].Setup.Contract)
	if active == "" {
		return "", nil
	}
	if info, err := os.Stat(contractTarget); err != nil || !info.Mode().IsRegular() {
		return "", nil
	}
	return active, nil
}
