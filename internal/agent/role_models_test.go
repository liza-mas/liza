package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/rolemodels"
)

var testRoleTypes = map[string]string{"coder": "doer", "code-reviewer": "reviewer", "orchestrator": "orchestrator"}

func single(cli, model string) rolemodels.Selection {
	return rolemodels.Selection{Items: []rolemodels.Entry{{CLI: cli, Model: model}}}
}

func writeModelsFile(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	path := paths.New(root).ModelsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadRoleModelsMissingOrCommentOnlySelectsNothing(t *testing.T) {
	for name, root := range map[string]string{
		"missing":      t.TempDir(),
		"comment-only": writeModelsFile(t, "# defaults:\n#   doer: {cli: claude}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			rm, err := LoadValidatedRoleModels(root, testRoleTypes, models.Config{})
			if err != nil {
				t.Fatalf("LoadValidatedRoleModels() error = %v", err)
			}
			if _, covered := rm.For("coder", "doer"); covered {
				t.Fatalf("coder covered by %+v, want nothing", rm)
			}
		})
	}
}

func TestLoadRoleModelsResolvesRoleThenTypeDefault(t *testing.T) {
	root := writeModelsFile(t, `defaults:
  doer: {cli: claude, model: doer-model}
  reviewer: {cli: codex}
roles:
  code-reviewer: {cli: claude, model: review-model}
`)
	rm, err := LoadValidatedRoleModels(root, testRoleTypes, models.Config{})
	if err != nil {
		t.Fatalf("LoadValidatedRoleModels() error = %v", err)
	}
	tests := []struct {
		role, roleType string
		want           rolemodels.Entry
	}{
		{"coder", "doer", rolemodels.Entry{CLI: "claude", Model: "doer-model"}},
		{"orchestrator", "orchestrator", rolemodels.Entry{CLI: "claude", Model: "doer-model"}},
		{"code-reviewer", "reviewer", rolemodels.Entry{CLI: "claude", Model: "review-model"}},
		{"architecture-reviewer", "reviewer", rolemodels.Entry{CLI: "codex"}},
	}
	for _, tt := range tests {
		got, covered := rm.For(tt.role, tt.roleType)
		if !covered || got.List || got.First() != tt.want {
			t.Errorf("For(%s) = %+v, %v; want single %+v", tt.role, got, covered, tt.want)
		}
	}
}

func TestLoadRoleModelsRejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"list on doer role", "roles:\n  coder:\n    - {cli: codex}\n", "roles.coder: only reviewer roles take an entry list"},
		{"list on orchestrator role", "roles:\n  orchestrator:\n    - {cli: codex}\n", "roles.orchestrator: only reviewer roles take an entry list"},
		{"list as doer default", "defaults:\n  doer:\n    - {cli: codex}\n", "defaults.doer: only reviewer roles take an entry list"},
		{"bad list item", "roles:\n  code-reviewer:\n    - {cli: codex}\n    - {cli: nope}\n", `roles.code-reviewer[1]: unknown CLI "nope"`},
		{"empty list", "defaults:\n  reviewer: []\n", "an entry list must not be empty"},
		{"unknown top-level key", "default:\n  doer: {cli: claude}\n", "field default not found"},
		{"unknown entry key", "roles:\n  coder: {cli: claude, effort: high}\n", `unknown entry field "effort"`},
		{"missing cli", "roles:\n  coder: {model: m}\n", "roles.coder: cli is required"},
		{"null role", "roles:\n  coder: null\n", "roles.coder: entry must not be empty"},
		{"null role through a merge key", "roles:\n  <<: {coder: null}\n", "roles.coder: entry must not be empty"},
		{"unknown role", "roles:\n  typo-coder: {cli: claude}\n", "roles.typo-coder: unknown role"},
		{"unknown cli", "defaults:\n  doer: {cli: nope}\n", `defaults.doer: unknown CLI "nope"`},
		{"model on unsupported cli", "defaults:\n  reviewer: {cli: opencode, model: m}\n", "defaults.reviewer: opencode does not support model selection"},
		{"second document", "roles:\n  coder: {cli: claude}\n---\nroles:\n  coder: {cli: claude, effort: high}\n", "must hold a single YAML document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeModelsFile(t, tt.content)
			_, err := LoadValidatedRoleModels(root, testRoleTypes, models.Config{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestResolveLaunchSelectionRules(t *testing.T) {
	config := models.Config{
		DefaultDoerCLI:     "codex",
		DefaultDoerProfile: "careful",
		AgentProfiles: map[string]models.AgentProfileConfig{
			"careful": {CLI: "opencode", Vars: map[string]string{"mode": "careful"}},
			"fast":    {CLI: "claude"},
		},
	}
	codexDefault := single("codex", "d")
	covering := rolemodels.File{Roles: map[string]rolemodels.Selection{"coder": single("claude", "file-model")}}
	tests := []struct {
		name        string
		req         LaunchSelectionRequest
		want        LaunchSelection
		wantProfile string
		wantErr     string
	}{
		{name: "explicit profile beats file", req: LaunchSelectionRequest{Profile: "fast", RoleModels: covering},
			want: LaunchSelection{CLI: "claude", Source: SelectionSourceProfile}, wantProfile: "fast"},
		{name: "explicit profile refuses model", req: LaunchSelectionRequest{Profile: "fast", Model: "m"}, wantErr: "--model cannot be combined with --profile"},
		{name: "model flag on file cli", req: LaunchSelectionRequest{Model: "flag-model", RoleModels: covering},
			want: LaunchSelection{CLI: "claude", Model: "flag-model", Source: SelectionSourceFlag}},
		{name: "model flag on role default cli", req: LaunchSelectionRequest{Model: "flag-model"},
			want: LaunchSelection{CLI: "codex", Model: "flag-model", Source: SelectionSourceFlag}},
		{name: "cli and model flags", req: LaunchSelectionRequest{CLI: "codex", CLIChanged: true, Model: "flag-model", RoleModels: covering},
			want: LaunchSelection{CLI: "codex", Model: "flag-model", Source: SelectionSourceFlag}},
		{name: "cli flag ignores file, keeps default profile", req: LaunchSelectionRequest{CLI: "codex", CLIChanged: true, RoleModels: covering},
			want: LaunchSelection{CLI: "codex", Source: SelectionSourceFlag}, wantProfile: "careful"},
		{name: "role entry beats default profile", req: LaunchSelectionRequest{RoleModels: covering},
			want: LaunchSelection{CLI: "claude", Model: "file-model", Source: SelectionSourceModelsFile}},
		{name: "type default entry", req: LaunchSelectionRequest{RoleModels: rolemodels.File{Defaults: rolemodels.Defaults{Doer: &codexDefault}}},
			want: LaunchSelection{CLI: "codex", Model: "d", Source: SelectionSourceModelsFile}},
		{name: "uncovered role keeps default profile", req: LaunchSelectionRequest{},
			want: LaunchSelection{CLI: "opencode", Source: SelectionSourceDefault}, wantProfile: "careful"},
		{name: "model on unsupported cli", req: LaunchSelectionRequest{CLI: "opencode", CLIChanged: true, Model: "m"}, wantErr: "opencode does not support model selection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.req
			req.Role, req.RoleType, req.Config = "coder", "doer", config
			got, err := ResolveLaunchSelection(req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveLaunchSelection() error = %v", err)
			}
			if got.CLI != tt.want.CLI || got.Model != tt.want.Model || got.Source != tt.want.Source || got.Profile.Name != tt.wantProfile {
				t.Fatalf("selection = %+v, want %+v with profile %q", got, tt.want, tt.wantProfile)
			}
		})
	}
}

func TestResolveLaunchSelectionModelsItem(t *testing.T) {
	list := rolemodels.Selection{Items: []rolemodels.Entry{{CLI: "claude", Model: "m1"}, {CLI: "codex", Model: "m2"}}, List: true}
	bound := rolemodels.File{Roles: map[string]rolemodels.Selection{"code-reviewer": list}}
	unbound := rolemodels.File{Roles: map[string]rolemodels.Selection{"code-reviewer": single("codex", "")}}
	tests := []struct {
		name    string
		req     LaunchSelectionRequest
		want    LaunchSelection
		wantErr string
	}{
		{name: "item 2", req: LaunchSelectionRequest{Item: 2, RoleModels: bound},
			want: LaunchSelection{CLI: "codex", Model: "m2", Source: SelectionSourceModelsFile}},
		{name: "no item starts item 1", req: LaunchSelectionRequest{RoleModels: bound},
			want: LaunchSelection{CLI: "claude", Model: "m1", Source: SelectionSourceModelsFile}},
		{name: "out of range", req: LaunchSelectionRequest{Item: 3, RoleModels: bound}, wantErr: "--models-item 3 is out of range: role code-reviewer lists 2 entries"},
		{name: "single entry", req: LaunchSelectionRequest{Item: 1, RoleModels: unbound}, wantErr: "--models-item needs a models.yaml entry list for role code-reviewer"},
		{name: "uncovered", req: LaunchSelectionRequest{Item: 1}, wantErr: "--models-item needs a models.yaml entry list"},
		{name: "with cli", req: LaunchSelectionRequest{Item: 1, CLI: "codex", CLIChanged: true, RoleModels: bound}, wantErr: "--models-item cannot be combined"},
		{name: "with model", req: LaunchSelectionRequest{Item: 1, Model: "m", RoleModels: bound}, wantErr: "--models-item cannot be combined"},
		{name: "with profile", req: LaunchSelectionRequest{Item: 1, Profile: "p", RoleModels: bound}, wantErr: "--models-item cannot be combined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.req
			req.Role, req.RoleType = "code-reviewer", "reviewer"
			got, err := ResolveLaunchSelection(req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveLaunchSelection() error = %v", err)
			}
			if got.CLI != tt.want.CLI || got.Model != tt.want.Model || got.Source != tt.want.Source {
				t.Fatalf("selection = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveLaunchPlanPrependsModelArgs(t *testing.T) {
	tests := []struct {
		name        string
		outputsDir  string
		interactive bool
		wantArgs    []string
	}{
		{name: "claude", wantArgs: []string{"--model", "m1", "-p", "--permission-mode", "auto"}},
		{name: "claude", outputsDir: "/logs", wantArgs: []string{"--model", "m1", "-p", "--permission-mode", "auto", "--verbose", "--output-format", "stream-json"}},
		{name: "claude", interactive: true, wantArgs: []string{"--model", "m1"}},
		{name: "codex", wantArgs: []string{"-m", "m1", "exec", "-"}},
		{name: "codex", outputsDir: "/logs", wantArgs: []string{"-m", "m1", "exec", "--json", "-"}},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.outputsDir, func(t *testing.T) {
			plan, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: tt.name, Model: "m1", OutputsDir: tt.outputsDir, Interactive: tt.interactive})
			if err != nil {
				t.Fatalf("ResolveLaunchPlan() error = %v", err)
			}
			if !slices.Equal(plan.Args, tt.wantArgs) || plan.Model != "m1" {
				t.Fatalf("args = %v model %q, want %v m1", plan.Args, plan.Model, tt.wantArgs)
			}
		})
	}
}

func TestResolveLaunchPlanRejectsModelWithoutModelArgs(t *testing.T) {
	for _, tool := range []string{"opencode", "codex-acp"} {
		_, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: tool, Prompt: "p", Model: "m"})
		if err == nil || !strings.Contains(err.Error(), "does not support model selection") {
			t.Fatalf("%s: error = %v, want unsupported model selection", tool, err)
		}
	}
}

// A profile whose tool args carry its own model must launch with exactly that
// model, even when models.yaml covers the role with a different one, and the
// selection must not claim the file's model.
func TestExplicitProfileModelDoesNotCollideWithFileModel(t *testing.T) {
	config := models.Config{
		AgentProfiles: map[string]models.AgentProfileConfig{"pinned": {CLI: "claude", Vars: map[string]string{"model": "profile-model"}}},
		AgentTools:    map[string]models.AgentToolConfig{"claude": {RunArgs: []string{"-p", "--model", "{{profile.model}}"}}},
	}
	sel, err := ResolveLaunchSelection(LaunchSelectionRequest{
		Role: "coder", RoleType: "doer", Profile: "pinned", Config: config,
		RoleModels: rolemodels.File{Roles: map[string]rolemodels.Selection{"coder": single("claude", "file-model")}},
	})
	if err != nil {
		t.Fatalf("ResolveLaunchSelection() error = %v", err)
	}
	if sel.Model != "" {
		t.Fatalf("selection model = %q, want none recorded under a profile", sel.Model)
	}
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: sel.CLI, ProfileName: sel.Profile.Name, ProfileVars: sel.Profile.Vars, Model: sel.Model, RuntimeConfig: config})
	if err != nil {
		t.Fatalf("ResolveLaunchPlan() error = %v", err)
	}
	if want := []string{"-p", "--model", "profile-model"}; !slices.Equal(plan.Args, want) {
		t.Fatalf("args = %v, want exactly one model flag %v", plan.Args, want)
	}
}

// An ACPX tool passes no argv model, so a model_args override must not make it
// accept one: the model would be recorded on registration without being used.
func TestModelSelectionRefusedForACPXEvenWithModelArgs(t *testing.T) {
	config := models.Config{AgentTools: map[string]models.AgentToolConfig{
		"codex-acp": {ModelArgs: []string{"-m", "{{model}}"}},
	}}
	if SupportsModelSelection(AgentToolRegistry(config)["codex-acp"]) {
		t.Fatal("SupportsModelSelection(codex-acp with model_args) = true, want false")
	}
	const want = "codex-acp does not support model selection"
	if err := ValidateRoleModels(rolemodels.File{Roles: map[string]rolemodels.Selection{"coder": single("codex-acp", "m")}}, testRoleTypes, config); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Validate() error = %v, want %q", err, want)
	}
	if _, err := ResolveLaunchSelection(LaunchSelectionRequest{Role: "coder", RoleType: "doer", CLI: "codex-acp", CLIChanged: true, Model: "m", Config: config}); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ResolveLaunchSelection() error = %v, want %q", err, want)
	}
	if _, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: "codex-acp", Prompt: "p", Model: "m", RuntimeConfig: config}); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ResolveLaunchPlan() error = %v, want %q", err, want)
	}
}
