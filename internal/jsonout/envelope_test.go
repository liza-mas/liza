package jsonout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestWriteResult_Success(t *testing.T) {
	var buf bytes.Buffer
	result := map[string]string{"task_id": "T1", "status": "claimed"}
	err := WriteResult(&buf, result, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !env.OK {
		t.Error("ok = false, want true")
	}
	if env.Error != nil {
		t.Errorf("error should be nil, got %+v", env.Error)
	}
	if env.Result == nil {
		t.Error("result should not be nil")
	}
}

func TestWriteResult_SuccessWithWarnings(t *testing.T) {
	var buf bytes.Buffer
	result := map[string]int{"count": 5}
	warnings := []string{"rate suspiciously high"}
	err := WriteResult(&buf, result, warnings, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !env.OK {
		t.Error("ok = false, want true")
	}
	if len(env.Warnings) != 1 {
		t.Fatalf("warnings length = %d, want 1", len(env.Warnings))
	}
	if env.Warnings[0] != "rate suspiciously high" {
		t.Errorf("warnings[0] = %q, want %q", env.Warnings[0], "rate suspiciously high")
	}
}

func TestWriteResult_Error(t *testing.T) {
	var buf bytes.Buffer
	inputErr := fmt.Errorf("something went wrong")
	err := WriteResult(&buf, nil, nil, inputErr)
	if !errors.Is(err, ErrAlreadyWritten) {
		t.Fatalf("err = %v, want ErrAlreadyWritten", err)
	}

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Error == nil {
		t.Fatal("error should not be nil")
	}
	if env.Error.Code != "internal" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "internal")
	}
	if env.Error.Message != "internal error" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "internal error")
	}
}

func TestWriteResult_NullResult(t *testing.T) {
	var buf bytes.Buffer
	err := WriteResult(&buf, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse as raw JSON to check "result" is null, not absent.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	resultField, ok := raw["result"]
	if !ok {
		t.Fatal("result field missing from JSON")
	}
	if string(resultField) != "null" {
		t.Errorf("result = %s, want null", string(resultField))
	}
	if _, ok := raw["warnings"]; ok {
		t.Error("warnings field should be absent for void-success with no warnings")
	}
}

func TestWriteResult_SuccessNoWarnings(t *testing.T) {
	var buf bytes.Buffer
	result := map[string]string{"id": "T1"}
	err := WriteResult(&buf, result, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := raw["warnings"]; ok {
		t.Error("warnings field should be absent when no warnings provided")
	}
}
