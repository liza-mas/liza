package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
)

// Check definitions init and setup report (specs/protocols/outcome-checks.md).
// Each covers something the command was asked to apply and could not; the
// command goes on, so every one is a warning.
var (
	initAssetCheck         = jsonout.CheckDefinition{ID: "init.asset", Severity: jsonout.SeverityWarn}
	setupAssetCheck        = jsonout.CheckDefinition{ID: "setup.asset", Severity: jsonout.SeverityWarn}
	setupProviderLinkCheck = jsonout.CheckDefinition{ID: "setup.provider-link", Severity: jsonout.SeverityWarn}
	setupGlobalFileCheck   = jsonout.CheckDefinition{ID: "setup.global-file", Severity: jsonout.SeverityWarn}
)

// reportProviderLinkSkip records a provider link setup could not create;
// reason is empty when the link was created or the user declined.
func reportProviderLinkSkip(report *jsonout.Report, providerID, linkPath, reason string) {
	if reason == "" {
		return
	}
	report.Add(setupProviderLinkCheck.Check(jsonout.CheckFailed, providerID, linkPath,
		fmt.Sprintf("%s is not linked: %s", linkPath, reason),
		fmt.Sprintf("move %s aside, then re-run %s", linkPath, brand.Command("setup"))))
}

// reportGlobalFileKept records a customizable global file setup kept because
// the overwrite answer could not be read.
func reportGlobalFileKept(report *jsonout.Report, path string) {
	report.Add(setupGlobalFileCheck.Check(jsonout.CheckFailed, "", path,
		fmt.Sprintf("kept the existing %s: the overwrite answer could not be read", filepath.Base(path)),
		fmt.Sprintf("re-run %s and answer the prompt, or pass --yes", brand.Command("setup"))))
}

// warnAssetFailure reports an asset the command could not write: the
// human warning on stderr, unchanged, and a failed check of def. command is
// the subcommand to re-run ("init" or "setup").
func warnAssetFailure(report *jsonout.Report, def jsonout.CheckDefinition, command, asset string, err error) {
	message := fmt.Sprintf("failed to write %s: %v", asset, err)
	fmt.Fprintf(os.Stderr, "Warning: %s\n", message)
	report.Add(def.Check(jsonout.CheckFailed, "", "", message,
		fmt.Sprintf("resolve the error, then re-run %s", brand.Command(command))))
}
