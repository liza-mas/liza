// LIZA MANAGED FILE: OpenCode exec compatibility tool. Safe for Liza to overwrite.
import { Buffer } from "node:buffer"
import { spawn } from "node:child_process"
import process from "node:process"
import { tool } from "@opencode-ai/plugin"

const DEFAULT_TIMEOUT_MS = 120_000
const FORCE_KILL_DELAY_MS = 2_000
const OUTPUT_LIMIT_BYTES = 20_000

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined
}

function appendLimited(value: string, usedBytes: number, chunk: unknown): [string, number, number] {
  const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk))
  const available = Math.max(OUTPUT_LIMIT_BYTES - usedBytes, 0)
  if (available === 0) return [value, usedBytes, bytes.length]
  const kept = bytes.subarray(0, available)
  return [value + kept.toString("utf8"), usedBytes + kept.length, Math.max(bytes.length - available, 0)]
}

function formatOutput(label: string, value: string, truncated: number): string | undefined {
  if (value.trim().length === 0 && truncated === 0) return undefined
  const suffix = truncated > 0 ? `\n[truncated ${truncated} bytes]` : ""
  return `${label}:\n${value.trimEnd()}${suffix}`
}

function killChildTree(child: ReturnType<typeof spawn>, signal: "SIGTERM" | "SIGKILL") {
  if (process.platform !== "win32" && typeof child.pid === "number") {
    try {
      process.kill(-child.pid, signal)
      return
    } catch {
      // Fall back to the shell process if process-group signaling is unavailable.
    }
  }
  child.kill(signal)
}

function defaultWorkdir(context: unknown): string {
  const ctx = context as Record<string, unknown>
  return (
    stringValue(ctx.worktree) ??
    stringValue(ctx.directory) ??
    stringValue(ctx.cwd) ??
    process.cwd()
  )
}

export default tool({
  description:
    "Run a shell command for trusted Liza bridge work. The cmd string is executed through the system shell and is not safe for less-trusted contexts. Prefer this exec tool for shell and file operations instead of built-in bash/read/write tools. Omit optional fields when they are not needed; null is tolerated and treated as omitted. Do not repeat the same successful command. After a successful command, inspect the result and move to the next Liza protocol step.",
  args: {
    cmd: tool.schema.string().describe("Shell command to run."),
    workdir: tool.schema
      .string()
      .nullable()
      .optional()
      .describe("Working directory. Omit when using the OpenCode worktree or directory."),
    timeout_ms: tool.schema
      .number()
      .nullable()
      .optional()
      .describe("Timeout in milliseconds. Omit for Liza's default timeout."),
  },
  async execute(args, context) {
    const cwd = args.workdir ?? defaultWorkdir(context)
    const timeoutMs =
      typeof args.timeout_ms === "number" && Number.isFinite(args.timeout_ms) && args.timeout_ms > 0
        ? args.timeout_ms
        : DEFAULT_TIMEOUT_MS

    return await new Promise<string>((resolve) => {
      let stdout = ""
      let stderr = ""
      let stdoutBytes = 0
      let stderrBytes = 0
      let stdoutTruncated = 0
      let stderrTruncated = 0
      let timedOut = false
      let forceKill: ReturnType<typeof setTimeout> | undefined

      const child = spawn(args.cmd, {
        cwd,
        detached: process.platform !== "win32",
        env: process.env,
        shell: true,
      })

      const timeout = setTimeout(() => {
        timedOut = true
        killChildTree(child, "SIGTERM")
        forceKill = setTimeout(() => {
          killChildTree(child, "SIGKILL")
        }, FORCE_KILL_DELAY_MS)
      }, timeoutMs)

      child.stdout?.on("data", (chunk) => {
        const [next, used, truncated] = appendLimited(stdout, stdoutBytes, chunk)
        stdout = next
        stdoutBytes = used
        stdoutTruncated += truncated
      })
      child.stderr?.on("data", (chunk) => {
        const [next, used, truncated] = appendLimited(stderr, stderrBytes, chunk)
        stderr = next
        stderrBytes = used
        stderrTruncated += truncated
      })

      child.on("error", (error) => {
        clearTimeout(timeout)
        if (forceKill) clearTimeout(forceKill)
        resolve(`exit_code: 127\nerror: ${error.message}`)
      })

      child.on("close", (code, signal) => {
        clearTimeout(timeout)
        if (forceKill) clearTimeout(forceKill)
        const parts = [`exit_code: ${code ?? -1}`]
        if (signal) parts.push(`signal: ${signal}`)
        if (timedOut) parts.push(`timed_out: true`)
        const stdoutPart = formatOutput("stdout", stdout, stdoutTruncated)
        const stderrPart = formatOutput("stderr", stderr, stderrTruncated)
        if (stdoutPart) parts.push(stdoutPart)
        if (stderrPart) parts.push(stderrPart)
        resolve(parts.join("\n"))
      })
    })
  },
})
