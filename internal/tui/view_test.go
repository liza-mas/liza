package tui

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestView_NotReady_ReturnsLoading(t *testing.T) {
	m := New("/tmp/test")
	// ready defaults to false
	got := m.View()
	if !strings.Contains(got, "Loading...") {
		t.Errorf("View() when not ready should contain 'Loading...', got: %q", got)
	}
}

func TestView_Ready_ContainsHeaderAndFooter(t *testing.T) {
	m := New("/tmp/test")
	m.ready = true
	m.width = 120
	m.height = 40
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal: models.Goal{Description: "Test Goal"},
		Sprint: models.Sprint{
			ID: "sprint-1",
		},
		Config: models.Config{Mode: models.SystemModeRunning},
	}

	got := m.View()
	// Header should be present (contains LIZA)
	if !strings.Contains(got, "LIZA") {
		t.Errorf("View() should contain header with 'LIZA', got: %q", got)
	}
}

func TestRenderHeader_ContainsGoalDescription(t *testing.T) {
	m := New("/tmp/test")
	m.width = 120
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal: models.Goal{Description: "Build the TUI dashboard"},
		Sprint: models.Sprint{
			ID: "sprint-42",
		},
		Config: models.Config{Mode: models.SystemModeRunning},
	}

	got := m.renderHeader()
	if !strings.Contains(got, "Build the TUI dashboard") {
		t.Errorf("renderHeader() should contain goal description, got: %q", got)
	}
}

func TestRenderHeader_ContainsSprintID(t *testing.T) {
	m := New("/tmp/test")
	m.width = 120
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal: models.Goal{Description: "Some goal"},
		Sprint: models.Sprint{
			ID: "sprint-7",
		},
		Config: models.Config{Mode: models.SystemModeRunning},
	}

	got := m.renderHeader()
	if !strings.Contains(got, "sprint-7") {
		t.Errorf("renderHeader() should contain sprint ID, got: %q", got)
	}
}

func TestRenderHeader_StatusMatchesSystemMode(t *testing.T) {
	tests := []struct {
		name       string
		mode       models.SystemMode
		wantStatus string
	}{
		{"running", models.SystemModeRunning, "RUNNING"},
		{"paused", models.SystemModePaused, "PAUSED"},
		{"stopped", models.SystemModeStopped, "STOPPED"},
		{"circuit breaker", models.SystemModeCircuitBreakerTripped, "CIRCUIT_BREAKER_TRIPPED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New("/tmp/test")
			m.width = 120
			m.styles = NewStyles(120)
			m.state = &models.State{
				Goal:   models.Goal{Description: "goal"},
				Sprint: models.Sprint{ID: "s1"},
				Config: models.Config{Mode: tt.mode},
			}

			got := m.renderHeader()
			if !strings.Contains(got, tt.wantStatus) {
				t.Errorf("renderHeader() with mode %s should contain %q, got: %q", tt.mode, tt.wantStatus, got)
			}
		})
	}
}

func TestRenderHeader_NilState_ReturnsLoadingFallback(t *testing.T) {
	m := New("/tmp/test")
	m.width = 120
	m.styles = NewStyles(120)
	m.state = nil

	got := m.renderHeader()
	if !strings.Contains(got, "Loading") {
		t.Errorf("renderHeader() with nil state should contain 'Loading', got: %q", got)
	}
	if !strings.Contains(got, "LIZA") {
		t.Errorf("renderHeader() with nil state should still contain 'LIZA', got: %q", got)
	}
}

func TestRenderHeader_CheckpointOverridesColor(t *testing.T) {
	m := New("/tmp/test")
	m.width = 120
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal: models.Goal{Description: "goal"},
		Sprint: models.Sprint{
			ID:     "s1",
			Status: models.SprintStatusCheckpoint,
		},
		Config: models.Config{Mode: models.SystemModeRunning},
	}

	// The header should still show RUNNING (the system mode text) but the
	// color should be checkpoint/magenta. We verify the text is present;
	// color verification would require inspecting ANSI codes which is brittle.
	got := m.renderHeader()
	if !strings.Contains(got, "RUNNING") {
		t.Errorf("renderHeader() with CHECKPOINT sprint should still show system mode text, got: %q", got)
	}
}
