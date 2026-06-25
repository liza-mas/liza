package statehygiene

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

const MaxStateTextBytes = 4096

func agentOutputsRelPath() string {
	return filepath.ToSlash(filepath.Join(paths.ProjectDirName(), paths.AgentOutputsDirName))
}

func scrubbedProviderAuditMessage() string {
	return fmt.Sprintf("provider audit degraded; raw provider event omitted from state; inspect %s and alerts for transcript evidence", agentOutputsRelPath())
}

func scrubbedStateMessage() string {
	return fmt.Sprintf("raw state payload omitted; inspect %s and alerts for evidence", agentOutputsRelPath())
}

var cappedTextFields = map[string]bool{
	"message":    true,
	"summary":    true,
	"excerpt":    true,
	"note":       true,
	"reason":     true,
	"command":    true,
	"hypothesis": true,
	"next_step":  true,
}

// ValidateState rejects transcript-shaped provider payloads and oversized
// human-readable text fields in state. Raw evidence belongs in agent-output
// logs; state keeps only bounded orchestration facts.
func ValidateState(state *models.State) error {
	return validateValue(reflect.ValueOf(state), "state", "")
}

// ScrubStateForMigration removes known raw transcript payloads from legacy
// state while preserving the surrounding orchestration/anomaly records.
func ScrubStateForMigration(state *models.State) bool {
	changed := false
	for i := range state.Anomalies {
		anomaly := &state.Anomalies[i]
		if anomaly.Details == nil {
			continue
		}
		message, ok := anomaly.Details["message"].(string)
		if !ok {
			continue
		}
		if !IsTranscriptPayload(message) && len([]byte(message)) <= MaxStateTextBytes {
			continue
		}
		if anomaly.Type == "provider_audit_degraded" {
			anomaly.Details["message"] = scrubbedProviderAuditMessage()
		} else {
			anomaly.Details["message"] = scrubbedStateMessage()
		}
		anomaly.Details["message_scrubbed"] = true
		anomaly.Details["scrub_reason"] = scrubReason(message)
		anomaly.Details["original_message_bytes"] = len([]byte(message))
		changed = true
	}
	if scrubValue(reflect.ValueOf(state), "") {
		changed = true
	}
	return changed
}

// IsTranscriptPayload detects raw provider transcript/event records that should
// stay in agent output logs rather than state.yaml.
func IsTranscriptPayload(s string) bool {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "item.completed") ||
		strings.Contains(lower, "command_execution") ||
		strings.Contains(lower, "aggregated_output") {
		if hasJSONTranscriptShape(s) {
			return true
		}
	}
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
	return strings.Contains(compact, `"type":"item.completed"`) ||
		(strings.Contains(compact, `"type":"command_execution"`) && strings.Contains(compact, `"aggregated_output"`)) ||
		(strings.Contains(compact, `item.completed`) && strings.Contains(compact, `command_execution`) && strings.Contains(compact, `aggregated_output`)) ||
		strings.Contains(compact, `\"type\":\"item.completed\"`)
}

func scrubReason(s string) string {
	if IsTranscriptPayload(s) {
		return "raw_provider_transcript"
	}
	return "oversized_state_message"
}

func validateValue(v reflect.Value, path, fieldName string) error {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		return validateValue(v.Elem(), path, fieldName)
	}

	switch v.Kind() {
	case reflect.String:
		value := v.String()
		if IsTranscriptPayload(value) {
			return fmt.Errorf("%s contains raw provider transcript payload; store raw evidence under %s and keep only a bounded summary/log_ref in state.yaml", path, agentOutputsRelPath())
		}
		if isCappedTextField(fieldName) && len([]byte(value)) > MaxStateTextBytes {
			return fmt.Errorf("%s is %d bytes, exceeds %d-byte state text limit; store raw evidence under %s and keep a bounded summary/log_ref in state.yaml", path, len([]byte(value)), MaxStateTextBytes, agentOutputsRelPath())
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			childName := yamlFieldName(field)
			childPath := path + "." + childName
			if err := validateValue(v.Field(i), childPath, childName); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := validateValue(v.Index(i), fmt.Sprintf("%s[%d]", path, i), fieldName); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			keyName := fmt.Sprint(key.Interface())
			if err := validateValue(v.MapIndex(key), path+"."+keyName, keyName); err != nil {
				return err
			}
		}
	}
	return nil
}

func scrubValue(v reflect.Value, fieldName string) bool {
	if !v.IsValid() {
		return false
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		return scrubValue(v.Elem(), fieldName)
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return false
		}
		if v.Elem().Kind() == reflect.String {
			value := v.Elem().String()
			if shouldScrubString(value, fieldName) {
				v.Set(reflect.ValueOf(scrubbedStateMessage()))
				return true
			}
			return false
		}
		return scrubValue(v.Elem(), fieldName)
	}

	switch v.Kind() {
	case reflect.String:
		if !v.CanSet() {
			return false
		}
		if shouldScrubString(v.String(), fieldName) {
			v.SetString(scrubbedStateMessage())
			return true
		}
	case reflect.Struct:
		changed := false
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			if scrubValue(v.Field(i), yamlFieldName(field)) {
				changed = true
			}
		}
		return changed
	case reflect.Slice:
		changed := false
		for i := 0; i < v.Len(); i++ {
			if scrubValue(v.Index(i), fieldName) {
				changed = true
			}
		}
		return changed
	case reflect.Map:
		changed := false
		for _, key := range v.MapKeys() {
			keyName := fmt.Sprint(key.Interface())
			next, didChange := scrubAny(v.MapIndex(key).Interface(), keyName)
			if didChange {
				v.SetMapIndex(key, reflect.ValueOf(next))
				changed = true
			}
		}
		return changed
	}
	return false
}

func scrubAny(value any, fieldName string) (any, bool) {
	switch typed := value.(type) {
	case string:
		if shouldScrubString(typed, fieldName) {
			return scrubbedStateMessage(), true
		}
		return typed, false
	case map[string]any:
		changed := false
		for k, v := range typed {
			next, didChange := scrubAny(v, k)
			if didChange {
				typed[k] = next
				changed = true
			}
		}
		return typed, changed
	case []any:
		changed := false
		for i, v := range typed {
			next, didChange := scrubAny(v, fieldName)
			if didChange {
				typed[i] = next
				changed = true
			}
		}
		return typed, changed
	default:
		return value, false
	}
}

func shouldScrubString(value, fieldName string) bool {
	return IsTranscriptPayload(value) || (isCappedTextField(fieldName) && len([]byte(value)) > MaxStateTextBytes)
}

func hasJSONTranscriptShape(s string) bool {
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(s)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return false
	}
	return containsTranscriptShape(decoded)
}

func containsTranscriptShape(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		if isItemCompleted(typed) || isCommandExecutionWithOutput(typed) {
			return true
		}
		for _, v := range typed {
			if containsTranscriptShape(v) {
				return true
			}
		}
	case []any:
		for _, v := range typed {
			if containsTranscriptShape(v) {
				return true
			}
		}
	}
	return false
}

func isItemCompleted(value map[string]any) bool {
	typeValue, _ := value["type"].(string)
	return strings.EqualFold(typeValue, "item.completed")
}

func isCommandExecutionWithOutput(value map[string]any) bool {
	typeValue, _ := value["type"].(string)
	if !strings.EqualFold(typeValue, "command_execution") {
		return false
	}
	_, hasAggregatedOutput := value["aggregated_output"]
	return hasAggregatedOutput
}

func isCappedTextField(fieldName string) bool {
	return cappedTextFields[strings.ToLower(fieldName)]
}

func yamlFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("yaml")
	if tag == "" {
		return strings.ToLower(field.Name)
	}
	name := strings.Split(tag, ",")[0]
	if name == "" || name == "-" {
		return strings.ToLower(field.Name)
	}
	return name
}
