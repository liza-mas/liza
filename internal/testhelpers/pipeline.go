package testhelpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
)

// SetupPipelineConfig writes the embedded pipeline.yaml into the .liza/ directory
// of the given tmpDir. Creates the .liza directory if it doesn't exist.
func SetupPipelineConfig(t *testing.T, tmpDir string) {
	t.Helper()

	lizaDir := filepath.Join(tmpDir, ".liza")
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatalf("Failed to create .liza directory: %v", err)
	}
	if err := embedded.WritePipelineConfig(lizaDir, nil); err != nil {
		t.Fatalf("Failed to write pipeline config: %v", err)
	}
}

// SetupPipelineConfigBytes writes the supplied pipeline config into the .liza/
// directory of tmpDir. It is useful for exercising frozen legacy topologies.
func SetupPipelineConfigBytes(t *testing.T, tmpDir string, content []byte) {
	t.Helper()

	lizaDir := filepath.Join(tmpDir, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("Failed to create .liza directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lizaDir, "pipeline.yaml"), content, 0o644); err != nil {
		t.Fatalf("Failed to write pipeline config: %v", err)
	}
}
