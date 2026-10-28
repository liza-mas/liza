package agent

// streamEventKind is the kind of a provider stream event, as a line of session
// output or its subtype field spells it. The spellings are the providers' own;
// they decide which parsing branch a line takes. Text and JSON boundaries
// convert explicitly, so an unknown spelling never matches a kind.
type streamEventKind string

const (
	// streamTaskNotification is the subtype of a background-job notification.
	streamTaskNotification streamEventKind = "task_notification"
	// streamToolResult marks a line that carries a tool result.
	streamToolResult streamEventKind = "tool_result"
)

// acpSessionUpdateKind is the value of an ACP session update's discriminator.
// Updates are decoded as map[string]any, so comparisons convert the constant to
// string: a boxed defined type would never equal the decoded string.
type acpSessionUpdateKind string

// acpSessionUpdateMessageChunk selects the agent message chunks of an ACP
// session update.
const acpSessionUpdateMessageChunk acpSessionUpdateKind = "agent_message_chunk"

// Provider stream spellings that are not event kinds: the JSON line prefix, the
// ACP discriminator key, and the verdict command's failure report.
const (
	// streamJSONObjectPrefix marks a line of provider output as a JSON event.
	streamJSONObjectPrefix = "{"
	// acpSessionUpdateKey is the key of an ACP session update's discriminator.
	acpSessionUpdateKey = "sessionUpdate"
	// submitVerdictFailedCode and submitVerdictCommand name the failure of the
	// verdict submission command, as its JSON output reports it.
	submitVerdictFailedCode = "submit_verdict_failed"
	submitVerdictCommand    = "submit-verdict"
)
