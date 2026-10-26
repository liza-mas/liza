package agent

import (
	"context"

	"github.com/liza-mas/liza/internal/providersignal"
)

// acpxTurn is the identity of one ACPX turn that its events and result share.
type acpxTurn struct {
	req       LLMAgentRunRequest
	base      LLMAgentEvent
	acpxAgent string
	warm      bool
}

func (t acpxTurn) result(exitCode int, output string, usage LLMAgentUsage) LLMAgentRunResult {
	return LLMAgentRunResult{ExitCode: exitCode, Output: output, Usage: usage, WarmUsage: t.warm, SessionID: t.base.SessionID}
}

// completeRun maps the prompt's outcome to the run result and its completed
// event: an error (with only a classifiable provider-unavailable diagnostic as
// output), a quota message printed by a successful turn, or success.
func (a *ACPXAgent) completeRun(ctx context.Context, turn acpxTurn, output acpxOutput, usage LLMAgentUsage, failureEvidence string, runErr error) (LLMAgentRunResult, error) {
	sink := turn.req.EventSink
	if runErr != nil {
		errText := a.maskText(runErr.Error())
		resultOutput := ""
		if _, diagnostic := providersignal.ClassifyProviderUnavailable(a.maskText(failureEvidence), turn.req.BackendName); diagnostic != "" {
			resultOutput = diagnostic
		}
		emitLLMAgentFailed(ctx, sink, turn.base, errText)
		return turn.result(1, resultOutput, usage), maskedError{message: errText, err: runErr}
	}
	if qe := providersignal.DetectQuotaExhaustion(output.Text, turn.acpxAgent); qe != nil {
		emitLLMAgentQuotaExhausted(ctx, sink, turn.base, qe.Message)
		return turn.result(1, output.Text, usage), nil
	}
	emitLLMAgentExited(ctx, sink, turn.base, 0)
	return turn.result(0, output.Text, usage), nil
}

func (a *ACPXAgent) boundedProviderFailure(err error, backendName string) (string, maskedError) {
	errText := a.maskText(err.Error())
	if _, diagnostic := providersignal.ClassifyProviderUnavailable(errText, backendName); diagnostic != "" {
		errText = diagnostic
	}
	return errText, maskedError{message: errText, err: err}
}
