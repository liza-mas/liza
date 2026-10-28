package toolresultacp

// Method is an ACP method name the proxy handles itself. The set names the
// handled methods; it is not an allowlist. Any other method, including unknown
// "terminal/" ones, is routed exactly as before.
type Method string

const (
	MethodInitialize      Method = "initialize"
	MethodSessionNew      Method = "session/new"
	MethodSessionPrompt   Method = "session/prompt"
	MethodSessionCancel   Method = "session/cancel"
	MethodFSReadTextFile  Method = "fs/read_text_file"
	MethodTerminalCreate  Method = "terminal/create"
	MethodTerminalOutput  Method = "terminal/output"
	MethodTerminalWait    Method = "terminal/wait_for_exit"
	MethodTerminalKill    Method = "terminal/kill"
	MethodTerminalRelease Method = "terminal/release"
)

// terminalMethodPrefix routes every agent request under it to terminalCall,
// known or not.
const terminalMethodPrefix = "terminal/"
