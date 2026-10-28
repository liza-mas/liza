package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestInspectAnomalies(t *testing.T) {
	now := time.Now()

	state := &models.State{
		Anomalies: []models.Anomaly{
			{
				Timestamp: now.Add(-2 * time.Hour),
				Task:      "task-1",
				Reporter:  "coder-1",
				Type:      "retry_loop",
				Details: map[string]any{
					"attempts": 3,
					"reason":   "test failures",
				},
			},
			{
				Timestamp: now.Add(-1 * time.Hour),
				Task:      "task-2",
				Reporter:  "coder-2",
				Type:      "spec_ambiguity",
				Details: map[string]any{
					"section": "authentication",
				},
			},
			{
				Timestamp: now.Add(-30 * time.Minute),
				Task:      "task-1",
				Reporter:  "coder-1",
				Type:      "workaround",
				Details: map[string]any{
					"description": "temporary fix applied",
				},
			},
			{
				Timestamp: now.Add(-15 * time.Minute),
				Task:      "task-3",
				Reporter:  "code-reviewer-1",
				Type:      "debt_created",
				Details: map[string]any{
					"type": "technical debt",
				},
			},
		},
	}

	tests := []struct {
		name       string
		opts       inspectAnomaliesOptions
		wantCount  int
		wantFormat string // "json", "yaml", "table", "value", or "internal"
		wantErr    bool
	}{
		{
			name:       "list all anomalies",
			opts:       inspectAnomaliesOptions{},
			wantCount:  4,
			wantFormat: "table",
		},
		{
			name: "filter by type retry_loop",
			opts: inspectAnomaliesOptions{
				TypeFilter: "retry_loop",
			},
			wantCount:  1,
			wantFormat: "table",
		},
		{
			name: "filter by type spec_ambiguity",
			opts: inspectAnomaliesOptions{
				TypeFilter: "spec_ambiguity",
			},
			wantCount:  1,
			wantFormat: "table",
		},
		{
			name: "filter by task",
			opts: inspectAnomaliesOptions{
				TaskFilter: "task-1",
			},
			wantCount:  2,
			wantFormat: "table",
		},
		{
			name: "filter by reporter",
			opts: inspectAnomaliesOptions{
				ReporterFilter: "coder-1",
			},
			wantCount:  2,
			wantFormat: "table",
		},
		{
			name: "multiple filters - task and type",
			opts: inspectAnomaliesOptions{
				TaskFilter: "task-1",
				TypeFilter: "retry_loop",
			},
			wantCount:  1,
			wantFormat: "table",
		},
		{
			name: "filter returns no results",
			opts: inspectAnomaliesOptions{
				TypeFilter: "external_blocker",
			},
			wantCount:  0,
			wantFormat: "table",
		},
		{
			name: "JSON format",
			opts: inspectAnomaliesOptions{
				Format: "json",
			},
			wantCount:  4,
			wantFormat: "json",
		},
		{
			name: "YAML format",
			opts: inspectAnomaliesOptions{
				Format: "yaml",
			},
			wantCount:  4,
			wantFormat: "yaml",
		},
		{
			name: "internal flag returns structured data",
			opts: inspectAnomaliesOptions{
				Internal: true,
			},
			wantCount:  4,
			wantFormat: "internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := inspectAnomalies(state, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Validate result based on format
			switch tt.wantFormat {
			case "internal":
				// Should return []anomalyInfo
				anomalies, ok := result.([]anomalyInfo)
				if !ok {
					t.Errorf("expected []anomalyInfo, got %T", result)
					return
				}
				if len(anomalies) != tt.wantCount {
					t.Errorf("expected %d anomalies, got %d", tt.wantCount, len(anomalies))
				}
				// Check that computed fields are present
				for _, anomaly := range anomalies {
					if anomaly.Age == "" {
						t.Errorf("expected Age to be computed")
					}
				}
			case "json":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Validate JSON
				var anomalies []anomalyInfo
				if err := json.Unmarshal([]byte(output), &anomalies); err != nil {
					t.Errorf("invalid JSON output: %v", err)
				}
				if len(anomalies) != tt.wantCount {
					t.Errorf("expected %d anomalies in JSON, got %d", tt.wantCount, len(anomalies))
				}
			case "yaml":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Just check it's not empty for non-zero results
				if tt.wantCount > 0 && output == "" {
					t.Errorf("expected non-empty YAML output")
				}
			case "table":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				if tt.wantCount == 0 {
					if !strings.Contains(output, "No anomalies found") {
						t.Errorf("expected 'No anomalies found' message")
					}
				} else {
					// Check that output contains expected types
					if tt.opts.TypeFilter == "retry_loop" {
						if !strings.Contains(output, "retry_loop") {
							t.Errorf("expected output to contain retry_loop")
						}
					}
					if tt.opts.Format == "" && tt.name == "list all anomalies" {
						if !strings.Contains(output, "DETAILS") {
							t.Errorf("expected default table output to contain DETAILS column, got:\n%s", output)
						}
						if !strings.Contains(output, "reason=test failures") {
							t.Errorf("expected default table output to surface anomaly details, got:\n%s", output)
						}
					}
				}
			}
		})
	}
}

func TestBuildAnomalyInfo(t *testing.T) {
	now := time.Now()
	timestamp := now.Add(-2 * time.Hour)

	anomaly := models.Anomaly{
		Timestamp: timestamp,
		Task:      "task-1",
		Reporter:  "coder-1",
		Type:      "retry_loop",
		Details: map[string]any{
			"attempts": 3,
			"reason":   "test failures",
		},
	}

	info := buildAnomalyInfo(anomaly)

	// Verify all fields are copied correctly
	if info.Task != "task-1" {
		t.Errorf("expected Task=task-1, got %s", info.Task)
	}
	if info.Reporter != "coder-1" {
		t.Errorf("expected Reporter=coder-1, got %s", info.Reporter)
	}
	if info.Type != "retry_loop" {
		t.Errorf("expected Type=retry_loop, got %s", info.Type)
	}
	if info.Timestamp != timestamp {
		t.Errorf("expected Timestamp=%v, got %v", timestamp, info.Timestamp)
	}

	// Check computed Age field
	if info.Age == "" {
		t.Errorf("expected Age to be computed")
	}
	if !strings.Contains(info.Age, "2h") {
		t.Errorf("expected Age to contain '2h', got %s", info.Age)
	}

	// Check Details are preserved
	if info.Details == nil {
		t.Errorf("expected Details to be preserved")
	}
	if attempts, ok := info.Details["attempts"].(int); !ok || attempts != 3 {
		t.Errorf("expected Details[attempts]=3, got %v", info.Details["attempts"])
	}
}

func TestFormatAnomaliesTable_MasksSecretsInDetails(t *testing.T) {
	secret := "table-secret-submit-verdict-token"
	t.Setenv("LIZA_TABLE_TOKEN", secret)

	output := formatAnomaliesTable([]anomalyInfo{
		{
			Age:      "1m",
			Task:     "task-1",
			Reporter: "code-reviewer-1",
			Type:     "submit_verdict_failed",
			Details: map[string]any{
				"error": "failed with " + secret,
			},
			Timestamp: time.Now().UTC(),
		},
	})

	if strings.Contains(output, secret) {
		t.Fatalf("table output leaked secret: %s", output)
	}
	if !strings.Contains(output, "error=failed with ***") {
		t.Fatalf("table output = %s, want redacted details summary", output)
	}
}

func TestFilterAnomalies(t *testing.T) {
	now := time.Now()

	anomalies := []models.Anomaly{
		{
			Timestamp: now.Add(-2 * time.Hour),
			Task:      "task-1",
			Reporter:  "coder-1",
			Type:      "retry_loop",
			Details:   map[string]any{},
		},
		{
			Timestamp: now.Add(-1 * time.Hour),
			Task:      "task-2",
			Reporter:  "coder-2",
			Type:      "spec_ambiguity",
			Details:   map[string]any{},
		},
		{
			Timestamp: now.Add(-30 * time.Minute),
			Task:      "task-1",
			Reporter:  "coder-1",
			Type:      "workaround",
			Details:   map[string]any{},
		},
	}

	tests := []struct {
		name      string
		opts      inspectAnomaliesOptions
		wantCount int
		wantTypes []string
	}{
		{
			name:      "no filters - return all",
			opts:      inspectAnomaliesOptions{},
			wantCount: 3,
			wantTypes: []string{"retry_loop", "spec_ambiguity", "workaround"},
		},
		{
			name: "filter by task",
			opts: inspectAnomaliesOptions{
				TaskFilter: "task-1",
			},
			wantCount: 2,
			wantTypes: []string{"retry_loop", "workaround"},
		},
		{
			name: "filter by type",
			opts: inspectAnomaliesOptions{
				TypeFilter: "retry_loop",
			},
			wantCount: 1,
			wantTypes: []string{"retry_loop"},
		},
		{
			name: "filter by reporter",
			opts: inspectAnomaliesOptions{
				ReporterFilter: "coder-2",
			},
			wantCount: 1,
			wantTypes: []string{"spec_ambiguity"},
		},
		{
			name: "multiple filters",
			opts: inspectAnomaliesOptions{
				TaskFilter: "task-1",
				TypeFilter: "retry_loop",
			},
			wantCount: 1,
			wantTypes: []string{"retry_loop"},
		},
		{
			name: "no matches",
			opts: inspectAnomaliesOptions{
				TaskFilter: "nonexistent",
			},
			wantCount: 0,
			wantTypes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterAnomalies(anomalies, tt.opts)

			if len(filtered) != tt.wantCount {
				t.Errorf("expected %d anomalies, got %d", tt.wantCount, len(filtered))
			}

			// Check that filtered results have expected types
			gotTypes := make([]string, len(filtered))
			for i, a := range filtered {
				gotTypes[i] = string(a.Type)
			}

			for _, wantType := range tt.wantTypes {
				found := false
				for _, gotType := range gotTypes {
					if gotType == wantType {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected to find type %s in filtered results", wantType)
				}
			}
		})
	}
}

func TestAnomalyInfo_EmptyState(t *testing.T) {
	state := &models.State{
		Anomalies: []models.Anomaly{},
	}

	opts := inspectAnomaliesOptions{}
	result, err := inspectAnomalies(state, opts)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	output, ok := result.(string)
	if !ok {
		t.Errorf("expected string output, got %T", result)
		return
	}

	if !strings.Contains(output, "No anomalies found") {
		t.Errorf("expected 'No anomalies found' message, got: %s", output)
	}
}
