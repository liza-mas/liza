package agent

import (
	"context"
	"os/exec"

	"github.com/liza-mas/liza/internal/providersignal"
)

func cliRunResult(req LLMAgentRunRequest, exitCode int, output string) LLMAgentRunResult {
	return LLMAgentRunResult{ExitCode: exitCode, Output: output, Usage: LLMAgentUsage{}, WarmUsage: req.WarmSession, SessionID: req.SessionID}
}

// failCLIRun reports a run that never reached a provider outcome: its command
// could not be built or the launch gate refused the start.
func failCLIRun(ctx context.Context, req LLMAgentRunRequest, base LLMAgentEvent, err error) (LLMAgentRunResult, error) {
	emitLLMAgentUsage(ctx, req.EventSink, base, LLMAgentUsage{})
	emitLLMAgentFailed(ctx, req.EventSink, base, err.Error())
	return cliRunResult(req, 0, ""), err
}

// completeRun maps the provider's wait outcome to the run result and its
// completed event. The CLI transport reports no usage object, so the empty
// usage is emitted explicitly on every path: the supervisor sink then pins
// unknown provenance instead of inferring zero tokens from a missing event.
func (d *CLIAgent) completeRun(ctx context.Context, req LLMAgentRunRequest, base LLMAgentEvent, streams *cliRunStreams, waitErr error) (LLMAgentRunResult, error) {
	output := streams.stdout.String() + "\n" + streams.stderr.String()
	if d.masker != nil {
		output = d.masker.MaskText(output)
	}
	emitLLMAgentUsage(ctx, req.EventSink, base, LLMAgentUsage{})
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			emitLLMAgentExited(ctx, req.EventSink, base, exitCode)
			return cliRunResult(req, exitCode, output), nil
		}
		emitLLMAgentFailed(ctx, req.EventSink, base, waitErr.Error())
		return cliRunResult(req, 0, output), waitErr
	}
	// A CLI may print its quota failure and still exit 0. Report it as a failed
	// session, as the ACPX backend does, so the supervisor's quota path runs.
	if qe := providersignal.DetectQuotaExhaustion(output, req.BackendName); qe != nil {
		emitLLMAgentQuotaExhausted(ctx, req.EventSink, base, qe.Message)
		return cliRunResult(req, 1, output), nil
	}
	emitLLMAgentExited(ctx, req.EventSink, base, 0)
	return cliRunResult(req, 0, output), nil
}
