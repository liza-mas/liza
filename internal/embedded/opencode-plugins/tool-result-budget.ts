// __BRAND_NAME_UPPER__ MANAGED FILE: OpenCode tool-result budget plugin. Safe for __BRAND_NAME_TITLE__ to overwrite.
import { Buffer } from "node:buffer"
import { spawn } from "node:child_process"
import { constants } from "node:fs"
import { lstat, mkdir, open, readFile } from "node:fs/promises"
import { createHash } from "node:crypto"
import { join, parse } from "node:path"
import process from "node:process"
import type { Plugin } from "@opencode-ai/plugin"

const failure = "tool_result_error: sanitized artifact processing failed; raw output withheld"
const managedExecHeader = "// __BRAND_NAME_UPPER__ MANAGED FILE: OpenCode exec compatibility tool."

const budget = (payload: Record<string, unknown>, directory: string): Promise<string> => new Promise((resolve) => {
  const child = spawn("__BRAND_BINARY_NAME__", ["--project-root", directory, "tool-result", "filter", "--json-input"], { cwd: directory, env: process.env, stdio: ["pipe", "pipe", "ignore"] })
  const chunks: Buffer[] = []
  let total = 0
  let failed = false
  const timer = setTimeout(() => { failed = true; child.kill("SIGKILL"); resolve(failure) }, 30_000)
  child.stdout?.on("data", (chunk) => {
    total += chunk.length
    if (total > 65_536) { failed = true; child.kill("SIGKILL"); return }
    chunks.push(Buffer.from(chunk))
  })
  child.stdin?.on("error", () => { failed = true })
  child.on("error", () => { clearTimeout(timer); resolve(failure) })
  child.on("close", (code) => { clearTimeout(timer); resolve(code === 0 && !failed ? Buffer.concat(chunks).toString("utf8") : failure) })
  child.stdin?.end(JSON.stringify(payload))
})

type Content = { type: string; text?: string; resource?: { text?: string; blob?: string; [key: string]: unknown }; [key: string]: unknown }
type Result = { output?: string; content?: Content[]; title?: string; metadata?: Record<string, unknown>; [key: string]: unknown }
const textual = (part: Content): boolean => part.type === "text" && typeof part.text === "string" || part.type === "resource" && typeof part.resource?.text === "string" && !part.resource.blob

export const ToolResultBudget: Plugin = async ({ directory, worktree }) => {
  const root = worktree && worktree !== parse(worktree).root ? worktree : directory
  // A user-owned tool named exec has no pre-return guarantee and MUST be covered.
  const managedExec = await readFile(join(root, ".opencode", "tools", "exec.ts"), "utf8").then((text) => text.startsWith(managedExecHeader), () => false)
  // OpenCode throws MCP isError results before tool.execute.after. This final
  // pre-model hook covers those errors and interrupted terminal snapshots.
  // Durable sanitized receipts avoid recounting history on every model turn or
  // process restart. Names contain hashes only, never raw tool output.
  const errors = new Map<string, Promise<string>>()
  const policy = [process.env["__BRAND_ENV_PREFIX___TOOL_RESULT_THRESHOLD_BYTES"], process.env["__BRAND_ENV_PREFIX___TOOL_RESULT_DIGEST_BYTES"]]
  const errorBudget = (payload: Record<string, unknown>, callID: string): Promise<string> => {
    const key = createHash("sha256").update(JSON.stringify(["v1", policy, callID, payload])).digest("hex")
    if (errors.has(key)) return errors.get(key)!
    const pending = (async () => {
      let dir = root
      for (const component of ["__BRAND_PROJECT_DIRNAME__", "tool-results", "opencode-error-receipts"]) {
        dir = join(dir, component)
        await mkdir(dir, { mode: 0o700 }).catch((error) => { if (error.code !== "EEXIST") throw error })
        const info = await lstat(dir)
        if (!info.isDirectory() || info.isSymbolicLink()) throw new Error("unsafe receipt directory")
      }
      const file = join(dir, key + ".json")
      try {
        const handle = await open(file, constants.O_RDONLY | constants.O_NOFOLLOW)
        try {
          const info = await handle.stat()
          if (!info.isFile() || info.size > 65_536) throw new Error("invalid receipt")
          const receipt = JSON.parse(await handle.readFile("utf8"))
          if (typeof receipt.output !== "string") throw new Error("invalid receipt")
          return receipt.output
        } finally { await handle.close() }
      } catch (error: any) { if (error.code !== "ENOENT") throw error }
      const output = await budget(payload, root)
      const handle = await open(file, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600)
      try { await handle.writeFile(JSON.stringify({ output })) } finally { await handle.close() }
      return output
    })().catch(() => failure)
    errors.set(key, pending)
    void pending.then((output) => {
      const boundedKey = createHash("sha256").update(JSON.stringify(["v1", policy, callID, { ...payload, content: output }])).digest("hex")
      errors.set(boundedKey, pending)
    })
    return pending
  }
  return {
    "experimental.chat.messages.transform": async (_input, { messages }) => {
      for (const message of messages) for (const part of message.parts) {
        if (part.type !== "tool" || part.state.status !== "error") continue
        const state = part.state
        const interrupted = state.metadata?.interrupted === true && typeof state.metadata.output === "string"
        const content = interrupted ? state.metadata!.output as string : state.error
        const bounded = await errorBudget({
          tool: part.tool, command: JSON.stringify(state.input ?? {}), content,
          task_id: process.env["__BRAND_ENV_PREFIX__" + "_TASK_ID"] ?? "",
          agent_id: process.env["__BRAND_ENV_PREFIX__" + "_AGENT_ID"] ?? "",
          session_id: part.sessionID, exit_code: null, truncated: true,
        }, part.callID)
        if (interrupted) state.metadata!.output = bounded
        else state.error = bounded
      }
    },
    "tool.execute.after": async (input, returned) => {
      if (input.tool === "exec" && managedExec) return
      const result = returned as Result | undefined
      if (!result) return
      const blocks = Array.isArray(result.content) ? result.content.filter(textual) : []
      const content = typeof result.output === "string" ? result.output : blocks.length === 1 && blocks[0].type === "text" ? blocks[0].text! : blocks.length ? JSON.stringify(blocks) : undefined
      if (content === undefined) return
      let bounded: string
      try {
        bounded = await budget({
          tool: input.tool,
          command: JSON.stringify(input.args ?? {}),
          content,
          content_json: typeof result.output !== "string" && !(blocks.length === 1 && blocks[0].type === "text") && blocks.length > 0,
          task_id: process.env["__BRAND_ENV_PREFIX__" + "_TASK_ID"] ?? "",
          agent_id: process.env["__BRAND_ENV_PREFIX__" + "_AGENT_ID"] ?? "",
          session_id: input.sessionID,
          exit_code: typeof result.metadata?.exit === "number" ? result.metadata.exit : null,
          truncated: result.metadata?.truncated === true,
        }, root)
      } catch { bounded = failure }
      // Preserve exact small results, including MCP text block boundaries.
      if (bounded === content) return
      if (typeof result.output === "string") result.output = bounded
      if (blocks.length) {
        // Store contains the complete textual block envelope (including resource
        // URI/annotations) when there are multiple blocks or a text resource.
        // Non-text attachments stay untouched; a single digest cannot multiply
        // into one budget allowance per MCP block.
        result.content = [{ type: "text", text: bounded }, ...result.content!.filter((part) => !textual(part))]
      }
    },
  }
}
