package prompts

import (
	"path"
	"strings"
	"testing"
)

// Every wake_*.tmpl must be reachable from WakeTriggers, and every trigger
// must render: the role budget measures the orchestrator per trigger, so a
// template added without a trigger entry would be unmeasured.
func TestWakeTriggersCoverEveryWakeTemplate(t *testing.T) {
	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	templates := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "wake_") && strings.HasSuffix(name, ".tmpl") {
			templates[strings.TrimSuffix(name, path.Ext(name))] = false
		}
	}
	for _, trigger := range WakeTriggers {
		rendered, err := RenderWakeInstructions(trigger, "orchestrator-1")
		if err != nil {
			t.Fatalf("%s: %v", trigger, err)
		}
		if strings.TrimSpace(rendered) == "" {
			t.Errorf("%s rendered nothing", trigger)
		}
		name := "wake_" + strings.ToLower(trigger)
		if _, ok := templates[name]; !ok {
			t.Errorf("trigger %s has no template %s.tmpl", trigger, name)
		}
		templates[name] = true
	}
	for name, covered := range templates {
		if !covered {
			t.Errorf("template %s.tmpl is not reachable from WakeTriggers", name)
		}
	}
}
