// LIZA MANAGED FILE: OpenCode exec compatibility tool. Safe for Liza to overwrite.
import { spawn } from "node:child_process"
import process from "node:process"
import { tool } from "@opencode-ai/plugin"

const DEFAULT_TIMEOUT_MS = 120_000
const OUTPUT_LIMIT = 20_000

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined
}

function truncateOutput(value: string): string {
  if (value.length <= OUTPUT_LIMIT) return value
  return `${value.slice(0, OUTPUT_LIMIT)}\n[truncated ${value.length - OUTPUT_LIMIT} bytes]`
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
    "Run a shell command for Liza bridge work. Prefer this exec tool for shell and file operations instead of built-in bash/read/write tools. Omit optional fields when they are not needed; null is tolerated and treated as omitted. Do not repeat the same successful command. After a successful command, inspect the result and move to the next Liza protocol step.",
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
      let timedOut = false

      const child = spawn(args.cmd, {
        cwd,
        env: process.env,
        shell: true,
      })

      const timeout = setTimeout(() => {
        timedOut = true
        child.kill("SIGTERM")
      }, timeoutMs)

      child.stdout?.on("data", (chunk) => {
        stdout += chunk.toString()
      })
      child.stderr?.on("data", (chunk) => {
        stderr += chunk.toString()
      })

      child.on("error", (error) => {
        clearTimeout(timeout)
        resolve(`exit_code: 127\nerror: ${error.message}`)
      })

      child.on("close", (code, signal) => {
        clearTimeout(timeout)
        const parts = [`exit_code: ${code ?? -1}`]
        if (signal) parts.push(`signal: ${signal}`)
        if (timedOut) parts.push(`timed_out: true`)
        if (stdout.trim().length > 0) parts.push(`stdout:\n${truncateOutput(stdout.trimEnd())}`)
        if (stderr.trim().length > 0) parts.push(`stderr:\n${truncateOutput(stderr.trimEnd())}`)
        resolve(parts.join("\n"))
      })
    })
  },
})
