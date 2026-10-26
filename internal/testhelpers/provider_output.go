package testhelpers

// Provider output fixtures shared by the tests of internal/providersignal, which
// classifies them, and internal/agent, which acts on the classification.

// ClaudeRateLimitedResult429 is the result event of #184: a Claude limit whose
// wording matches no quota pattern.
const ClaudeRateLimitedResult429 = `{"type":"result","subtype":"success","is_error":true,"api_error_status":429,"terminal_reason":"api_error","result":"API Error: Server is temporarily limiting requests (not your usage limit) · No account can serve this request for claude-opus-5-5: all N accounts are at their quota or rate limit. Quota resets in 3h 43m."}`

const ClaudeOAuthExpiredDiagnostic = "Failed to authenticate: OAuth session expired and could not be refreshed"

// ClaudeOAuthExpiredOutput is the tail of a Claude stream-json session whose
// login expired, trimmed from an observed run: the CLI fails before any turn.
const ClaudeOAuthExpiredOutput = `{"type":"assistant","message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Failed to authenticate: OAuth session expired and could not be refreshed"}]},"error":"authentication_failed","is_api_error_message":true}
{"type":"result","subtype":"success","is_error":true,"terminal_reason":"api_error","duration_ms":247,"num_turns":1,"result":"Failed to authenticate: OAuth session expired and could not be refreshed"}`

// CodexSessionAccessDiagnostic is the bounded diagnostic persisted for a Codex session-access failure.
const CodexSessionAccessDiagnostic = "thread/start failed: error creating thread: Codex cannot access session files under .codex/sessions (permission denied)"
