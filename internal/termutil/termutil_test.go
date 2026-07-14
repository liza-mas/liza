package termutil

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadLineKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"single y", "y\n", "y"},
		{"single n", "n\n", "n"},
		{"uppercase Y", "Y\n", "y"},
		{"uppercase N", "N\n", "n"},
		{"full yes", "yes\n", "y"},
		{"full no", "no\n", "n"},
		{"with spaces", "  y  \n", "y"},
		{"empty", "\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			result, err := ReadLineKey(reader)
			if err != nil {
				t.Fatalf("ReadLineKey() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("ReadLineKey() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsInteractive(t *testing.T) {
	// This test just verifies the function doesn't panic
	// In a test environment, this will typically return false
	result := IsInteractive()
	// We can't make assertions about the result since it depends on the test environment
	_ = result
}
