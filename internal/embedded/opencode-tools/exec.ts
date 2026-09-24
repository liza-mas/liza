// __BRAND_NAME_UPPER__ MANAGED FILE: OpenCode exec compatibility tool. Safe for __BRAND_NAME_TITLE__ to overwrite.
import { Buffer } from "node:buffer"
import { spawn } from "node:child_process"
import process from "node:process"
import { tool } from "@opencode-ai/plugin"

const DEFAULT_TIMEOUT_MS = 120_000
const FORCE_KILL_DELAY_MS = 2_000
const EXIT_GRACE_MS = 1_000
const FILTER_OUTPUT_LIMIT_BYTES = 65_536
// Bounds a runaway command (`yes`, `tail -f`) before it can grow memory; the
// remainder is drained and the result marked truncated, as the engine's own
// capture does.
const CAPTURE_LIMIT_BYTES = 1_048_576
const FILTER_TIMEOUT_MS = 30_000

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined
}

function formatOutput(label: string, value: string): string | undefined {
  if (value.length === 0) return undefined
  return `${label}:\n${value}`
}

// Raw tool bytes stay only in memory until the shared sanitizer processes them.
async function budgetOutput(payload: Record<string, unknown>, projectRoot: string): Promise<string> {
  return await new Promise<string>((resolve) => {
    const failure = "tool_result_error: sanitized artifact processing failed; raw output withheld"
    const filter = spawn("__BRAND_BINARY_NAME__", ["--project-root", projectRoot, "tool-result", "filter", "--json-input"], {
      cwd: projectRoot,
      env: process.env,
      stdio: ["pipe", "pipe", "ignore"],
    })
    const chunks: Buffer[] = []
    let total = 0
    let failed = false
    const timer = setTimeout(() => { failed = true; filter.kill("SIGKILL"); resolve(failure) }, FILTER_TIMEOUT_MS)
    filter.stdout?.on("data", (chunk) => {
      total += chunk.length
      if (total > FILTER_OUTPUT_LIMIT_BYTES) { failed = true; filter.kill("SIGKILL"); return }
      chunks.push(Buffer.from(chunk))
    })
    filter.stdin?.on("error", () => { failed = true })
    filter.on("error", () => { clearTimeout(timer); resolve(failure) })
    filter.on("close", (code) => {
      clearTimeout(timer)
      resolve(code === 0 && !failed ? Buffer.concat(chunks).toString("utf8") : failure)
    })
    filter.stdin?.end(JSON.stringify(payload))
  })
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
    "Run a shell command for trusted __BRAND_NAME_TITLE__ bridge work. The cmd string is executed through the system shell and is not safe for less-trusted contexts. Prefer this exec tool for shell and file operations instead of built-in bash/read/write tools. Omit optional fields when they are not needed; null is tolerated and treated as omitted. Do not repeat the same successful command. After a successful command, inspect the result and move to the next __BRAND_NAME_TITLE__ protocol step.",
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
      .describe("Timeout in milliseconds. Omit for __BRAND_NAME_TITLE__'s default timeout."),
  },
  async execute(args, context) {
    const cwd = args.workdir ?? defaultWorkdir(context)
    const timeoutMs =
      typeof args.timeout_ms === "number" && Number.isFinite(args.timeout_ms) && args.timeout_ms > 0
        ? args.timeout_ms
        : DEFAULT_TIMEOUT_MS

    const result = await new Promise<{ content: string; exit_code: number; truncated: boolean }>((resolve) => {
      const stdout: Buffer[] = []
      const stderr: Buffer[] = []
      let captured = 0
      let dropped = 0
      const capture = (target: Buffer[], chunk: unknown) => {
        const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk))
        const kept = bytes.subarray(0, Math.max(CAPTURE_LIMIT_BYTES - captured, 0))
        if (kept.length) target.push(Buffer.from(kept))
        captured += kept.length
        dropped += bytes.length - kept.length
      }
      let timedOut = false
      let settled = false
      let forceKill: ReturnType<typeof setTimeout> | undefined
      let exitGrace: ReturnType<typeof setTimeout> | undefined

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

      child.stdout?.on("data", (chunk) => capture(stdout, chunk))
      child.stderr?.on("data", (chunk) => capture(stderr, chunk))

      child.on("error", (error) => {
        clearTimeout(timeout)
        if (forceKill) clearTimeout(forceKill)
        if (settled) return
        settled = true
        resolve({ content: `exit_code: 127\nerror: ${error.message}`, exit_code: 127, truncated: false })
      })

      // `close` waits for every holder of the output pipes, including a
      // background job the command started (`npm run dev &`). Once the command
      // itself exits, give its output a short grace period, then stop reading,
      // as the engine's own runner does.
      const finish = (code: number | null, signal: NodeJS.Signals | null, detached: boolean) => {
        if (settled) return
        settled = true
        clearTimeout(timeout)
        if (forceKill) clearTimeout(forceKill)
        if (exitGrace) clearTimeout(exitGrace)
        const parts = [`exit_code: ${code ?? -1}`]
        if (signal) parts.push(`signal: ${signal}`)
        if (timedOut) parts.push(`timed_out: true`)
        if (detached) parts.push(`background_output_detached: true`)
        if (dropped > 0) parts.push(`capture_truncated_bytes: ${dropped}`)
        const stdoutPart = formatOutput("stdout", Buffer.concat(stdout).toString("utf8"))
        const stderrPart = formatOutput("stderr", Buffer.concat(stderr).toString("utf8"))
        if (stdoutPart) parts.push(stdoutPart)
        if (stderrPart) parts.push(stderrPart)
        resolve({ content: parts.join("\n"), exit_code: code ?? -1, truncated: dropped > 0 || timedOut || detached })
      }

      child.on("exit", (code, signal) => {
        // The command is done; a timeout must not kill its background job.
        clearTimeout(timeout)
        exitGrace = setTimeout(() => {
          child.stdout?.destroy()
          child.stderr?.destroy()
          finish(code, signal, true)
        }, EXIT_GRACE_MS)
      })

      child.on("close", (code, signal) => finish(code, signal, false))
    })
    const ctx = context as unknown as Record<string, unknown>
    return await budgetOutput({
      ...result,
      tool: "exec",
      command: args.cmd,
      command_class: "shell",
      role: stringValue(ctx.agent) ?? "",
      agent_id: process.env["__BRAND_ENV_PREFIX__" + "_AGENT_ID"] ?? "",
      task_id: process.env["__BRAND_ENV_PREFIX__" + "_TASK_ID"] ?? "",
      session_id: stringValue(ctx.sessionID) ?? "",
    }, defaultWorkdir(context))
  },
})
