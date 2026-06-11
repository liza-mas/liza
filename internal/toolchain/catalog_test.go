package toolchain

import (
	"slices"
	"testing"
)

func TestResolveSelectionBalancedDefaults(t *testing.T) {
	selection, err := ResolveSelection(ProfileBalanced, nil, nil)
	if err != nil {
		t.Fatalf("ResolveSelection() error = %v", err)
	}

	ids := selectionIDs(selection.Tools)
	for _, want := range []string{"rtk", "stacklit", "scip-search", "semble", "rg", "ast-grep", "mdtoc", "mdq", "jq", "yq", "gh", "pre-commit"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("balanced profile missing %q in %v", want, ids)
		}
	}
	for _, unwanted := range []string{"functional-clusters", "claude-usage", "postgres-mcp"} {
		if slices.Contains(ids, unwanted) {
			t.Fatalf("balanced profile unexpectedly selected %q in %v", unwanted, ids)
		}
	}
}

func TestResolveSelectionLeanExcludesModelDownloadTools(t *testing.T) {
	selection, err := ResolveSelection(ProfileLean, nil, nil)
	if err != nil {
		t.Fatalf("ResolveSelection() error = %v", err)
	}

	ids := selectionIDs(selection.Tools)
	if slices.Contains(ids, "semble") {
		t.Fatalf("lean profile selected Semble: %v", ids)
	}
	if !slices.Contains(ids, "stacklit") || !slices.Contains(ids, "scip-search") {
		t.Fatalf("lean profile should keep core index tools: %v", ids)
	}
}

func TestResolveSelectionFullKeepsMCPUnchecked(t *testing.T) {
	selection, err := ResolveSelection(ProfileFull, nil, nil)
	if err != nil {
		t.Fatalf("ResolveSelection() error = %v", err)
	}

	ids := selectionIDs(selection.Tools)
	if !slices.Contains(ids, "functional-clusters") {
		t.Fatalf("full profile missing functional-clusters: %v", ids)
	}
	if slices.Contains(ids, "postgres-mcp") {
		t.Fatalf("full profile should not install MCP capabilities by default: %v", ids)
	}
}

func TestResolveSelectionIncludeExclude(t *testing.T) {
	selection, err := ResolveSelection(ProfileLean, []string{"semble", "postgres-mcp"}, []string{"stacklit", "postgres-mcp"})
	if err != nil {
		t.Fatalf("ResolveSelection() error = %v", err)
	}

	ids := selectionIDs(selection.Tools)
	if !slices.Contains(ids, "semble") {
		t.Fatalf("include did not select Semble: %v", ids)
	}
	if slices.Contains(ids, "stacklit") {
		t.Fatalf("exclude did not remove Stacklit: %v", ids)
	}
	if slices.Contains(ids, "postgres-mcp") {
		t.Fatalf("exclude should win over include for postgres-mcp: %v", ids)
	}
}

func TestResolveSelectionUnknownTool(t *testing.T) {
	if _, err := ResolveSelection(ProfileBalanced, []string{"nope"}, nil); err == nil {
		t.Fatal("ResolveSelection() error = nil, want unknown tool error")
	}
}

func selectionIDs(tools []Tool) []string {
	ids := make([]string, len(tools))
	for i, tool := range tools {
		ids[i] = tool.ID
	}
	return ids
}
