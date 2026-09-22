package embedded

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePiInitGateCreatesManagedExtension(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	if err := WritePiInitGate(); err != nil {
		t.Fatalf("WritePiInitGate() error = %v", err)
	}

	gatePath := filepath.Join(fakeHome, ".liza", "extensions", "liza-init-gate.ts")
	content, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatalf("pi init gate not written: %v", err)
	}

	if !strings.HasPrefix(string(content), PiInitGateManagedHeader()) {
		t.Fatalf("pi init gate missing managed header:\n%s", string(content))
	}
	for _, want := range []string{
		`pi.on("tool_call"`,
		"AGENT_TOOLS.md",
		"MULTI_AGENT_MODE.md",
		"PAIRING_MODE.md",
		"GUARDRAILS.md",
		"block: true",
		// Deadlock guard: absent docs are skipped, not demanded.
		"fs.existsSync",
		// Same file through symlinked or /tmp-vs-/private/tmp paths must count.
		"fs.realpathSync",
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("pi init gate missing %q:\n%s", want, string(content))
		}
	}

	// Idempotent: rewriting a managed file succeeds and keeps it managed.
	if err := WritePiInitGate(); err != nil {
		t.Fatalf("second WritePiInitGate() error = %v", err)
	}
	again, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatalf("reread pi init gate: %v", err)
	}
	if string(again) != string(content) {
		t.Fatal("managed pi init gate content drifted between writes")
	}
}

func TestWritePiInitGateDoesNotClobberForeignFile(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	gateDir := filepath.Join(fakeHome, ".liza", "extensions")
	if err := os.MkdirAll(gateDir, 0755); err != nil {
		t.Fatal(err)
	}
	foreign := "// my carefully hand-tuned gate\nexport default function (pi) {}\n"
	gatePath := filepath.Join(gateDir, "liza-init-gate.ts")
	if err := os.WriteFile(gatePath, []byte(foreign), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WritePiInitGate(); err != nil {
		t.Fatalf("WritePiInitGate() error = %v", err)
	}

	content, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != foreign {
		t.Fatalf("foreign pi init gate was overwritten:\n%s", string(content))
	}
}
