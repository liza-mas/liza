package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/mcp/protocol"
	"github.com/liza-mas/liza/internal/ops"
)

// ToolHandler is a function that handles tool invocation
type ToolHandler func(params map[string]any) (any, error)

type runTransport interface {
	ReadRequest() (*protocol.JSONRPCRequest, error)
	WriteResponse(resp *protocol.JSONRPCResponse) error
	WriteError(id json.RawMessage, code int, message string, data any) error
}

// HandleRequest handles a JSON-RPC request
func (s *Server) HandleRequest(req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolCall(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourceRead(req)
	default:
		return rpcError(req, protocol.NewMethodNotFoundError(req.Method))
	}
}

// rpcResult builds a successful JSON-RPC response.
func rpcResult(req *protocol.JSONRPCRequest, result any) *protocol.JSONRPCResponse {
	return &protocol.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// rpcError builds an error JSON-RPC response.
func rpcError(req *protocol.JSONRPCRequest, jerr *protocol.JSONRPCError) *protocol.JSONRPCResponse {
	return &protocol.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: jerr}
}

// handleInitialize handles the initialize request
func (s *Server) handleInitialize(req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	return rpcResult(req, map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    s.GetCapabilities(),
		"serverInfo": map[string]any{
			"name":    "liza-mcp",
			"version": Version,
		},
	})
}

// handleToolsList handles the tools/list request
func (s *Server) handleToolsList(req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	return rpcResult(req, map[string]any{"tools": s.ListTools()})
}

// NotInitializedError indicates the .liza directory does not exist.
type NotInitializedError struct{ ProjectRoot string }

func (e *NotInitializedError) Error() string {
	return fmt.Sprintf("workspace not initialized (no .liza directory in %s) — run 'liza init' first", e.ProjectRoot)
}

// checkInitialized returns an error if the .liza directory doesn't exist.
func (s *Server) checkInitialized() error {
	lizaDir := filepath.Join(s.projectRoot, ".liza")
	if _, err := os.Stat(lizaDir); os.IsNotExist(err) {
		return &NotInitializedError{ProjectRoot: s.projectRoot}
	}
	return nil
}

// handleToolCall handles the tools/call request
func (s *Server) handleToolCall(req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	params, ok := req.Params.(map[string]any)
	if !ok {
		return rpcError(req, protocol.NewInvalidParamsError("params must be an object"))
	}

	toolName, ok := params["name"].(string)
	if !ok {
		return rpcError(req, protocol.NewInvalidParamsError("name must be a string"))
	}

	// liza_version works without initialization
	if toolName != "liza_version" {
		if err := s.checkInitialized(); err != nil {
			return rpcError(req, s.classifyError(err))
		}
	}

	handler, ok := s.handlers[toolName]
	if !ok {
		return rpcError(req, protocol.NewError(protocol.NotFound, "Tool not found: "+toolName, nil))
	}

	args, _ := params["arguments"].(map[string]any)
	if args == nil {
		args = make(map[string]any)
	}

	result, err := handler(args)
	if err != nil {
		return rpcError(req, s.classifyError(err))
	}

	return rpcResult(req, result)
}

// handleResourcesList handles the resources/list request
func (s *Server) handleResourcesList(req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	return rpcResult(req, map[string]any{"resources": s.ListResources()})
}

// handleResourceRead handles the resources/read request
func (s *Server) handleResourceRead(req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	if err := s.checkInitialized(); err != nil {
		return rpcError(req, s.classifyError(err))
	}

	params, ok := req.Params.(map[string]any)
	if !ok {
		return rpcError(req, protocol.NewInvalidParamsError("params must be an object"))
	}

	uri, ok := params["uri"].(string)
	if !ok {
		return rpcError(req, protocol.NewInvalidParamsError("uri must be a string"))
	}

	result, err := s.handleResourceReadInternal(uri)
	if err != nil {
		return rpcError(req, s.classifyError(err))
	}

	return rpcResult(req, result)
}

// stringErrorRules maps error message substrings to MCP error responses.
// Used as a fallback when typed error checks (errors.As) don't match.
var stringErrorRules = []struct {
	patterns []string
	code     int
	message  string
}{
	{
		patterns: []string{"not found", "does not exist"},
		code:     protocol.NotFound,
		message:  "resource not found",
	},
	{
		patterns: []string{"race condition", "changed concurrently"},
		code:     protocol.RaceCondition,
		message:  "state changed concurrently, retry",
	},
	{
		patterns: []string{
			"not IMPLEMENTING", "not REVIEWING", "not READY_FOR_REVIEW",
			"not CODE_READY_FOR_REVIEW", "not CODE_APPROVED",
			"not APPROVED", "must be", "is required", "invalid task ID",
			"validation failed", "must include", "mandatory",
		},
		code:    protocol.ValidationError,
		message: "validation failed: precondition not met",
	},
}

// matchStringErrorRule checks error message text against known patterns.
// Returns nil if no pattern matches.
func matchStringErrorRule(msg string) *protocol.JSONRPCError {
	for _, rule := range stringErrorRules {
		for _, p := range rule.patterns {
			if strings.Contains(msg, p) {
				return protocol.NewError(rule.code, rule.message, nil)
			}
		}
	}
	return nil
}

// classifyError converts Go errors to MCP error codes.
// Maps internal error patterns to JSON-RPC error codes for intelligent client handling.
// Typed errors may expose controlled fields (e.g. Reason) — raw err.Error() is never exposed directly.
func (s *Server) classifyError(err error) *protocol.JSONRPCError {
	// Type-based checks first (preferred)
	var nie *NotInitializedError
	if errors.As(err, &nie) {
		return protocol.NewError(protocol.InvalidRequest, nie.Error(), nil)
	}
	var nfe *lizaerrors.NotFoundError
	if errors.As(err, &nfe) {
		return protocol.NewError(protocol.NotFound, "resource not found", nil)
	}
	var postWriteValidationErr *ops.PostWriteValidationError
	if errors.As(err, &postWriteValidationErr) {
		return protocol.NewError(protocol.ValidationError, "validation failed: precondition not met", nil)
	}
	var preconditionErr *ops.PreconditionError
	if errors.As(err, &preconditionErr) {
		return protocol.NewError(protocol.ValidationError, preconditionErr.Reason, nil)
	}
	var roleErr *RoleError
	if errors.As(err, &roleErr) {
		return protocol.NewError(protocol.ValidationError, roleErr.Error(), nil)
	}
	var inputShapeErr *InputShapeError
	if errors.As(err, &inputShapeErr) {
		return protocol.NewError(protocol.ValidationError, inputShapeErr.Message, nil)
	}
	var intErr *ops.IntegrationFailedError
	if errors.As(err, &intErr) {
		return protocol.NewError(protocol.ValidationError,
			fmt.Sprintf("integration failed: %s — task moved to INTEGRATION_FAILED", intErr.Reason), nil)
	}
	var opErr *ops.OperationalError
	if errors.As(err, &opErr) {
		return protocol.NewError(protocol.InternalError, opErr.Message, nil)
	}

	msg := err.Error()

	// Lock timeout: compound match (requires "lock" AND a timeout indicator)
	if strings.Contains(msg, "lock") && (strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out")) {
		return protocol.NewError(protocol.LockTimeout, "lock acquisition timed out", nil)
	}

	// String fallback for errors from external packages (git, etc.)
	if jerr := matchStringErrorRule(msg); jerr != nil {
		return jerr
	}

	// Default: internal error without leaking implementation details
	return protocol.NewError(protocol.InternalError, "internal error", nil)
}

// handleNotification processes JSON-RPC notifications (no response sent).
// Per JSON-RPC 2.0 spec, notifications MUST NOT receive a response.
// Unknown notifications are silently ignored.
func (s *Server) handleNotification(req *protocol.JSONRPCRequest) {
	switch req.Method {
	case "notifications/initialized", "notifications/cancelled":
		// Expected lifecycle notifications, no action needed
	}
}
