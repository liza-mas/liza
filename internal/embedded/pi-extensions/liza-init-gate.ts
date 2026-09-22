// __BRAND_NAME_UPPER__ MANAGED FILE: pi init gate. Safe for __BRAND_NAME_TITLE__ to overwrite.
//
// Enforcement counterpart of .claude/hooks/enforce-init.sh and .codex/hooks/
// enforce-init.sh for the pi harness: mutating tools (bash, powershell, edit,
// write, and unrecognized custom tools) stay blocked until the behavioral
// contract documents and GUARDRAILS.md have actually been read in this
// session. Read-only tools remain available so the initialization sequence
// itself can complete. CORE.md is not gated: pi delivers it as AGENTS.md
// context before the first turn.
//
// Loaded by __BRAND_BINARY_NAME__ through the pi provider's run_args
// (`pi -p -e <projectRoot>/.pi/extensions/liza-init-gate.ts ...`).

import { homedir } from "node:os"
import * as fs from "node:fs"
import * as path from "node:path"

const globalDir = path.join(homedir(), "__BRAND_GLOBAL_DIRNAME__")

const AGENT_TOOLS_DOC = path.join(globalDir, "AGENT_TOOLS.md")
const MULTI_AGENT_DOC = path.join(globalDir, "MULTI_AGENT_MODE.md")
const PAIRING_DOC = path.join(globalDir, "PAIRING_MODE.md")
const GUARDRAILS_DOC = path.join(process.cwd(), "GUARDRAILS.md")

// Tools that only inspect the world. They stay available during init — the
// gate exists to force reading, not to prevent it.
const READ_ONLY_TOOLS = new Set(["read", "grep", "find", "ls", "glob", "list"])

// Docs that must be read before any mutating tool call. The mode doc accepts
// either variant because the base prompt names one depending on pairing vs
// multi-agent session.
const requiredDocs = [AGENT_TOOLS_DOC, MULTI_AGENT_DOC, PAIRING_DOC, GUARDRAILS_DOC]
const read = new Set<string>()

function normalizeDocPath(value) {
  if (typeof value !== "string" || value.length === 0) return ""
  let expanded = value
  if (expanded === "~") expanded = homedir()
  else if (expanded.startsWith("~/")) expanded = path.join(homedir(), expanded.slice(2))
  return path.resolve(expanded)
}

// Same file via different paths (/tmp vs /private/tmp, symlinked homes, ...)
// must count as read. Falls back to lexical equality when a path does not
// resolve (e.g. the doc was deleted between the read and the comparison).
function sameFile(a, b) {
  if (a === b) return true
  try {
    return fs.realpathSync(a) === fs.realpathSync(b)
  } catch {
    return false
  }
}

// A doc that does not exist on disk can never be read; demanding it would
// deadlock the session (pairing workspaces have no GUARDRAILS.md, machines
// without `liza setup` have no AGENT_TOOLS.md). Absent docs are skipped.
function missingDocs() {
  const missing = []
  if (!read.has(AGENT_TOOLS_DOC) && fs.existsSync(AGENT_TOOLS_DOC)) missing.push(AGENT_TOOLS_DOC)
  if (!read.has(MULTI_AGENT_DOC) && !read.has(PAIRING_DOC)) {
    if (fs.existsSync(MULTI_AGENT_DOC)) missing.push(MULTI_AGENT_DOC)
    else if (fs.existsSync(PAIRING_DOC)) missing.push(PAIRING_DOC)
  }
  if (!read.has(GUARDRAILS_DOC) && fs.existsSync(GUARDRAILS_DOC)) missing.push(GUARDRAILS_DOC)
  return missing
}

export default function (pi) {
  pi.on("tool_call", (event) => {
    const toolName = String(event.toolName ?? "").toLowerCase()

    if (toolName === "read") {
      const requested = normalizeDocPath(event.input?.path)
      for (const doc of requiredDocs) {
        if (sameFile(requested, doc)) read.add(doc)
      }
      return
    }

    if (READ_ONLY_TOOLS.has(toolName)) return

    const missing = missingDocs()
    if (missing.length === 0) return

    return {
      block: true,
      reason:
        "__BRAND_NAME_TITLE__ init gate: complete the initialization sequence before using the '" +
        event.toolName +
        "' tool. Read these files FULLY, one at a time and in this order, with the read tool: " +
        missing.join(", ") +
        ". Do not fake reads, do not batch or parallelize them, and do not treat the user prompt as a replacement.",
    }
  })
}
