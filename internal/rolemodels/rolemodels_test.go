package rolemodels

import (
	"strings"
	"testing"
)

func TestParseSingleEntryAndList(t *testing.T) {
	f, err := Parse([]byte(`defaults:
  doer: {cli: " claude ", model: m1}
  reviewer:
    - {cli: codex, model: m2}
    - {cli: claude}
roles:
  security-reviewer:
    - {cli: codex}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if d := f.Defaults.Doer; d == nil || d.List || d.First() != (Entry{CLI: "claude", Model: "m1"}) {
		t.Errorf("defaults.doer = %+v, want single trimmed entry", d)
	}
	r := f.Defaults.Reviewer
	if r == nil || !r.List || len(r.Items) != 2 || r.Items[1] != (Entry{CLI: "claude"}) {
		t.Errorf("defaults.reviewer = %+v, want two-item list", r)
	}
	if s := f.Roles["security-reviewer"]; !s.List || len(s.Items) != 1 {
		t.Errorf("roles.security-reviewer = %+v, want one-item list", s)
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct{ name, content, want string }{
		{"empty list", "defaults:\n  reviewer: []\n", "an entry list must not be empty"},
		{"unknown entry key", "roles:\n  r:\n    - {cli: codex, effort: high}\n", `unknown entry field "effort"`},
		{"scalar entry", "roles:\n  r: codex\n", "entry must be a {cli, model} mapping or a list of them"},
		{"scalar list item", "roles:\n  r: [codex]\n", "entry must be a mapping"},
		{"second document", "roles: {}\n---\nroles: {}\n", "must hold a single YAML document"},
		{"null role", "roles:\n  coder: null\n", "roles.coder: entry must not be empty"},
		{"empty role value", "roles:\n  coder:\n", "roles.coder: entry must not be empty"},
		{"null default", "defaults:\n  reviewer: ~\n", "defaults.reviewer: entry must not be empty"},
		{"null role through a merge key", "roles:\n  <<: {coder: null}\n", "roles.coder: entry must not be empty"},
		{"null default through a merge key", "defaults:\n  <<: {reviewer: null}\n", "defaults.reviewer: entry must not be empty"},
		{"unknown defaults key", "defaults:\n  planner: {cli: codex}\n", `unknown defaults field "planner"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.content)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestParseEmptySectionsSelectNothing(t *testing.T) {
	for _, content := range []string{"", "# only comments\n", "defaults:\nroles:\n", "roles: {}\n"} {
		f, err := Parse([]byte(content))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", content, err)
		}
		if _, ok := f.For("coder", "doer"); ok {
			t.Fatalf("Parse(%q) covers coder, want nothing", content)
		}
	}
}

func TestSlotReusesLastItem(t *testing.T) {
	s := Selection{Items: []Entry{{CLI: "codex"}, {CLI: "claude"}}, List: true}
	for slot, want := range map[int]string{-1: "codex", 0: "codex", 1: "claude", 2: "claude"} {
		if got := s.Slot(slot).CLI; got != want {
			t.Errorf("Slot(%d) = %s, want %s", slot, got, want)
		}
	}
}

func TestEntryMatchesExactModel(t *testing.T) {
	e := Entry{CLI: "codex", Model: "m"}
	if !e.Matches(" codex ", "m ") {
		t.Error("Matches(codex, m) = false, want true")
	}
	if e.Matches("codex", "") || e.Matches("claude", "m") {
		t.Error("Matches accepted another model or CLI")
	}
	if !(Entry{CLI: "codex"}).Matches("codex", "") || (Entry{CLI: "codex"}).Matches("codex", "m") {
		t.Error("an empty model must match only the tool default")
	}
}

func TestForAndReviewSlots(t *testing.T) {
	list := Selection{Items: []Entry{{CLI: "codex"}}, List: true}
	doer := Selection{Items: []Entry{{CLI: "claude"}}}
	f := File{
		Defaults: Defaults{Doer: &doer, Reviewer: &list},
		Roles:    map[string]Selection{"plain-reviewer": {Items: []Entry{{CLI: "claude"}}}},
	}
	if s, ok := f.For("orchestrator", "orchestrator"); !ok || s.First().CLI != "claude" {
		t.Errorf("orchestrator = %+v, %v; want the doer default", s, ok)
	}
	if _, ok := f.ReviewSlots("code-reviewer"); !ok {
		t.Error("ReviewSlots(code-reviewer) unbound, want the default list")
	}
	if _, ok := f.ReviewSlots("plain-reviewer"); ok {
		t.Error("ReviewSlots(plain-reviewer) bound, want a single entry to bind nothing")
	}
	if _, ok := (File{}).ReviewSlots("code-reviewer"); ok {
		t.Error("ReviewSlots on an empty file bound")
	}
}
