package jsonout

import (
	"encoding/json"
	"errors"
	"io"
)

// Envelope is the standard JSON response wrapper for --json mode.
type Envelope struct {
	OK       bool         `json:"ok"`
	Result   any          `json:"result"`
	Warnings []string     `json:"warnings,omitempty"`
	Error    *ErrorDetail `json:"error,omitempty"`
}

// ErrorDetail contains structured error information.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrAlreadyWritten is a sentinel error indicating that a JSON error envelope
// has already been written to stdout. main() uses this to exit 1 without
// printing a duplicate error to stderr.
var ErrAlreadyWritten = errors.New("json output already written")

// WriteResult writes a JSON envelope to w. On error, writes an error envelope
// and returns ErrAlreadyWritten. On success, writes a success envelope and
// returns nil.
func WriteResult(w io.Writer, result any, warnings []string, err error) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	if err != nil {
		code, msg := ClassifyError(err)
		env := Envelope{
			OK: false,
			Error: &ErrorDetail{
				Code:    code,
				Message: msg,
			},
		}
		_ = enc.Encode(env)
		return ErrAlreadyWritten
	}

	env := Envelope{
		OK:       true,
		Result:   result,
		Warnings: warnings,
	}
	_ = enc.Encode(env)
	return nil
}
