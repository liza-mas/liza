// Package rolemodels reads models.yaml, the operator's per-role CLI and model
// selection (ADR-0167). It is a leaf package so that both the agent runtime
// and the review-claim operations can read the same selection.
package rolemodels

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/liza-mas/liza/internal/paths"
)

// Entry selects the CLI, and optionally the model, an agent launches with.
type Entry struct {
	CLI   string `yaml:"cli"`
	Model string `yaml:"model,omitempty"`
}

// Matches reports whether an agent registered with provider and model runs
// this entry. An empty model means the tool default and matches only itself.
func (e Entry) Matches(provider, model string) bool {
	return e.CLI == strings.TrimSpace(provider) && e.Model == strings.TrimSpace(model)
}

// Selection is one role's models.yaml value: a single entry, or a list whose
// items bind review slots in priority order.
type Selection struct {
	Items []Entry
	// List is set when the value was written as a YAML list; only a list
	// binds claims, even with one item.
	List bool
}

// UnmarshalYAML accepts a {cli, model} mapping or a non-empty list of them.
func (s *Selection) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.MappingNode:
		entry, err := decodeEntry(node)
		if err != nil {
			return err
		}
		*s = Selection{Items: []Entry{entry}}
		return nil
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return fmt.Errorf("line %d: an entry list must not be empty", node.Line)
		}
		items := make([]Entry, 0, len(node.Content))
		for _, item := range node.Content {
			entry, err := decodeEntry(item)
			if err != nil {
				return err
			}
			items = append(items, entry)
		}
		*s = Selection{Items: items, List: true}
		return nil
	default:
		return fmt.Errorf("line %d: entry must be a {cli, model} mapping or a list of them", node.Line)
	}
}

func decodeEntry(node *yaml.Node) (Entry, error) {
	if node.Kind != yaml.MappingNode {
		return Entry{}, fmt.Errorf("line %d: entry must be a mapping with cli and optional model", node.Line)
	}
	for i := 0; i < len(node.Content); i += 2 {
		if key := node.Content[i].Value; key != "cli" && key != "model" {
			return Entry{}, fmt.Errorf("line %d: unknown entry field %q (allowed: cli, model)", node.Content[i].Line, key)
		}
	}
	var e Entry
	if err := node.Decode(&e); err != nil {
		return Entry{}, err
	}
	return Entry{CLI: strings.TrimSpace(e.CLI), Model: strings.TrimSpace(e.Model)}, nil
}

// First returns the entry an agent started without a slot launches with.
func (s Selection) First() Entry {
	return s.Items[0]
}

// Slot returns the entry for review slot n (0-based, the task's approval
// count); slots past the end reuse the last item.
func (s Selection) Slot(n int) Entry {
	return s.Items[min(max(n, 0), len(s.Items)-1)]
}

// Defaults holds the per-role-type selections.
type Defaults struct {
	Doer     *Selection `yaml:"doer,omitempty"`
	Reviewer *Selection `yaml:"reviewer,omitempty"`
}

// UnmarshalYAML accepts only the doer and reviewer keys and refuses a null
// one, which would otherwise decode as an absent default.
func (d *Defaults) UnmarshalYAML(node *yaml.Node) error {
	var values map[string]*Selection
	if err := node.Decode(&values); err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		s := values[key]
		switch key {
		case "doer":
			d.Doer = s
		case "reviewer":
			d.Reviewer = s
		default:
			return fmt.Errorf("line %d: unknown defaults field %q (allowed: doer, reviewer)", node.Line, key)
		}
		if s == nil {
			return fmt.Errorf("defaults.%s: entry must not be empty", key)
		}
	}
	return nil
}

// File is the decoded models.yaml.
type File struct {
	Defaults Defaults             `yaml:"defaults,omitempty"`
	Roles    map[string]Selection `yaml:"roles,omitempty"`
}

// Load reads models.yaml. A missing file, or one holding only comments,
// selects nothing.
func Load(projectRoot string) (File, error) {
	path := paths.New(projectRoot).ModelsPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("read %s: %w", path, err)
	}
	f, err := Parse(data)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Parse decodes models.yaml content strictly, as a single document.
func Parse(data []byte) (File, error) {
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return File{}, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return File{}, fmt.Errorf("must hold a single YAML document")
	}
	// YAML decodes a null value, written directly or through a merge key,
	// without calling Selection.UnmarshalYAML: it would read as a covering
	// selection with no entry.
	for _, role := range slices.Sorted(maps.Keys(f.Roles)) {
		if len(f.Roles[role].Items) == 0 {
			return File{}, fmt.Errorf("roles.%s: entry must not be empty", role)
		}
	}
	return f, nil
}

// For returns the selection covering a role: its own, else the default for
// its type. Orchestrators use the doer default, as for CLI defaults.
func (f File) For(role, roleType string) (Selection, bool) {
	if s, ok := f.Roles[role]; ok {
		return s, true
	}
	var s *Selection
	switch roleType {
	case "doer", "orchestrator":
		s = f.Defaults.Doer
	case "reviewer":
		s = f.Defaults.Reviewer
	}
	if s == nil {
		return Selection{}, false
	}
	return *s, true
}

// ReviewSlots returns the list binding a reviewer role's review slots, or
// false when the role's selection is absent or a single entry.
func (f File) ReviewSlots(role string) (Selection, bool) {
	s, ok := f.For(role, "reviewer")
	if !ok || !s.List {
		return Selection{}, false
	}
	return s, true
}
