package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providersignal"
)

const unknownJSONCommand = "unknown-json-envelope"

type observedRuntimeFailure struct {
	Command string
	Code    string
}

func (f observedRuntimeFailure) key() string {
	return f.Command + "\x00" + f.Code
}

type runtimeFailureState struct {
	Count int
	Key   string
}

type runtimeFailureTracker struct {
	byTask map[string]runtimeFailureState
}

func newRuntimeFailureTracker() *runtimeFailureTracker {
	return &runtimeFailureTracker{byTask: make(map[string]runtimeFailureState)}
}

func (t *runtimeFailureTracker) Track(taskID string, failure observedRuntimeFailure) int {
	prev := t.byTask[taskID]
	key := failure.key()
	if prev.Key != "" && prev.Key != key {
		prev.Count = 0
	}
	prev.Count++
	prev.Key = key
	t.byTask[taskID] = prev
	return prev.Count
}

func (t *runtimeFailureTracker) reset(taskID string) {
	delete(t.byTask, taskID)
}

func detectObservedRuntimeFailure(output string) *observedRuntimeFailure {
	if strings.Contains(output, submitVerdictFailedCode) {
		return &observedRuntimeFailure{Command: submitVerdictCommand, Code: submitVerdictFailedCode}
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, streamJSONObjectPrefix) {
			continue
		}
		if failure := detectFailedCommandEvent(line); failure != nil {
			return failure
		}
		env, ok := parseFailedJSONEnvelope(line)
		if !ok {
			continue
		}
		return &observedRuntimeFailure{
			Command: detectLizaCommandContext(output),
			Code:    env.Error.Code,
		}
	}
	return nil
}

type runtimeFailureEnvelope struct {
	OK    *bool `json:"ok"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func parseFailedJSONEnvelope(line string) (runtimeFailureEnvelope, bool) {
	var env runtimeFailureEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return runtimeFailureEnvelope{}, false
	}
	if env.OK == nil || *env.OK || env.Error == nil || env.Error.Code == "" {
		return runtimeFailureEnvelope{}, false
	}
	return env, true
}

type commandEventEnvelope struct {
	Item *struct {
		Command          string `json:"command"`
		AggregatedOutput string `json:"aggregated_output"`
		ExitCode         *int   `json:"exit_code"`
	} `json:"item"`
}

func detectFailedCommandEvent(line string) *observedRuntimeFailure {
	var event commandEventEnvelope
	if err := json.Unmarshal([]byte(line), &event); err != nil || event.Item == nil {
		return nil
	}

	command := detectLizaCommandContext(event.Item.Command)
	output := event.Item.AggregatedOutput
	if strings.Contains(output, submitVerdictFailedCode) {
		return &observedRuntimeFailure{Command: submitVerdictCommand, Code: submitVerdictFailedCode}
	}
	for _, outputLine := range strings.Split(output, "\n") {
		outputLine = strings.TrimSpace(outputLine)
		if !strings.HasPrefix(outputLine, streamJSONObjectPrefix) {
			continue
		}
		env, ok := parseFailedJSONEnvelope(outputLine)
		if !ok {
			continue
		}
		return &observedRuntimeFailure{Command: command, Code: env.Error.Code}
	}
	if event.Item.ExitCode != nil && *event.Item.ExitCode != 0 && command != unknownJSONCommand {
		return &observedRuntimeFailure{Command: command, Code: fmt.Sprintf("exit_%d", *event.Item.ExitCode)}
	}
	return nil
}

func detectLizaCommandContext(output string) string {
	for _, command := range []string{
		"submit-verdict",
		"submit-review",
		"await-verdict",
		"await-resubmission",
		"mark-blocked",
		"retarget-dependency",
		"set-task-output",
		"handoff",
	} {
		if strings.Contains(output, command) {
			return command
		}
	}
	return unknownJSONCommand
}

func handleObservedRuntimeFailureRetry(
	bb *db.Blackboard,
	config SupervisorConfig,
	taskID string,
	runtimeConfig models.Config,
	failure observedRuntimeFailure,
	runtimeTracker *runtimeFailureTracker,
	spinTracker *spinningTracker,
) bool {
	if taskID == "" {
		return false
	}

	spinTracker.reset(taskID)

	count := runtimeTracker.Track(taskID, failure)
	threshold := effectiveSpinningRestartThreshold(runtimeConfig)
	if count <= threshold {
		GetLogger().Warn("Observed structured CLI/runtime failure; suppressing no-progress spin count",
			"task_id", taskID,
			"agent_id", config.AgentID,
			"command", failure.Command,
			"code", failure.Code,
			"count", count,
			"threshold", threshold)
		return false
	}

	reason := fmt.Sprintf("tool failure retry loop detected: %d consecutive observed CLI/runtime failures for task %s without progress (threshold=%d, command=%s, code=%s)",
		count, taskID, threshold, failure.Command, failure.Code)
	GetLogger().Error("Tool failure retry loop detected, blocking task",
		"task_id", taskID,
		"agent_id", config.AgentID,
		"command", failure.Command,
		"code", failure.Code,
		"count", count)
	if alertErr := providersignal.LogAlert(config.ProjectRoot, alertLevelCritical, alertCategoryToolFailureRetryLoop, reason); alertErr != nil {
		GetLogger().Warn("Failed to write tool failure retry alert", "error", alertErr)
	}
	if err := blockTaskFromSupervisor(bb, config.ProjectRoot, taskID, config.Authority, reason); err != nil {
		GetLogger().Warn("Failed to block task from supervisor", "error", err, "task_id", taskID)
	}
	runtimeTracker.reset(taskID)
	spinTracker.reset(taskID)
	return true
}

// providerEndedSession reports whether the session output shows the provider
// itself ended the session: a startup failure or an exhausted quota.
func providerEndedSession(output, cliName string) bool {
	return providersignal.DetectProviderUnavailable(output, cliName) != nil || providersignal.DetectQuotaExhaustion(output, cliName) != nil
}
