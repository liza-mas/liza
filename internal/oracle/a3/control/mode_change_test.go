package control

import "testing"

func TestOracleModeChange(t *testing.T) {
	runCases(t, "mode-change", []controlCase{
		{name: "pause-from-running", fixture: "base", args: []string{"pause"}, state: true},
		{name: "pause-from-paused", fixture: "paused", args: []string{"pause"}, state: true},
		{name: "resume-from-paused", fixture: "paused", args: []string{"resume"}, state: true},
		{name: "stop-from-running", fixture: "base", args: []string{"stop"}, state: true},
		{name: "start-from-stopped", fixture: "stopped", args: []string{"start"}, state: true},
		{name: "start-refused-running", fixture: "base", args: []string{"start"}, code: 1, state: true},
		{name: "resume-refused-running", fixture: "base", args: []string{"resume"}, code: 1, state: true},
	})
}
