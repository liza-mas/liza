package agent

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// RoleModelEntry selects the CLI, and optionally the model, an agent role
// launches with.
type RoleModelEntry struct {
	CLI   string `yaml:"cli"`
	Model string `yaml:"model,omitempty"`
}

// UnmarshalYAML accepts only a mapping with cli and model keys, so a reviewer
// list (ADR-0167 step 2) is refused with a clear message instead of a type
// error.
func (e *RoleModelEntry) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.SequenceNode {
		return fmt.Errorf("line %d: reviewer lists are not supported yet (ADR-0167 step 2); use a single {cli, model} entry", node.Line)
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: entry must be a mapping with cli and optional model", node.Line)
	}
	for i := 0; i < len(node.Content); i += 2 {
		if key := node.Content[i].Value; key != "cli" && key != "model" {
			return fmt.Errorf("line %d: unknown entry field %q (allowed: cli, model)", node.Content[i].Line, key)
		}
	}
	type plain RoleModelEntry
	return node.Decode((*plain)(e))
}

// RoleModelDefaults holds the per-role-type entries of models.yaml.
type RoleModelDefaults struct {
	Doer     *RoleModelEntry `yaml:"doer,omitempty"`
	Reviewer *RoleModelEntry `yaml:"reviewer,omitempty"`
}

// RoleModels is the operator's per-role launch selection, read from the
// project runtime directory at every agent start.
type RoleModels struct {
	Defaults RoleModelDefaults         `yaml:"defaults,omitempty"`
	Roles    map[string]RoleModelEntry `yaml:"roles,omitempty"`
}

// LoadRoleModels reads models.yaml. A missing file, or one holding only
// comments, selects nothing.
func LoadRoleModels(projectRoot string) (RoleModels, error) {
	path := paths.New(projectRoot).ModelsPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return RoleModels{}, nil
	}
	if err != nil {
		return RoleModels{}, fmt.Errorf("read %s: %w", path, err)
	}
	rm, err := ParseRoleModels(data)
	if err != nil {
		return RoleModels{}, fmt.Errorf("%s: %w", path, err)
	}
	return rm, nil
}

// ParseRoleModels decodes models.yaml content strictly.
func ParseRoleModels(data []byte) (RoleModels, error) {
	var rm RoleModels
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&rm); err != nil && !errors.Is(err, io.EOF) {
		return RoleModels{}, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return RoleModels{}, fmt.Errorf("must hold a single YAML document")
	}
	return rm, nil
}

// LoadValidatedRoleModels loads models.yaml and validates it against the
// pipeline's roles and the configured tools.
func LoadValidatedRoleModels(projectRoot string, roleNames []string, config models.Config) (RoleModels, error) {
	rm, err := LoadRoleModels(projectRoot)
	if err != nil {
		return RoleModels{}, err
	}
	if err := rm.Validate(roleNames, config); err != nil {
		return RoleModels{}, fmt.Errorf("%s: %w", paths.New(projectRoot).ModelsPath(), err)
	}
	return rm, nil
}

// Validate refuses entries naming an unknown role or CLI, and models set for
// a CLI that cannot take one.
func (rm RoleModels) Validate(roleNames []string, config models.Config) error {
	check := func(where string, e RoleModelEntry) error {
		cli := strings.TrimSpace(e.CLI)
		if cli == "" {
			return fmt.Errorf("%s: cli is required", where)
		}
		if !IsValidCLI(cli, config) {
			return fmt.Errorf("%s: unknown CLI %q (must be %s)", where, cli, strings.Join(AvailableCLIs(config), ", "))
		}
		if strings.TrimSpace(e.Model) != "" && !SupportsModelSelection(AgentToolRegistry(config)[cli]) {
			return fmt.Errorf("%s: %s does not support model selection", where, cli)
		}
		return nil
	}
	if e := rm.Defaults.Doer; e != nil {
		if err := check("defaults.doer", *e); err != nil {
			return err
		}
	}
	if e := rm.Defaults.Reviewer; e != nil {
		if err := check("defaults.reviewer", *e); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(rm.Roles))
	for name := range rm.Roles {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !slices.Contains(roleNames, name) {
			return fmt.Errorf("roles.%s: unknown role (valid: %s)", name, strings.Join(roleNames, ", "))
		}
		if err := check("roles."+name, rm.Roles[name]); err != nil {
			return err
		}
	}
	return nil
}

// EntryFor returns the entry covering a role: its own entry, else the default
// for its type. Orchestrators use the doer default, as for CLI defaults.
func (rm RoleModels) EntryFor(role, roleType string) (RoleModelEntry, bool) {
	if e, ok := rm.Roles[role]; ok {
		return trimEntry(e), true
	}
	var e *RoleModelEntry
	switch roleType {
	case "doer", "orchestrator":
		e = rm.Defaults.Doer
	case "reviewer":
		e = rm.Defaults.Reviewer
	}
	if e == nil {
		return RoleModelEntry{}, false
	}
	return trimEntry(*e), true
}

func trimEntry(e RoleModelEntry) RoleModelEntry {
	return RoleModelEntry{CLI: strings.TrimSpace(e.CLI), Model: strings.TrimSpace(e.Model)}
}
