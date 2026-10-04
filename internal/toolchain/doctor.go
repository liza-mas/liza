package toolchain

import (
	"fmt"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

type DoctorOptions struct {
	Profile Profile
	Include []string
	Exclude []string
	ToolID  string
	Runner  Runner
}

type DoctorStatus string

const (
	DoctorOK      DoctorStatus = "ok"
	DoctorMissing DoctorStatus = "missing"
	DoctorFailed  DoctorStatus = "failed"
	DoctorManual  DoctorStatus = "manual"
)

type DoctorCheck struct {
	ToolID  string        `json:"tool_id"`
	Status  DoctorStatus  `json:"status"`
	Path    string        `json:"path,omitempty"`
	Message string        `json:"message,omitempty"`
	Output  CommandOutput `json:"output,omitempty"`
}

type DoctorResult struct {
	Profile Profile       `json:"profile"`
	Checks  []DoctorCheck `json:"checks"`
}

func Doctor(opts DoctorOptions) (DoctorResult, error) {
	runner := runnerOrDefault(opts.Runner)
	include := append([]string{}, opts.Include...)
	if opts.ToolID != "" && opts.ToolID != "all" {
		include = []string{opts.ToolID}
		opts.Exclude = allCatalogIDsExcept(opts.ToolID)
	}
	selection, err := ResolveSelection(opts.Profile, include, opts.Exclude)
	if err != nil {
		return DoctorResult{}, err
	}
	result := DoctorResult{Profile: selection.Profile}
	for _, tool := range selection.Tools {
		result.Checks = append(result.Checks, doctorOne(tool, runner))
	}
	return result, nil
}

func doctorOne(tool Tool, runner Runner) DoctorCheck {
	if tool.InstallKind == InstallManualOnly {
		return DoctorCheck{ToolID: tool.ID, Status: DoctorManual, Message: tool.ManualNote}
	}
	if tool.Binary == "" {
		return DoctorCheck{ToolID: tool.ID, Status: DoctorFailed, Message: "tool has no binary probe"}
	}
	path, err := runner.LookPath(tool.Binary)
	if err != nil || path == "" {
		return DoctorCheck{ToolID: tool.ID, Status: DoctorMissing, Message: fmt.Sprintf("%s not found on PATH", tool.Binary)}
	}
	args := tool.VersionArgs
	if len(args) == 0 {
		args = []string{"--version"}
	}
	output, err := runner.Run(Command{Name: tool.Binary, Args: args})
	if err != nil {
		return DoctorCheck{ToolID: tool.ID, Status: DoctorFailed, Path: path, Message: err.Error(), Output: output}
	}
	if missing, helpOutput, err := missingRequiredCommands(tool, runner); err != nil {
		return DoctorCheck{ToolID: tool.ID, Status: DoctorFailed, Path: path, Message: fmt.Sprintf("%s --help failed: %v", tool.Binary, err), Output: helpOutput}
	} else if len(missing) > 0 {
		return DoctorCheck{
			ToolID: tool.ID, Status: DoctorFailed, Path: path, Output: output,
			Message: fmt.Sprintf("%s lacks required command %s; upgrade with %s toolchain install %s",
				tool.Binary, strings.Join(missing, ", "), brand.RuntimeValues().BinaryName, tool.ID),
		}
	}
	return DoctorCheck{ToolID: tool.ID, Status: DoctorOK, Path: path, Output: output}
}

// missingRequiredCommands returns the tool's required subcommands that its
// --help output does not list. A command is listed when it is the first word of
// a help line, so a mention in prose does not count.
func missingRequiredCommands(tool Tool, runner Runner) ([]string, CommandOutput, error) {
	if len(tool.RequiredCommands) == 0 {
		return nil, CommandOutput{}, nil
	}
	output, err := runner.Run(Command{Name: tool.Binary, Args: []string{"--help"}})
	if err != nil {
		return nil, output, err
	}
	var listed []string
	for _, line := range strings.Split(output.Stdout+"\n"+output.Stderr, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			listed = append(listed, fields[0])
		}
	}
	var missing []string
	for _, command := range tool.RequiredCommands {
		if !slices.Contains(listed, command) {
			missing = append(missing, command)
		}
	}
	return missing, output, nil
}

func allCatalogIDsExcept(keep string) []string {
	var ids []string
	for _, tool := range Catalog() {
		if tool.ID != keep {
			ids = append(ids, tool.ID)
		}
	}
	return ids
}
