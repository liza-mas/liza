package tui

import (
	"fmt"
	"sort"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
)

// stubWatcher implements StateWatcher for testing.
type stubWatcher struct {
	events chan struct{}
	errors chan error
}

func newStubWatcher() *stubWatcher {
	return &stubWatcher{
		events: make(chan struct{}, 1),
		errors: make(chan error, 1),
	}
}

func (w *stubWatcher) Events() <-chan struct{} { return w.events }
func (w *stubWatcher) Errors() <-chan error    { return w.errors }
func (w *stubWatcher) Close() error            { return nil }

func TestUpdateStateChangedMsgReturnsCmd(t *testing.T) {
	w := newStubWatcher()
	m := Model{
		watcher:    w,
		blackboard: nil, // readStateCmd will be called but won't execute in test
		stateCache: make(map[string]time.Time),
	}

	result, cmd := m.Update(stateChangedMsg{})
	if cmd == nil {
		t.Fatal("stateChangedMsg handler must return a non-nil tea.Cmd")
	}
	_ = result
}

func TestUpdateStateMsgSetsReadyAndState(t *testing.T) {
	m := Model{
		stateCache: make(map[string]time.Time),
	}
	state := &models.State{
		Goal: models.Goal{Description: "test goal"},
	}

	result, _ := m.Update(StateMsg{State: state})
	updated := result.(Model)
	if !updated.ready {
		t.Error("StateMsg must set m.ready to true")
	}
	if updated.state != state {
		t.Error("StateMsg must set m.state to the provided state")
	}
}

func TestUpdateStateMsgProcessesNewAnomalies(t *testing.T) {
	ts := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)

	// Model has already processed 1 anomaly
	m := Model{
		lastAnomalyCount: 1,
		activities:       make([]ActivityEntry, 0),
		stateCache:       make(map[string]time.Time),
	}

	state := &models.State{
		Anomalies: []models.Anomaly{
			{Timestamp: ts, Reporter: "coder-1", Type: "retry_loop", Task: "task-1", Details: map[string]any{"count": 3}},
			{Timestamp: ts.Add(time.Minute), Reporter: "coder-2", Type: "trade_off", Task: "task-2", Details: map[string]any{"what": "perf"}},
			{Timestamp: ts.Add(2 * time.Minute), Reporter: "coder-3", Type: "spec_gap", Task: "task-3", Details: map[string]any{"note": "unclear"}},
		},
	}

	result, _ := m.Update(StateMsg{State: state})
	updated := result.(Model)

	// 3 anomalies total, 1 already processed → 2 new entries
	if len(updated.activities) != 2 {
		t.Fatalf("expected 2 new ActivityEntry items, got %d", len(updated.activities))
	}
	for _, entry := range updated.activities {
		if entry.Source != "anomaly" {
			t.Errorf("expected Source 'anomaly', got %q", entry.Source)
		}
	}
	if updated.lastAnomalyCount != 3 {
		t.Errorf("expected lastAnomalyCount 3, got %d", updated.lastAnomalyCount)
	}
}

func TestUpdateTickMsgReturnsCmd(t *testing.T) {
	m := Model{
		stateCache: make(map[string]time.Time),
	}

	_, cmd := m.Update(TickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("TickMsg handler must return a non-nil tea.Cmd")
	}
}

func TestUpdateAlertsMsgCriticalSetsBanner(t *testing.T) {
	m := Model{
		activities: make([]ActivityEntry, 0),
		stateCache: make(map[string]time.Time),
	}

	now := time.Now()
	alerts := alertsMsg{
		Alerts: []AlertMsg{
			{Timestamp: now, Level: "⚠️", Category: "BLOCKED", Message: "task blocked"},
			{Timestamp: now, Level: "🚨", Category: "CIRCUIT_BREAKER", Message: "escalated to WARNING"},
		},
		StateCache: map[string]time.Time{"key": now},
	}

	result, _ := m.Update(alerts)
	updated := result.(Model)

	if updated.alertBanner == nil {
		t.Fatal("alertsMsg with critical alert must set m.alertBanner")
	}
	if updated.alertBanner.Action != "CIRCUIT_BREAKER" {
		t.Errorf("expected banner Action CIRCUIT_BREAKER, got %q", updated.alertBanner.Action)
	}

	// Alert expiry should be ~10s in the future
	expectedExpiry := time.Now().Add(10 * time.Second)
	diff := updated.alertExpiry.Sub(expectedExpiry)
	if diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("alertExpiry should be ~10s in the future, got diff %v", diff)
	}
}

func TestUpdateAlertsMsgReplacesStateCache(t *testing.T) {
	originalCache := map[string]time.Time{"old": time.Now()}
	m := Model{
		activities: make([]ActivityEntry, 0),
		stateCache: originalCache,
	}

	newCache := map[string]time.Time{"new": time.Now()}
	alerts := alertsMsg{
		Alerts:     []AlertMsg{},
		StateCache: newCache,
	}

	result, _ := m.Update(alerts)
	updated := result.(Model)

	if _, exists := updated.stateCache["old"]; exists {
		t.Error("alertsMsg must replace stateCache, not merge")
	}
	if _, exists := updated.stateCache["new"]; !exists {
		t.Error("alertsMsg must set stateCache to the returned cache")
	}
}

func TestUpdateLogEntriesMsgUpdatesPositionAndAppends(t *testing.T) {
	m := Model{
		activities:  make([]ActivityEntry, 0),
		logPosition: 100,
		stateCache:  make(map[string]time.Time),
	}

	ts := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	task := "task-1"
	msg := LogEntriesMsg{
		Entries: []log.Entry{
			{Timestamp: ts, Agent: "coder-1", Action: "started", Task: &task, Detail: "working"},
			{Timestamp: ts.Add(time.Minute), Agent: "coder-2", Action: "claimed", Detail: "claimed it"},
			{Timestamp: ts.Add(2 * time.Minute), Agent: "coder-3", Action: "done", Detail: "finished"},
		},
		NewPosition: 500,
	}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.logPosition != 500 {
		t.Errorf("expected logPosition 500, got %d", updated.logPosition)
	}
	if len(updated.activities) != 3 {
		t.Fatalf("expected 3 activity entries, got %d", len(updated.activities))
	}
}

func TestUpdateWindowSizeMsgSetsColumnTier(t *testing.T) {
	m := Model{
		stateCache: make(map[string]time.Time),
	}

	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	updated := result.(Model)

	if updated.width != 120 {
		t.Errorf("expected width 120, got %d", updated.width)
	}
	if updated.height != 40 {
		t.Errorf("expected height 40, got %d", updated.height)
	}
	if updated.columnTier != ColumnTierWide {
		t.Errorf("expected ColumnTierWide for width 120, got %d", updated.columnTier)
	}
}

func TestUpdateErrMsgNoOp(t *testing.T) {
	m := Model{
		stateCache: make(map[string]time.Time),
	}

	result, cmd := m.Update(errMsg{err: fmt.Errorf("test error")})
	if cmd != nil {
		t.Error("errMsg handler should return nil cmd")
	}
	_ = result.(Model) // should not panic
}

func TestUpdateWatcherClosedMsg(t *testing.T) {
	w := newStubWatcher()
	m := Model{
		watcher:    w,
		stateCache: make(map[string]time.Time),
	}

	result, cmd := m.Update(watcherClosedMsg{})
	updated := result.(Model)
	if cmd != nil {
		t.Error("watcherClosedMsg handler should return nil cmd")
	}
	if updated.watcher != nil {
		t.Error("watcherClosedMsg must set watcher to nil")
	}
}

func TestAppendActivityCapsAt200(t *testing.T) {
	ts := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)

	// Test: 199 + 1 = 200 (no drop)
	activities := make([]ActivityEntry, 199)
	for i := range activities {
		activities[i] = ActivityEntry{Timestamp: ts, Source: "log", Action: fmt.Sprintf("action-%d", i)}
	}
	newEntry := ActivityEntry{Timestamp: ts, Source: "log", Action: "action-199"}
	result := appendActivity(activities, newEntry)
	if len(result) != 200 {
		t.Errorf("expected 200 entries with 199+1, got %d", len(result))
	}

	// Test: 200 + 1 = 200 (oldest dropped)
	activities = make([]ActivityEntry, 200)
	for i := range activities {
		activities[i] = ActivityEntry{Timestamp: ts, Source: "log", Action: fmt.Sprintf("action-%d", i)}
	}
	newEntry = ActivityEntry{Timestamp: ts, Source: "log", Action: "newest"}
	result = appendActivity(activities, newEntry)
	if len(result) != 200 {
		t.Errorf("expected 200 entries with 200+1, got %d", len(result))
	}
	// Verify oldest dropped and newest present
	if result[len(result)-1].Action != "newest" {
		t.Errorf("expected last entry to be 'newest', got %q", result[len(result)-1].Action)
	}
	if result[0].Action != "action-1" {
		t.Errorf("expected oldest surviving entry to be 'action-1', got %q", result[0].Action)
	}
}

func TestFormatAnomalyDetails(t *testing.T) {
	// Nil map
	if got := formatAnomalyDetails(nil); got != "" {
		t.Errorf("expected empty string for nil map, got %q", got)
	}

	// Empty map
	if got := formatAnomalyDetails(map[string]any{}); got != "" {
		t.Errorf("expected empty string for empty map, got %q", got)
	}

	// Sorted keys
	details := map[string]any{"z_key": "last", "a_key": "first", "m_key": 42}
	got := formatAnomalyDetails(details)
	// Keys should be sorted alphabetically
	expected := "a_key=first m_key=42 z_key=last"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}

	// Verify sort order independently
	keys := []string{"z_key", "a_key", "m_key"}
	sort.Strings(keys)
	if keys[0] != "a_key" || keys[1] != "m_key" || keys[2] != "z_key" {
		t.Errorf("sort sanity check failed: %v", keys)
	}
}
