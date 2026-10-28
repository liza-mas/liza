package process

import "testing"

// The supervisor output directory keeps its permission after the K7 naming.
func TestA9SupervisorOutputDirModeKeepsValue(t *testing.T) {
	if supervisorOutputDirMode != 0o755 {
		t.Fatalf("supervisorOutputDirMode = %o, want 755", supervisorOutputDirMode)
	}
}
