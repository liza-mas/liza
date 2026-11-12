package commands

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/toolchain"
)

// Check definitions doctor reports. Doctor checks the end state setup and
// init leave, so a check may restate a step check those commands report.
var (
	doctorGlobalFileCheck     = jsonout.CheckDefinition{ID: "doctor.global-file", Severity: jsonout.SeverityWarn}
	doctorSkillLinkCheck      = jsonout.CheckDefinition{ID: "doctor.skill-link", Severity: jsonout.SeverityWarn}
	doctorProviderLinkCheck   = jsonout.CheckDefinition{ID: "doctor.provider-link", Severity: jsonout.SeverityWarn}
	doctorCatalogCheck        = jsonout.CheckDefinition{ID: "doctor.provider-catalog", Severity: jsonout.SeverityWarn}
	doctorToolchainCheck      = jsonout.CheckDefinition{ID: "doctor.toolchain-profile", Severity: jsonout.SeverityWarn}
	doctorGlobalContractCheck = jsonout.CheckDefinition{ID: "doctor.global-contract", Severity: jsonout.SeverityWarn}
	doctorRepoContractCheck   = jsonout.CheckDefinition{ID: "doctor.repo-contract", Severity: jsonout.SeverityWarn}
	doctorActivationCheck     = jsonout.CheckDefinition{ID: "doctor.activation-state", Severity: jsonout.SeverityWarn}
	doctorRepoAssetCheck      = jsonout.CheckDefinition{ID: "doctor.repo-asset", Severity: jsonout.SeverityWarn}
	doctorIndexHooksCheck     = jsonout.CheckDefinition{ID: "doctor.index-hooks", Severity: jsonout.SeverityWarn}
	doctorWorkspaceCheck      = jsonout.CheckDefinition{ID: "doctor.workspace-state", Severity: jsonout.SeverityWarn}
)

// ToolchainOutcomeCheck is a toolchain tool's shared outcome check
// (specs/protocols/outcome-checks.md): id "toolchain.<tool-id>". Toolchain
// tools are optional, so a tool that is not ok is a warning. Both doctors
// emit it.
func ToolchainOutcomeCheck(check toolchain.DoctorCheck) jsonout.Check {
	definition := jsonout.CheckDefinition{ID: "toolchain." + check.ToolID, Severity: jsonout.SeverityWarn}
	return definition.Check(jsonout.CheckStatus(check.Status), "", check.Path, check.Message, "")
}

// DoctorOptions selects what Doctor checks.
type DoctorOptions struct {
	HomeDir   string // home directory ("" = paths.UserHomeDir())
	GlobalDir string // branded global directory ("" = <home>/<global dir name>)
	// SkillProviders have their skill links checked in addition to the
	// providers setup left configured.
	SkillProviders []string
	// ContractProviders have their contract checked: the global location,
	// and with ProjectRoot their repo contract, activation state and assets.
	ContractProviders []string
	// ProjectRoot enables the repository checks ("" = none).
	ProjectRoot string
	// Toolchain checks the tools of the configured toolchain profile.
	Toolchain       bool
	ToolchainRunner toolchain.Runner // nil = the real runner
}

// Doctor reports, as checks, whether the files and links setup and init
// produce are in place. It never writes, repairs or fetches anything: it reads
// the provider catalog cache instead of loading it, and it shares every
// read-only inspection with the code that installs what it checks. The error
// return is for a run that cannot check at all, such as an unknown provider.
func Doctor(opts DoctorOptions, report *jsonout.Report) error {
	homeDir := opts.HomeDir
	if homeDir == "" {
		var err error
		if homeDir, err = paths.UserHomeDir(); err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
	}
	globalDir := opts.GlobalDir
	if globalDir == "" {
		globalDir = filepath.Join(homeDir, paths.GlobalDirName())
	}
	d := doctorRun{homeDir: homeDir, globalDir: globalDir, report: report}

	catalog := d.checkCatalog()
	skillProviders, err := resolveCatalogProviders(catalog, opts.SkillProviders)
	if err != nil {
		return err
	}
	contractProviders, err := resolveCatalogProviders(catalog, opts.ContractProviders)
	if err != nil {
		return err
	}

	plannedSkills, err := embedded.PlannedSkillDirs(globalDir)
	if err != nil {
		return err
	}
	d.checkGlobalFiles()
	linked := configuredSkillProviders(catalog, homeDir, globalDir)
	for _, provider := range append(skillProviders, contractProviders...) {
		if !slices.ContainsFunc(linked, func(p providers.Provider) bool { return p.ID == provider.ID }) {
			linked = append(linked, provider)
		}
	}
	for _, provider := range linked {
		d.checkProviderLinks(provider, plannedSkills)
	}
	if opts.Toolchain {
		d.checkToolchain(opts.ToolchainRunner)
	}
	for _, provider := range contractProviders {
		d.checkGlobalContract(provider)
	}
	if opts.ProjectRoot != "" {
		d.checkRepo(opts.ProjectRoot, contractProviders)
	}
	return nil
}

type doctorRun struct {
	homeDir   string
	globalDir string
	report    *jsonout.Report
}

// checkCatalog reports the provider catalog cache and returns the catalog to
// resolve providers with: the cache when it parses, else the embedded one,
// which is what loading falls back to as well.
func (d doctorRun) checkCatalog() providers.Catalog {
	catalog, path, err := providers.ReadCachedCatalog(d.homeDir)
	switch {
	case err == nil:
		d.report.Add(doctorCatalogCheck.Check(jsonout.CheckOK, "", path, "provider catalog cache is readable", ""))
		return catalog
	case errors.Is(err, os.ErrNotExist):
		d.report.Add(doctorCatalogCheck.Check(jsonout.CheckOK, "", path, "no provider catalog cache; the embedded catalog is in use", ""))
	default:
		d.report.Add(doctorCatalogCheck.Check(jsonout.CheckFailed, "", path,
			fmt.Sprintf("provider catalog cache cannot be read: %v", err),
			fmt.Sprintf("run %s", brand.Command("providers", "refresh"))))
	}
	return providers.EmbeddedCatalog()
}

// checkGlobalFiles reports each unit setup writes to the global directory:
// every contract file, every skill directory, the support docs and the
// pipeline configuration. A unit is ok when all its files are regular files.
func (d doctorRun) checkGlobalFiles() {
	units := map[string][]string{}
	var order []string
	for _, path := range embedded.PlanGlobalFiles(d.globalDir) {
		unit := globalFileUnit(d.globalDir, path)
		if _, seen := units[unit]; !seen {
			order = append(order, unit)
		}
		units[unit] = append(units[unit], path)
	}
	pipeline := embedded.PipelineConfigPath(d.globalDir)
	order = append(order, pipeline)
	units[pipeline] = []string{pipeline}

	for _, unit := range order {
		d.report.Add(globalUnitCheck(doctorGlobalFileCheck, unit, units[unit]))
	}
}

// globalFileUnit groups a planned file by skill directory, top-level directory
// or, for a file at the top of the global directory, the file itself.
func globalFileUnit(globalDir, path string) string {
	rel, err := filepath.Rel(globalDir, path)
	if err != nil {
		return path
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch {
	case len(parts) > 2 && parts[0] == "skills":
		return filepath.Join(globalDir, parts[0], parts[1])
	case len(parts) > 1:
		return filepath.Join(globalDir, parts[0])
	default:
		return path
	}
}

func globalUnitCheck(definition jsonout.CheckDefinition, unit string, files []string) jsonout.Check {
	fix := fmt.Sprintf("re-run %s", brand.Command("setup"))
	for _, file := range files {
		info, err := os.Stat(file)
		if os.IsNotExist(err) {
			return definition.Check(jsonout.CheckMissing, "", file, fmt.Sprintf("%s is missing", file), fix)
		}
		if err != nil {
			return definition.Check(jsonout.CheckFailed, "", file, fmt.Sprintf("cannot read %s: %v", file, err), fix)
		}
		if !info.Mode().IsRegular() {
			return definition.Check(jsonout.CheckFailed, "", file, fmt.Sprintf("%s is not a regular file", file), fix)
		}
	}
	return definition.Check(jsonout.CheckOK, "", unit, fmt.Sprintf("%s is installed", unit), "")
}

// checkProviderLinks reports the links setup creates for a provider: one per
// release skill, as setup links every skill of the global directory, and the
// provider's extra links.
func (d doctorRun) checkProviderLinks(provider providers.Provider, plannedSkills map[string]bool) {
	cfg := provider.Setup
	if cfg.ConfigDir == "" || cfg.SkillsDir == "" {
		return
	}
	configDir := filepath.Join(d.homeDir, cfg.ConfigDir)
	skillsDir := filepath.Join(configDir, cfg.SkillsDir)
	for _, skillDir := range slices.Sorted(maps.Keys(plannedSkills)) {
		d.report.Add(linkCheck(doctorSkillLinkCheck, provider.ID, filepath.Join(skillsDir, filepath.Base(skillDir)), skillDir))
	}
	for _, extra := range cfg.Symlinks {
		d.report.Add(linkCheck(doctorProviderLinkCheck, provider.ID, filepath.Join(configDir, extra.Target), filepath.Join(d.globalDir, extra.Source)))
	}
}

// linkCheck compares the link setup creates at linkPath with what is there.
func linkCheck(definition jsonout.CheckDefinition, providerID, linkPath, target string) jsonout.Check {
	fix := fmt.Sprintf("re-run %s", brand.Command("setup", "--provider", providerID))
	state, err := inspectLink(linkPath, target)
	if err != nil {
		return definition.Check(jsonout.CheckFailed, providerID, linkPath, err.Error(), fix)
	}
	switch state.Status {
	case linkLinked:
		return definition.Check(jsonout.CheckOK, providerID, linkPath, fmt.Sprintf("%s links to %s", linkPath, target), "")
	case linkAbsent:
		return definition.Check(jsonout.CheckMissing, providerID, linkPath, fmt.Sprintf("%s is not linked", linkPath), fix)
	case linkElsewhere:
		return definition.Check(jsonout.CheckFailed, providerID, linkPath,
			fmt.Sprintf("%s links to %s instead of %s", linkPath, state.Target, target), fix)
	default:
		return definition.Check(jsonout.CheckFailed, providerID, linkPath,
			fmt.Sprintf("%s is occupied by a %s", linkPath, state.Kind),
			fmt.Sprintf("move %s aside, then %s", linkPath, fix))
	}
}

// checkToolchain reports the tools the configured toolchain profile selected.
// It runs nothing when the toolchain was never configured, and skips the
// Semble model query, which writes a temporary fixture.
func (d doctorRun) checkToolchain(runner toolchain.Runner) {
	ids, err := toolchain.ReadConfiguredTools(d.globalDir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		d.report.Add(doctorToolchainCheck.Check(jsonout.CheckFailed, "", toolchain.ConfiguredProfilePath(d.globalDir),
			fmt.Sprintf("toolchain profile cannot be read: %v", err),
			fmt.Sprintf("re-run %s", brand.Command("toolchain", "configure"))))
		return
	}
	for _, check := range toolchain.DoctorTools(ids, runner) {
		d.report.Add(ToolchainOutcomeCheck(check))
	}
}

// checkGlobalContract reports a provider's global contract location. A
// global-first provider must link it; a repo-first provider only must not
// find it occupied, since its contract location is the repo file.
func (d doctorRun) checkGlobalContract(provider providers.Provider) {
	contract := provider.Setup.Contract
	if contract.GlobalFallback == "" {
		return
	}
	fix := fmt.Sprintf("re-run %s in a repository", brand.Command("init"))
	globalPath, err := contract.GlobalPath(d.homeDir)
	if err != nil {
		d.report.Add(doctorGlobalContractCheck.Check(jsonout.CheckFailed, provider.ID, "",
			fmt.Sprintf("%s global contract location cannot be resolved: %v", provider.ID, err),
			"fix the provider's global configuration directory, then "+fix))
		return
	}
	state, err := inspectLink(globalPath, globalContractTarget(d.homeDir))
	if err != nil {
		d.report.Add(doctorGlobalContractCheck.Check(jsonout.CheckFailed, provider.ID, globalPath, err.Error(), fix))
		return
	}
	switch state.Status {
	case linkLinked:
		d.report.Add(doctorGlobalContractCheck.Check(jsonout.CheckOK, provider.ID, globalPath,
			fmt.Sprintf("%s contract is active at %s", brand.NameTitle, globalPath), ""))
	case linkAbsent:
		if !contract.PrefersGlobal() {
			d.report.Add(doctorGlobalContractCheck.Check(jsonout.CheckOK, provider.ID, globalPath,
				fmt.Sprintf("%s is not linked; %s loads its repository contract", globalPath, provider.ID), ""))
			return
		}
		d.report.Add(doctorGlobalContractCheck.Check(jsonout.CheckMissing, provider.ID, globalPath,
			fmt.Sprintf("%s contract is not linked at %s", brand.NameTitle, globalPath), fix))
	default:
		d.report.Add(doctorGlobalContractCheck.Check(jsonout.CheckFailed, provider.ID, globalPath,
			fmt.Sprintf("%s is occupied by a non-%s %s; the %s contract is not active there", globalPath, brand.NameTitle, state.Kind, brand.NameTitle),
			fmt.Sprintf("move %s aside, then %s", globalPath, fix)))
	}
}

// checkRepo reports what init leaves in the repository for the providers it
// activated: where each contract is active, the activation state, the
// provider assets and the index lifecycle hooks.
func (d doctorRun) checkRepo(projectRoot string, contractProviders []providers.Provider) {
	contractTarget := globalContractTarget(d.homeDir)
	repoContract := false
	for _, provider := range contractProviders {
		if provider.Setup.Contract.RepoFile == "" {
			continue
		}
		repoContract = true
		d.report.Add(d.repoContractCheck(projectRoot, contractTarget, provider))
	}
	if repoContract {
		d.report.Add(activationStateCheck(projectRoot))
	}
	for _, asset := range repoAssets(projectRoot, contractProviders) {
		d.report.Add(asset)
	}
	workspace := readWorkspaceState(projectRoot)
	if workspace.mas {
		d.report.Add(workspace.check)
	}
	if check, ok := indexHooksCheck(projectRoot, workspace); ok {
		d.report.Add(check)
	}
}

// workspaceState is what doctor knows of a MAS workspace in a repository.
// mas is set when MAS init's own outputs are there (the frozen pipeline or
// the models file): a runtime directory alone does not count, since pairing
// repositories have one too. state is nil when it could not be read.
type workspaceState struct {
	mas   bool
	state *models.State
	check jsonout.Check
}

// readWorkspaceState reads the MAS state lock-free, as the orchestrator does,
// when the repository holds a MAS workspace.
func readWorkspaceState(projectRoot string) workspaceState {
	lizaPaths := paths.New(projectRoot)
	var w workspaceState
	for _, marker := range []string{lizaPaths.FrozenPipelinePath(), lizaPaths.ModelsPath()} {
		if _, err := os.Stat(marker); err == nil {
			w.mas = true
		}
	}
	if !w.mas {
		return w
	}
	statePath := lizaPaths.StatePath()
	fix := fmt.Sprintf("restore %s from a backup, or re-run %s", statePath, brand.Command("init"))
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		w.check = doctorWorkspaceCheck.Check(jsonout.CheckMissing, "", statePath, "workspace state is missing", fix)
		return w
	}
	state, err := db.New(statePath).ReadSnapshot()
	if err != nil {
		w.check = doctorWorkspaceCheck.Check(jsonout.CheckFailed, "", statePath, fmt.Sprintf("cannot read the workspace state: %v", err), fix)
		return w
	}
	w.state = state
	w.check = doctorWorkspaceCheck.Check(jsonout.CheckOK, "", statePath, "workspace state is readable", "")
	return w
}

func (d doctorRun) repoContractCheck(projectRoot, contractTarget string, provider providers.Provider) jsonout.Check {
	contract := provider.Setup.Contract
	active := activeContract(d.homeDir, projectRoot, contractTarget, contract)
	if active != "" {
		return doctorRepoContractCheck.Check(jsonout.CheckOK, provider.ID, active,
			fmt.Sprintf("%s contract is active at %s", brand.NameTitle, active), "")
	}
	repoPath := filepath.Join(projectRoot, contract.RepoFile)
	return doctorRepoContractCheck.Check(jsonout.CheckMissing, provider.ID, repoPath,
		fmt.Sprintf("%s contract is not active for %s in this repository", brand.NameTitle, provider.ID),
		fmt.Sprintf("re-run %s", brand.Command("init")))
}

func activationStateCheck(projectRoot string) jsonout.Check {
	fix := fmt.Sprintf("re-run %s", brand.Command("init"))
	statePath, err := repoContractActivationStatePath(projectRoot)
	if err != nil {
		return doctorActivationCheck.Check(jsonout.CheckFailed, "", "", err.Error(), fix)
	}
	_, exists, err := readRepoContractActivationState(statePath)
	switch {
	case err != nil:
		return doctorActivationCheck.Check(jsonout.CheckFailed, "", statePath, err.Error(), fix)
	case !exists:
		return doctorActivationCheck.Check(jsonout.CheckMissing, "", statePath, "provider contract activation state is missing", fix)
	default:
		return doctorActivationCheck.Check(jsonout.CheckOK, "", statePath, "provider contract activation state is readable", "")
	}
}

// repoAssets reports each file the providers' activation assets install, the
// first provider declaring an asset owning its checks. Presence only: a
// declined prompt and a failed write both leave a file missing.
func repoAssets(projectRoot string, selected []providers.Provider) []jsonout.Check {
	assets := []struct {
		declared func(providers.ActivationAssets) bool
		files    func() ([]string, error)
	}{
		{func(a providers.ActivationAssets) bool { return a.ClaudeSettings },
			func() ([]string, error) { return embedded.ClaudeAssetPaths(projectRoot), nil }},
		{func(a providers.ActivationAssets) bool { return a.CodexConfig || a.CodexHooks },
			func() ([]string, error) { return embedded.CodexProjectAssetPaths(projectRoot), nil }},
		{func(a providers.ActivationAssets) bool { return a.CopilotHooks },
			func() ([]string, error) { return embedded.CopilotProjectAssetPaths(projectRoot), nil }},
		{func(a providers.ActivationAssets) bool { return a.OpenCodeExecTool },
			func() ([]string, error) { return embedded.OpenCodeAssetPaths(projectRoot), nil }},
		{func(a providers.ActivationAssets) bool { return a.PiExtension },
			func() ([]string, error) {
				path, err := embedded.PiInitGatePath()
				return []string{path}, err
			}},
	}
	var checks []jsonout.Check
	fix := fmt.Sprintf("re-run %s", brand.Command("init"))
	for _, asset := range assets {
		index := slices.IndexFunc(selected, func(p providers.Provider) bool { return asset.declared(p.Setup.ActivationAssets) })
		if index < 0 {
			continue
		}
		owner := selected[index].ID
		files, err := asset.files()
		if err != nil {
			checks = append(checks, doctorRepoAssetCheck.Check(jsonout.CheckFailed, owner, "", err.Error(), fix))
			continue
		}
		for _, file := range files {
			info, err := os.Stat(file)
			switch {
			case os.IsNotExist(err):
				checks = append(checks, doctorRepoAssetCheck.Check(jsonout.CheckMissing, owner, file, fmt.Sprintf("%s is missing", file), fix))
			case err != nil:
				checks = append(checks, doctorRepoAssetCheck.Check(jsonout.CheckFailed, owner, file, fmt.Sprintf("cannot read %s: %v", file, err), fix))
			case !info.Mode().IsRegular():
				checks = append(checks, doctorRepoAssetCheck.Check(jsonout.CheckFailed, owner, file, fmt.Sprintf("%s is not a regular file", file), fix))
			default:
				checks = append(checks, doctorRepoAssetCheck.Check(jsonout.CheckOK, owner, file, fmt.Sprintf("%s is installed", file), ""))
			}
		}
	}
	return checks
}

// indexHooksCheck reports the index lifecycle hooks when init would have
// installed them: when the plan the matching init uses is active. A MAS
// workspace plans with its configured scip_search languages; without a
// readable state its plan is unknown and doctor.workspace-state reports why.
// Any other repository plans as pairing init does. Presence only:
// CheckActivation's content comparison is not run, so a hook set the
// configuration has moved past is not reported.
func indexHooksCheck(projectRoot string, workspace workspaceState) (jsonout.Check, bool) {
	fix := fmt.Sprintf("re-run %s", brand.Command("init"))
	options := pairingIndexActivationOptions(projectRoot, nil, nil)
	if workspace.mas {
		if workspace.state == nil {
			return jsonout.Check{}, false
		}
		options = pairingindex.MASActivationOptions(projectRoot, workspace.state.Config.ScipSearch)
	}
	plan, err := pairingindex.PlanActivation(options)
	if err != nil {
		return doctorIndexHooksCheck.Check(jsonout.CheckFailed, "", "", err.Error(), fix), true
	}
	if !plan.Active() {
		return jsonout.Check{}, false
	}
	missing, err := pairingindex.CheckInstalled(plan.Install)
	switch {
	case err != nil:
		return doctorIndexHooksCheck.Check(jsonout.CheckFailed, "", "", err.Error(), fix), true
	case missing != "":
		return doctorIndexHooksCheck.Check(jsonout.CheckMissing, "", missing, fmt.Sprintf("index lifecycle hook file %s is missing", missing), fix), true
	default:
		return doctorIndexHooksCheck.Check(jsonout.CheckOK, "", "", "index lifecycle hooks are installed", ""), true
	}
}

// configuredSkillProviders returns, in catalog order, the CLI providers whose
// skills directory holds at least one link resolving inside globalDir/skills.
// That is how setup leaves a provider it was run for.
func configuredSkillProviders(cat providers.Catalog, homeDir, globalDir string) []providers.Provider {
	globalSkillsDir := filepath.Join(globalDir, "skills")
	globalResolved, err := filepath.EvalSymlinks(globalSkillsDir)
	if err != nil {
		globalResolved = filepath.Clean(globalSkillsDir)
	}

	var configured []providers.Provider
	for _, p := range cat.ProvidersSorted() {
		if p.Backend != "cli" || p.Setup.ConfigDir == "" || p.Setup.SkillsDir == "" {
			continue
		}
		skillsDir := filepath.Join(homeDir, p.Setup.ConfigDir, p.Setup.SkillsDir)
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join(skillsDir, entry.Name()))
			if err != nil {
				continue
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(skillsDir, target)
			}
			resolved, err := filepath.EvalSymlinks(target)
			if err != nil {
				resolved = filepath.Clean(target)
			}
			relative, err := filepath.Rel(globalResolved, resolved)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				configured = append(configured, p)
				break
			}
		}
	}
	return configured
}
