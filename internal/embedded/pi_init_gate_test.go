package embedded

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestWritePiInitGateCreatesManagedExtension(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	if err := WritePiInitGate(); err != nil {
		t.Fatalf("WritePiInitGate() error = %v", err)
	}

	gatePath := filepath.Join(fakeHome, brand.RuntimeValues().GlobalDirName, "extensions", "init-gate.ts")
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

// TestPiInitGateSourceHasNoRawBrandLiteral keeps the deployed gate brand
// neutral (GUARDRAILS G1.3): every brand reference must be a placeholder that
// renderEmbeddedAsset rewrites for the building brand.
func TestPiInitGateSourceHasNoRawBrandLiteral(t *testing.T) {
	if loc := regexp.MustCompile(`(?i)\bliza\b`).FindIndex(piInitGateContent); loc != nil {
		t.Fatalf("pi init gate source contains raw brand literal %q; use a __BRAND_*__ placeholder", piInitGateContent[loc[0]:loc[1]])
	}
}

func TestWritePiInitGateDoesNotClobberForeignFile(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	gateDir := filepath.Join(fakeHome, brand.RuntimeValues().GlobalDirName, "extensions")
	if err := os.MkdirAll(gateDir, 0755); err != nil {
		t.Fatal(err)
	}
	foreign := "// my carefully hand-tuned gate\nexport default function (pi) {}\n"
	gatePath := filepath.Join(gateDir, "init-gate.ts")
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

// piGateStep is one synthetic pi tool_call fed to the gate. The driver passes
// Path as event.input.path, the field pi's read tool uses for its target.
type piGateStep struct {
	Tool string `json:"tool"`
	Path string `json:"path,omitempty"`
}

type piGateResult struct {
	Block  bool   `json:"block"`
	Reason string `json:"reason"`
}

const piGateDriver = `import gate from "./init-gate.ts"

let handler
gate({ on(name, h) { if (name === "tool_call") handler = h } })
if (typeof handler !== "function") throw new Error("gate did not register a tool_call handler")

const steps = JSON.parse(process.argv[2])
const out = steps.map((step) => {
  const input = step.path === undefined ? {} : { path: step.path }
  const result = handler({ type: "tool_call", toolCallId: "t", toolName: step.tool, input })
  return result ? { block: result.block === true, reason: String(result.reason ?? "") } : null
})
process.stdout.write(JSON.stringify(out))
`

// piGateRuntime returns an argv prefix able to execute the TypeScript gate
// directly: bun, or node with type stripping. Skips when neither is present so
// the suite stays runnable on machines without a JS runtime.
func piGateRuntime(t *testing.T, dir string) []string {
	t.Helper()
	if bun, err := exec.LookPath("bun"); err == nil {
		return []string{bun}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		skipOrFailWithoutRuntime(t, "neither bun nor node on PATH; cannot execute the pi init gate")
	}
	probe := filepath.Join(dir, "probe.ts")
	if err := os.WriteFile(probe, []byte("const n: number = 1\nexport {}\nprocess.stdout.write(String(n))\n"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	for _, flags := range [][]string{nil, {"--experimental-strip-types"}} {
		argv := append(append([]string{node}, flags...), probe)
		if out, err := exec.Command(argv[0], argv[1:]...).Output(); err == nil && string(out) == "1" {
			return argv[:len(argv)-1]
		}
	}
	skipOrFailWithoutRuntime(t, "node on PATH cannot strip TypeScript types (needs >= 22.6); cannot execute the pi init gate")
	return nil
}

// skipOrFailWithoutRuntime skips locally but fails on CI, where the workflow
// provisions node: a silent skip there would drop the gate's only behavioral
// coverage while the suite stays green.
func skipOrFailWithoutRuntime(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatal(reason)
	}
	t.Skip(reason)
}

// runPiGate renders the embedded gate, loads it in a fresh JS process with
// home and working directory set, and returns the gate's verdict per step.
func runPiGate(t *testing.T, home, project string, steps []piGateStep) []*piGateResult {
	t.Helper()
	extDir := t.TempDir()
	for name, content := range map[string][]byte{
		"init-gate.ts": PiInitGateContent(),
		"driver.mjs":   []byte(piGateDriver),
		"package.json": []byte(`{"type":"module"}`),
	} {
		if err := os.WriteFile(filepath.Join(extDir, name), content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	argv := append(piGateRuntime(t, extDir), filepath.Join(extDir, "driver.mjs"), string(stepsJSON))
	t.Logf("pi init gate runtime: %v", argv[:len(argv)-2])
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run pi init gate with %v: %v\nstderr:\n%s", argv[0], err, stderr.String())
	}
	var results []*piGateResult
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("decode gate output %q: %v", out, err)
	}
	if len(results) != len(steps) {
		t.Fatalf("gate returned %d results for %d steps", len(results), len(steps))
	}
	return results
}

func writeDocs(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# "+name+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPiInitGateEnforcesInitSequence executes the gate against the two pi
// contracts it depends on: read reports its target as event.input.path, and
// a returned {block, reason} is the blocking verdict. A wrong input field
// would keep every mutating tool blocked forever; a wrong return shape would
// enforce nothing.
func TestPiInitGateEnforcesInitSequence(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	globalDir := filepath.Join(home, brand.RuntimeValues().GlobalDirName)
	writeDocs(t, globalDir, "AGENT_TOOLS.md", "MULTI_AGENT_MODE.md", "PAIRING_MODE.md")
	writeDocs(t, project, "GUARDRAILS.md")

	steps := []piGateStep{
		{Tool: "bash"},
		{Tool: "read", Path: "~/" + brand.RuntimeValues().GlobalDirName + "/AGENT_TOOLS.md"},
		{Tool: "grep"},
		{Tool: "edit"},
		{Tool: "read", Path: filepath.Join(globalDir, "PAIRING_MODE.md")},
		{Tool: "write"},
		{Tool: "read", Path: "GUARDRAILS.md"},
		{Tool: "write"},
		{Tool: "my_custom_tool"},
	}
	got := runPiGate(t, home, project, steps)

	blocked := func(i int, wantDocs []string, notDocs []string) {
		t.Helper()
		if got[i] == nil || !got[i].Block {
			t.Fatalf("step %d (%s) = %+v, want blocked", i, steps[i].Tool, got[i])
		}
		for _, doc := range wantDocs {
			if !strings.Contains(got[i].Reason, doc) {
				t.Errorf("step %d reason %q missing %s", i, got[i].Reason, doc)
			}
		}
		for _, doc := range notDocs {
			if strings.Contains(got[i].Reason, doc) {
				t.Errorf("step %d reason %q still demands already-read %s", i, got[i].Reason, doc)
			}
		}
	}
	allowed := func(i int) {
		t.Helper()
		if got[i] != nil {
			t.Fatalf("step %d (%s) = %+v, want allowed", i, steps[i].Tool, got[i])
		}
	}

	blocked(0, []string{"AGENT_TOOLS.md", "MULTI_AGENT_MODE.md", "GUARDRAILS.md"}, nil)
	allowed(1) // read is never blocked; the tilde path marks AGENT_TOOLS.md read
	allowed(2) // read-only tools stay available during init
	blocked(3, []string{"MULTI_AGENT_MODE.md", "GUARDRAILS.md"}, []string{"AGENT_TOOLS.md"})
	allowed(4) // either mode doc satisfies the mode requirement
	blocked(5, []string{"GUARDRAILS.md"}, []string{"AGENT_TOOLS.md", "MODE.md"})
	allowed(6) // relative path resolves against the session cwd
	allowed(7) // all required docs read: the gate unblocks
	allowed(8)
}

// TestPiInitGateSkipsAbsentDocs covers a pairing workspace: no GUARDRAILS.md,
// no multi-agent doc, no AGENT_TOOLS.md. Demanding absent docs would deadlock
// the session, and a read through a symlinked path must still count.
func TestPiInitGateSkipsAbsentDocs(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	globalDir := filepath.Join(home, brand.RuntimeValues().GlobalDirName)
	writeDocs(t, globalDir, "PAIRING_MODE.md")

	pairingRead := filepath.Join(globalDir, "PAIRING_MODE.md")
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "home-alias")
		if err := os.Symlink(home, alias); err != nil {
			t.Fatal(err)
		}
		pairingRead = filepath.Join(alias, brand.RuntimeValues().GlobalDirName, "PAIRING_MODE.md")
	}

	steps := []piGateStep{
		{Tool: "bash"},
		{Tool: "read", Path: pairingRead},
		{Tool: "bash"},
	}
	got := runPiGate(t, home, project, steps)

	if got[0] == nil || !got[0].Block || !strings.Contains(got[0].Reason, "PAIRING_MODE.md") {
		t.Fatalf("step 0 = %+v, want blocked on PAIRING_MODE.md", got[0])
	}
	for _, absent := range []string{"GUARDRAILS.md", "AGENT_TOOLS.md", "MULTI_AGENT_MODE.md"} {
		if strings.Contains(got[0].Reason, absent) {
			t.Errorf("step 0 reason %q demands absent %s", got[0].Reason, absent)
		}
	}
	if got[1] != nil || got[2] != nil {
		t.Fatalf("after reading the only present doc: read=%+v bash=%+v, want both allowed", got[1], got[2])
	}
}
