package agent

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/rolemodels"
)

// Launch selection sources, reported by --explain-launch.
const (
	SelectionSourceFlag       = "flag"
	SelectionSourceProfile    = "profile"
	SelectionSourceModelsFile = "models-file"
	SelectionSourceDefault    = "default"
)

// LaunchSelectionRequest holds the explicit flags and configuration an agent
// start resolves its CLI, model and profile from.
type LaunchSelectionRequest struct {
	Role       string
	RoleType   string
	CLI        string
	CLIChanged bool
	Model      string
	Profile    string
	// Item is the 1-based models.yaml list item a slot start runs; 0 for none.
	Item       int
	Config     models.Config
	RoleModels rolemodels.File
}

// LaunchSelection is the single effective choice an agent launches with.
// Model is set only when it is passed as a first-class model; a profile is
// never applied together with one, so the launch carries one model at most.
type LaunchSelection struct {
	CLI     string
	Model   string
	Profile ResolvedProfile
	Source  string
}

// ResolveLaunchSelection applies the first matching rule:
//  0. --models-item: that item of the role's entry list, alone.
//  1. --profile: the profile, with --cli or its CLI (as before); --model is refused.
//  2. --model: that model on --cli, else the covering entry's CLI, else the role default CLI.
//  3. --cli: that CLI with the default profile's vars (as before).
//  4. models.yaml roles.<role>, else defaults.<type>: its CLI and model, no profile.
//  5. The default profile and CLI chain (as before).
func ResolveLaunchSelection(req LaunchSelectionRequest) (LaunchSelection, error) {
	model := strings.TrimSpace(req.Model)
	selection, covered := req.RoleModels.For(req.Role, req.RoleType)
	var entry rolemodels.Entry
	if covered {
		entry = selection.First()
	}
	var sel LaunchSelection
	switch {
	case req.Item != 0:
		if req.CLIChanged || model != "" || strings.TrimSpace(req.Profile) != "" {
			return LaunchSelection{}, fmt.Errorf("--models-item cannot be combined with --cli, --model or --profile")
		}
		if !covered || !selection.List {
			return LaunchSelection{}, fmt.Errorf("--models-item needs a models.yaml entry list for role %s", req.Role)
		}
		if req.Item < 1 || req.Item > len(selection.Items) {
			return LaunchSelection{}, fmt.Errorf("--models-item %d is out of range: role %s lists %d entries", req.Item, req.Role, len(selection.Items))
		}
		item := selection.Items[req.Item-1]
		sel = LaunchSelection{CLI: item.CLI, Model: item.Model, Source: SelectionSourceModelsFile}
	case strings.TrimSpace(req.Profile) != "":
		if model != "" {
			return LaunchSelection{}, fmt.Errorf("--model cannot be combined with --profile; set the model in the profile")
		}
		profile, err := ResolveProfileForRole(req.Profile, req.RoleType, req.Config)
		if err != nil {
			return LaunchSelection{}, err
		}
		cli, err := ResolveCLIWithProfile(req.CLIChanged, req.CLI, profile, req.RoleType, req.Config)
		if err != nil {
			return LaunchSelection{}, err
		}
		sel = LaunchSelection{CLI: cli, Profile: profile, Source: SelectionSourceProfile}
	case model != "":
		cli := req.CLI
		if !req.CLIChanged {
			if covered {
				cli = entry.CLI
			} else {
				cli = ResolveDefaultCLIForRole(req.RoleType, cliResolutionConfig(req.Config))
			}
		}
		sel = LaunchSelection{CLI: cli, Model: model, Source: SelectionSourceFlag}
	case req.CLIChanged:
		profile, err := ResolveProfileForRole("", req.RoleType, req.Config)
		if err != nil {
			return LaunchSelection{}, err
		}
		sel = LaunchSelection{CLI: req.CLI, Profile: profile, Source: SelectionSourceFlag}
	case covered:
		sel = LaunchSelection{CLI: entry.CLI, Model: entry.Model, Source: SelectionSourceModelsFile}
	default:
		profile, err := ResolveProfileForRole("", req.RoleType, req.Config)
		if err != nil {
			return LaunchSelection{}, err
		}
		cli, err := ResolveCLIWithProfile(false, "", profile, req.RoleType, req.Config)
		if err != nil {
			return LaunchSelection{}, err
		}
		sel = LaunchSelection{CLI: cli, Profile: profile, Source: SelectionSourceDefault}
	}
	if tool, ok := AgentToolRegistry(req.Config)[sel.CLI]; ok && sel.Model != "" && !SupportsModelSelection(tool) {
		return LaunchSelection{}, fmt.Errorf("%s does not support model selection", sel.CLI)
	}
	return sel, nil
}

func cliResolutionConfig(config models.Config) CLIResolutionConfig {
	return CLIResolutionConfig{
		DefaultCLI:         config.DefaultCLI,
		DefaultDoerCLI:     config.DefaultDoerCLI,
		DefaultReviewerCLI: config.DefaultReviewerCLI,
	}
}
