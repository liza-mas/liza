package agent

// Provider stream vocabulary the supervisor reads from session output. These are
// the providers' and the CLI's own spellings, not ours; they decide what a line
// of output means, so they are named once here.
const (
	// streamJSONObjectPrefix marks a line of provider output as a JSON event.
	streamJSONObjectPrefix = "{"
	// streamTaskNotification is the subtype of a background-job notification.
	streamTaskNotification = "task_notification"
	// streamToolResult marks a line that carries a tool result.
	streamToolResult = "tool_result"
	// acpSessionUpdateKey and acpSessionUpdateMessageChunk select the agent
	// message chunks of an ACP session update.
	acpSessionUpdateKey          = "sessionUpdate"
	acpSessionUpdateMessageChunk = "agent_message_chunk"
	// submitVerdictFailedCode and submitVerdictCommand name the failure of the
	// verdict submission command, as its JSON output reports it.
	submitVerdictFailedCode = "submit_verdict_failed"
	submitVerdictCommand    = "submit-verdict"
)

// humanNoteForAll is the target of an operator note addressed to every task. The
// operations that write and read notes keep their own, unexported, copy.
const humanNoteForAll = "all"
