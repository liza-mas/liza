package embedded

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestOpenCodeExecRuntimeBudgetsBeforeReturningToAgent(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("Bun is required for the OpenCode TypeScript runtime test")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	// Mock only OpenCode's schema builder; execute is the actual rendered asset.
	content := strings.Replace(string(OpenCodeExecToolContent()), `import { tool } from "@opencode-ai/plugin"`, `const schema: any = new Proxy({}, { get: () => () => schema }); const tool: any = Object.assign((x: any) => x, { schema })`, 1)
	files := map[string]string{
		filepath.Join(dir, "exec.ts"): content,
		filepath.Join(bin, brand.BinaryName): "#!" + bun + "\n" + `
const raw = await Bun.stdin.text();
const payload = JSON.parse(raw);
if (process.argv.includes(payload.command)) process.exit(8);
await Bun.write(process.env.CAPTURE, raw);
if (process.env.FORMATTER_FAIL === "1") { console.log("RAW MUST NOT ESCAPE"); process.exit(1); }
if (process.env.FORMATTER_FLOOD === "1") { process.stdout.write("x".repeat(70000)); process.exit(0); }
process.stdout.write(payload.content.length > 32768 ? JSON.stringify({hash:"a".repeat(64),bytes:payload.content.length,exit_code:payload.exit_code}) : payload.content);
`,
		filepath.Join(dir, "check.ts"): `
import tool from "./exec.ts";
import assert from "node:assert/strict";
const context = { worktree: process.cwd(), agent: "coder", sessionID: "session-778" };
const large = await tool.execute({cmd: "awk 'BEGIN {for(i=0;i<8000;i++) print \"row.go:42 important match\"}'"}, context);
const payload = JSON.parse(await Bun.file(process.env.CAPTURE).text());
assert(payload.content.length > 160000, "complete stdout must reach formatter");
assert(large.length < 4096, "only bounded result returns to agent");
assert.equal(payload.session_id, "session-778"); assert.equal(payload.role, "coder"); assert.equal(payload.exit_code, 0);
assert.equal(payload.truncated, false);
const small = await tool.execute({cmd:"printf 'precise result'"}, context);
assert.equal(small, "exit_code: 0\nstdout:\nprecise result");
const trailing = await tool.execute({cmd:"printf 'tail\\n\\n'"}, context);
assert.equal(trailing, "exit_code: 0\nstdout:\ntail\n\n");
const whitespace = await tool.execute({cmd:"awk 'BEGIN {printf \"%40000s\", \"\"}'"}, context);
const whitespacePayload = JSON.parse(await Bun.file(process.env.CAPTURE).text());
assert(whitespacePayload.content.endsWith(" ".repeat(40000))); assert(whitespace.length<4096);
const unicode = await tool.execute({cmd:"printf '\\360'; sleep 0.01; printf '\\237\\230\\200'"}, context);
assert.equal(unicode, "exit_code: 0\nstdout:\n😀");
const failed = await tool.execute({cmd:"printf 'failure evidence' >&2; exit 9"}, context);
assert.equal(failed, "exit_code: 9\nstderr:\nfailure evidence");
const failurePayload = JSON.parse(await Bun.file(process.env.CAPTURE).text()); assert.equal(failurePayload.exit_code,9);
const started = Date.now();
const background = await tool.execute({cmd:"sleep 5 & echo started"}, context);
assert(Date.now() - started < 4000, "background job must not hold the result");
assert(background.startsWith("exit_code: 0\nbackground_output_detached: true\nstdout:\nstarted"), background);
assert.equal(JSON.parse(await Bun.file(process.env.CAPTURE).text()).truncated, true);
process.env.FORMATTER_FAIL="1";
const withheld = await tool.execute({cmd:"printf 'sensitive raw output'"}, context);
assert(withheld.includes("raw output withheld")); assert(!withheld.includes("sensitive")); assert(!withheld.includes("RAW MUST"));
delete process.env.FORMATTER_FAIL; process.env.FORMATTER_FLOOD="1";
const flood = await tool.execute({cmd:"printf 'result'"}, context);
assert(flood.includes("raw output withheld")); assert(flood.length<1024);
console.log("PASS: full >160KiB capture, bounded agent return, exact small output, exit/stderr/session metadata, fail-closed formatter and flood guard");
`,
	}
	for path, value := range files {
		if err := os.WriteFile(path, []byte(value), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bun, "check.ts")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "CAPTURE="+filepath.Join(dir, "payload.json"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real OpenCode exec path: %v\n%s", err, output)
	}
	t.Log(string(output))
}
