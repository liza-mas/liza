#!/usr/bin/env python3
"""Analyze §BRAND_NAME_TITLE§ agent log files for context usage patterns.

Reads NDJSON log files produced by `claude --verbose --output-format stream-json`
and prints a human-readable report of token usage, content breakdown, and cost.

Two log formats are supported:
  - Rich (Format A): first event type is "system". Per-API-call token breakdown.
  - Sparse (Format B): first event type is "thread.started". Aggregate usage only.

Usage:
    python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-log.py \
        §BRAND_PROJECT_DIRNAME§/agent-outputs/orchestrator-*.txt
    python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-log.py \
        §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path

BRAND_BINARY_NAME = "§BRAND_BINARY_NAME§"
BRAND_MCP_SERVER = "§BRAND_NAME_LOWER§"
BRAND_NAME_TITLE = "§BRAND_NAME_TITLE§"
SECRET_WORD_PREFIXES = (BRAND_NAME_TITLE, "Secret")
_BREADCRUMB_SEARCH_TEXT_BLOCK_LIMIT = 5
_AGGREGATE_USAGE_FIELDS = (
    "input_tokens",
    "cache_creation_input_tokens",
    "cache_read_input_tokens",
    "output_tokens",
)
_PROVIDER_BACKGROUNDING_RE = re.compile(
    r"\ACommand (?:did not complete within its (?P<duration>\d+(?:\.\d+)?)s timeout|timed out) "
    r"and was moved to the background(?=$|[\s.!?:;])"
)

# ---------------------------------------------------------------------------
# Data classes
# ---------------------------------------------------------------------------


@dataclass
class SessionMeta:
    file: str = ""
    format: str = ""  # "rich" or "sparse"
    provider: str = ""
    model: str = ""
    session_id: str = ""
    duration_ms: int = 0
    num_turns: int = 0
    context_window: int = 0
    max_output_tokens: int = 0


@dataclass
class TurnUsage:
    """Token usage for a single API call (rich format only)."""

    message_id: str = ""
    input_tokens: int = 0
    cache_creation_input_tokens: int = 0
    cache_read_input_tokens: int = 0
    output_tokens: int = 0
    duration_ms: int = 0

    @property
    def total_input(self) -> int:
        return self.input_tokens + self.cache_creation_input_tokens + self.cache_read_input_tokens


@dataclass
class TurnAction:
    """A single tool invocation correlated with its result."""

    turn_num: int = 0
    tool_name: str = ""
    detail: str = ""
    result_chars: int = 0
    is_error: bool = False
    result_preview: str = ""
    result_hash: str = ""
    duration_ms: int = 0


@dataclass
class ContentItem:
    """A single content item from the log."""

    item_type: str = ""  # reasoning, agent_message, command_execution, etc.
    item_id: str = ""
    chars: int = 0
    preview: str = ""


@dataclass
class EmptyTurn:
    """A turn/unit where the assistant produced no meaningful text or tool call."""

    turn_num: int = 0
    item_type: str = ""
    detail: str = ""
    preview: str = ""


@dataclass
class SessionReport:
    meta: SessionMeta = field(default_factory=SessionMeta)
    # Aggregate usage (both formats)
    total_input_tokens: int = 0
    total_cache_creation: int = 0
    total_cache_read: int = 0
    total_output_tokens: int = 0
    total_cost_usd: float = 0.0
    aggregate_usage_source: str = "unknown"
    # Per-turn (rich only)
    turns: list[TurnUsage] = field(default_factory=list)
    # Content items (both formats)
    items: list[ContentItem] = field(default_factory=list)
    # Tool/text/empty accounting across assistant API turns
    turn_units: int = 0
    tool_turn_units: int = 0
    empty_turns: list[EmptyTurn] = field(default_factory=list)
    # Tool call frequency (both formats)
    tool_calls: dict[str, int] = field(default_factory=dict)
    # Turn timeline (rich only)
    actions: list[TurnAction] = field(default_factory=list)
    # MCP server status (rich only)
    mcp_servers: list[dict[str, str]] = field(default_factory=list)
    # Skill invocations (both formats)
    skill_invocations: dict[str, int] = field(default_factory=dict)
    # Secret words (lines starting with the brand name or "Secret" in early assistant content)
    secret_words_lines: list[str] = field(default_factory=list)
    breadcrumb_applicability: str = "unknown"
    # Provider-forced foreground timeout/backgrounding events
    operational_frictions: list[OperationalFriction] = field(default_factory=list)


@dataclass
class PermissionFriction:
    """A permission, policy, or command-shape block surfaced by agent tooling."""

    category: str
    log_file: str
    role: str
    tool_name: str
    detail: str
    result_preview: str


@dataclass
class OperationalFriction:
    """A provider-forced foreground timeout/backgrounding event."""

    category: str
    log_file: str
    role: str
    tool_name: str
    command_preview: str
    duration_ms: int
    result_preview: str


# ---------------------------------------------------------------------------
# Format detection
# ---------------------------------------------------------------------------


def _parse_ts_ms(ts_str: str) -> int:
    """Parse an ISO timestamp like '2026-04-15T00:25:57.276Z' to milliseconds."""
    if not ts_str:
        return 0
    try:
        # Python 3.7+ supports fromisoformat, but Z needs to be handled
        dt = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
        return int(dt.timestamp() * 1000)
    except (ValueError, TypeError):
        return 0


def detect_format(lines: list[str]) -> str:
    """Return 'rich' or 'sparse' based on the first structural JSON event."""
    for line in lines[:10]:
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue
        event_type = obj.get("type", "")
        if event_type == "system":
            return "rich"
        if event_type == "thread.started":
            return "sparse"
    return "unknown"


# ---------------------------------------------------------------------------
# Skill detection helpers
# ---------------------------------------------------------------------------

# Commands where exit code 1 means "no matches/differences found", not an error.
# Real errors use exit code >= 2.
_BENIGN_EXIT1_COMMANDS = frozenset({"rg", "grep", "egrep", "fgrep", "ag", "ack", "diff"})


def _benign_exit_command_name(cmd_name: str) -> str:
    """Return the command whose exit semantics should be used.

    RTK is a wrapper; `rtk rg` should inherit `rg`'s normal exit-1 behavior.
    """
    parts = cmd_name.split()
    if parts and parts[0] == "rtk" and len(parts) > 1:
        return parts[1]
    return parts[0] if parts else ""


def _is_benign_exit(cmd_name: str, exit_code: int) -> bool:
    """Return True if exit code is a normal non-error signal for the command.

    Some tools use exit code 1 for "no matches found" (grep, rg) or
    "differences found" (diff), which is expected behavior, not an error.
    """
    return exit_code == 1 and _benign_exit_command_name(cmd_name) in _BENIGN_EXIT1_COMMANDS


def _hash_result(text: str) -> str:
    """Return a stable digest for exact duplicate result detection."""
    return hashlib.sha256(text.encode("utf-8", errors="replace")).hexdigest() if text else ""


def _tool_result_text(block: dict) -> str:
    """Normalize a rich tool-result payload to one text value."""
    content = block.get("content", "")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(part.get("text", "") if isinstance(part, dict) else str(part) for part in content)
    return str(content) if content is not None else ""


def _operational_friction_details(result_text: str) -> tuple[str, int]:
    """Return the provider friction category and message-reported duration."""
    normalized = " ".join(result_text.split())
    match = _PROVIDER_BACKGROUNDING_RE.match(normalized)
    if not match:
        return "", 0
    duration = match.group("duration")
    try:
        duration_ms = round(float(duration) * 1000) if duration else 0
    except (OverflowError, ValueError):
        duration_ms = 0
    return "provider foreground timeout/backgrounding", duration_ms


def _breadcrumb_applicability(report: SessionReport) -> str:
    """Return whether initialization breadcrumbs apply to the detected agent."""
    provider = report.meta.provider.lower()
    # Claude stream-json is the only supported rich format that omits provider
    # identity. Explicit providers always win over model branding.
    if provider == "claude" or (report.meta.format == "rich" and not provider):
        return "not required"
    if provider or report.meta.format == "sparse":
        return "required"
    return "unknown"


def _extract_secret_words_lines(text: str) -> list[str]:
    """Extract brand/secret-word lines from the first 30 lines.

    Strips leading markdown formatting (bold, italic, heading markers) before matching.
    """
    if not text:
        return []
    result = []
    for line in text.strip().splitlines()[:30]:
        stripped = line.strip().strip("*_#").strip()
        if stripped.startswith(SECRET_WORD_PREFIXES):
            result.append(stripped)
    return result


_SKILL_PATH_RE = re.compile(r"skills/([a-z][a-z0-9_-]*)/SKILL\.md$")


def _extract_skill_from_path(path: str) -> str:
    """Extract skill name from a path like '.../skills/code-review/SKILL.md'.

    Returns the skill name or empty string if not a skill path.
    """
    m = _SKILL_PATH_RE.search(path)
    return m.group(1) if m else ""


def _parse_mcp_tool_name(tool_name: str) -> tuple[str, str] | None:
    """Parse MCP server and tool from a tool name.

    Rich format: mcp__<server>__<tool> → (server, tool)
    Sparse format: <server>/<tool> → (server, tool)
    Returns None if not an MCP tool.
    """
    if tool_name.startswith("mcp__"):
        parts = tool_name.split("__", 2)
        if len(parts) == 3:
            return (parts[1], parts[2])
        return None
    if "/" in tool_name and tool_name[0].isalpha():
        server, _, tool = tool_name.partition("/")
        if re.fullmatch(r"[A-Za-z][A-Za-z0-9_.-]*", server) and tool:
            return (server, tool)
    return None


def _permission_friction_category(action: TurnAction) -> str:
    """Classify command/tool permission friction separately from normal failures."""
    if not action.is_error:
        return ""

    text = action.result_preview
    detail = action.detail

    if "This command requires approval" in text:
        return "generic approval-required command"
    if "This Bash command contains multiple operations" in text:
        return "multi-operation command"
    if "Commands that change directories and write via output redirection" in text:
        return "cd + output redirection"
    if "Commands that change directories and perform write operations" in text:
        return "cd + write-like operation"
    if "This command changes directory before running git" in text:
        return "cd before git"
    if "Blocked: sleep" in text:
        return "sleep/polling block"
    if "env with -C flag cannot be statically analyzed" in text:
        return "env -C unsupported shape"
    if "find contains unquoted glob" in text:
        return "find unquoted glob"
    if "This command uses shell operators" in text:
        return "shell operators require approval"
    if "Unrecognized redirect shape" in text:
        return "unrecognized redirect shape"
    if "sed target contains command-substitution" in text:
        return "runtime-determined sed target"
    if "PreToolUse:Bash hook error" in text:
        return "pre-tool hook block"
    if "must be run from project root" in text and f"{BRAND_BINARY_NAME}:" in text:
        return f"{BRAND_MCP_SERVER} project-root mismatch"
    if "ls in '" in text and "was blocked" in text:
        return "filesystem allowlist block"

    if "Contains shell syntax" in text:
        if "<<" in detail:
            return "shell syntax: heredoc/inline payload"
        if "$(" in detail:
            return "shell syntax: command substitution"
        return "shell syntax"
    if "Contains simple_expansion" in text:
        return "shell expansion: simple"
    if "Contains command_substitution" in text:
        return "shell expansion: command substitution"
    if "Contains subshell" in text:
        return "shell expansion: subshell"
    if "Contains expansion" in text:
        return "shell expansion"
    if "Contains brace with quote character" in text:
        return "shell expansion: brace/quote"

    return ""


def _permission_frictions_for_report(report: SessionReport) -> list[PermissionFriction]:
    """Return permission/policy frictions detected in a parsed report."""
    role = _role_from_log_path(report.meta.file) if report.meta.file else ""
    log_file = Path(report.meta.file).name if report.meta.file else ""
    frictions: list[PermissionFriction] = []
    for action in report.actions:
        category = _permission_friction_category(action)
        if not category:
            continue
        frictions.append(
            PermissionFriction(
                category=category,
                log_file=log_file,
                role=role,
                tool_name=action.tool_name,
                detail=action.detail,
                result_preview=action.result_preview,
            )
        )
    return frictions


# ---------------------------------------------------------------------------
# Rich format parser (Format A)
# ---------------------------------------------------------------------------


def _measure_content_block(block: dict) -> ContentItem:
    """Extract a ContentItem from a rich-format content block."""
    block_type = block.get("type", "unknown")
    text = ""
    if block_type == "thinking":
        text = block.get("thinking", "")
    elif block_type == "text":
        text = block.get("text", "")
    elif block_type == "tool_use":
        text = json.dumps(block.get("input", {}))
    elif block_type == "tool_result":
        text = _tool_result_text(block)
    return ContentItem(
        item_type=block_type,
        chars=len(text),
        preview=text[:120].replace("\n", " "),
    )


def _extract_tool_detail(name: str, input_data: dict) -> str:
    """Extract a short detail string from a tool_use input."""
    if name == "Bash":
        cmd = input_data.get("command", "")
        # First meaningful token
        return cmd.split("\n")[0][:80] if cmd else ""
    if name in ("Read", "Write"):
        return input_data.get("file_path", "")[:80]
    if name == "Edit":
        return input_data.get("file_path", "")[:80]
    if name in ("Glob", "Grep"):
        pat = input_data.get("pattern", "")
        path = input_data.get("path", "")
        return f"{pat} in {path}" if path else pat
    if name == "Skill":
        return input_data.get("skill", "")[:80]
    if name == "Task":
        return input_data.get("description", "")[:80]
    if name == "TaskCreate":
        return input_data.get("subject", "")[:80]
    if name == "TaskUpdate":
        return f"#{input_data.get('taskId', '')} → {input_data.get('status', '')}"
    if name.startswith("mcp__"):
        # MCP tool — show first string-valued input
        for v in input_data.values():
            if isinstance(v, str) and v:
                return v[:80]
    # Fallback: first string value
    for v in input_data.values():
        if isinstance(v, str) and v:
            return v[:60]
    return ""


def parse_rich(lines: list[str]) -> SessionReport:
    """Parse a rich-format (Format A) log file."""
    report = SessionReport()
    report.meta.format = "rich"

    seen_message_ids: dict[str, TurnUsage] = {}
    # For correlating tool_use → tool_result
    pending_tool_uses: dict[str, tuple[str, str, int]] = {}  # id → (name, detail, turn_num)
    turn_has_tool: dict[str, bool] = {}
    turn_has_meaningful_text: dict[str, bool] = {}
    turn_empty_detail: dict[str, EmptyTurn] = {}
    turn_order: list[str] = []
    first_assistant_texts: list[str] = []
    first_assistant_captured = False
    last_ts_ms = 0
    current_turn_duration_ms = 0
    terminal_usage: dict | None = None

    for line in lines:
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue

        event_type = obj.get("type", "")

        ts_str = obj.get("timestamp")
        if ts_str:
            ts_ms = _parse_ts_ms(ts_str)
            if ts_ms > 0:
                if last_ts_ms > 0 and report.turns:
                    delta = ts_ms - last_ts_ms
                    # Attribute the time since last timestamp to the *current* turn
                    report.turns[-1].duration_ms += delta
                    current_turn_duration_ms += delta
                last_ts_ms = ts_ms

        if event_type == "system":
            if obj.get("session_id"):
                report.meta.session_id = obj.get("session_id", "")
            if obj.get("provider"):
                report.meta.provider = obj.get("provider", "")
            if obj.get("model"):
                report.meta.model = obj.get("model", "")
            elif obj.get("provider") and not report.meta.model:
                # Display fallback only. Applicability uses provider/format
                # identity and never treats a model label as an agent identity.
                report.meta.model = obj.get("provider", "")
            if obj.get("mcp_servers"):
                report.mcp_servers = [
                    {
                        "name": srv.get("name", ""),
                        "status": srv.get("status", ""),
                    }
                    for srv in obj.get("mcp_servers", [])
                ]

        elif event_type == "assistant":
            msg = obj.get("message", {})
            msg_id = msg.get("id", "")
            usage = msg.get("usage", {})
            # Dedup: only count usage once per message.id
            if msg_id and msg_id not in seen_message_ids:
                turn = TurnUsage(
                    message_id=msg_id,
                    input_tokens=usage.get("input_tokens", 0),
                    cache_creation_input_tokens=usage.get("cache_creation_input_tokens", 0),
                    cache_read_input_tokens=usage.get("cache_read_input_tokens", 0),
                    output_tokens=usage.get("output_tokens", 0),
                )
                seen_message_ids[msg_id] = turn
                report.turns.append(turn)
                turn_order.append(msg_id)
                turn_has_tool[msg_id] = False
                turn_has_meaningful_text[msg_id] = False
                turn_empty_detail[msg_id] = EmptyTurn(
                    turn_num=len(report.turns),
                    item_type="assistant_message",
                    detail=msg_id,
                    preview="",
                )
                current_turn_duration_ms = 0  # reset for new turn

            # Extract content items and tool call names
            for block in msg.get("content", []):
                block_type = block.get("type", "unknown")
                item = _measure_content_block(block)
                if item.chars > 0:
                    report.items.append(item)
                    raw_text = block.get("text", "") if block_type == "text" else block.get("thinking", "")
                    if msg_id and block_type in ("text", "thinking") and raw_text.strip():
                        turn_has_meaningful_text[msg_id] = True
                    if msg_id and not turn_empty_detail.get(msg_id, EmptyTurn()).preview:
                        turn_empty_detail[msg_id] = EmptyTurn(
                            turn_num=turn_empty_detail.get(msg_id, EmptyTurn()).turn_num or len(report.turns),
                            item_type=block_type,
                            detail=msg_id,
                            preview=item.preview,
                        )
                # Tool-only envelopes may precede the initial contract-reading text.
                if not first_assistant_captured and block.get("type") == "text":
                    raw = block.get("text", "")
                    if raw.strip():
                        first_assistant_texts.append(raw)
                        if (
                            _extract_secret_words_lines(raw)
                            or len(first_assistant_texts) >= _BREADCRUMB_SEARCH_TEXT_BLOCK_LIMIT
                        ):
                            first_assistant_captured = True
                if block.get("type") == "tool_use":
                    if msg_id:
                        turn_has_tool[msg_id] = True
                    name = block.get("name", "unknown")
                    input_data = block.get("input", {})
                    tool_call_name = name
                    if name == "Bash":
                        tool_call_name = _extract_command_name(input_data.get("command", "")) or name
                    report.tool_calls[tool_call_name] = report.tool_calls.get(tool_call_name, 0) + 1
                    # Track skill invocations (Skill tool or direct file read)
                    if name == "Skill":
                        skill_name = input_data.get("skill", "")
                        if skill_name:
                            report.skill_invocations[skill_name] = report.skill_invocations.get(skill_name, 0) + 1
                    elif name in ("Read", "mcp__filesystem__read_text_file", "mcp__filesystem__read_file"):
                        path = input_data.get("file_path", "") or input_data.get("path", "")
                        skill_name = _extract_skill_from_path(path)
                        if skill_name:
                            report.skill_invocations[skill_name] = report.skill_invocations.get(skill_name, 0) + 1
                    # Track for timeline correlation
                    tool_id = block.get("id", "")
                    if tool_id:
                        detail = _extract_tool_detail(name, block.get("input", {}))
                        turn_num = len(report.turns)  # current turn index (1-based after append)
                        pending_tool_uses[tool_id] = (name, detail, turn_num)

        elif event_type == "user":
            msg = obj.get("message", {})
            for content_part in msg.get("content", []):
                if not isinstance(content_part, dict):
                    continue

                item = _measure_content_block(content_part)
                if item.chars > 0:
                    report.items.append(item)
                if content_part.get("type") != "tool_result":
                    continue

                tool_use_id = content_part.get("tool_use_id", "")
                if not tool_use_id or tool_use_id not in pending_tool_uses:
                    continue

                name, detail, turn_num = pending_tool_uses.pop(tool_use_id)
                result_text = _tool_result_text(content_part)
                result_preview = result_text[:120].replace("\n", " ")
                is_error = bool(content_part.get("is_error"))
                # For Bash commands, check if exit code 1 is benign
                # (e.g., rg/grep with no matches produce empty output)
                effective_error = is_error
                if is_error and name == "Bash" and detail:
                    bash_cmd = _extract_command_name(detail)
                    if _is_benign_exit(bash_cmd, 1) and not result_preview.strip():
                        effective_error = False
                report.actions.append(
                    TurnAction(
                        turn_num=turn_num,
                        tool_name=name,
                        detail=detail,
                        result_chars=len(result_text),
                        is_error=effective_error,
                        result_preview=result_preview,
                        result_hash=_hash_result(result_text),
                        duration_ms=current_turn_duration_ms,
                    )
                )
                friction_category, reported_duration_ms = _operational_friction_details(result_text)
                if friction_category:
                    report.operational_frictions.append(
                        OperationalFriction(
                            category=friction_category,
                            log_file="",
                            role="",
                            tool_name=name,
                            command_preview=detail[:120],
                            duration_ms=current_turn_duration_ms or reported_duration_ms,
                            result_preview=result_preview,
                        )
                    )

        elif event_type == "result":
            report.meta.duration_ms = obj.get("duration_ms", 0)
            report.meta.num_turns = obj.get("num_turns", 0)
            report.total_cost_usd = obj.get("total_cost_usd", 0.0)
            usage = obj.get("usage")
            if isinstance(usage, dict):
                token_values = [usage.get(field) for field in _AGGREGATE_USAGE_FIELDS]
                if all(type(value) is int and value >= 0 for value in token_values):
                    terminal_usage = usage
            model_usage = obj.get("modelUsage", {})
            for model_name, mu in model_usage.items():
                report.meta.context_window = mu.get("contextWindow", 0)
                report.meta.max_output_tokens = mu.get("maxOutputTokens", 0)

    if terminal_usage is not None:
        report.total_input_tokens = terminal_usage.get("input_tokens", 0)
        report.total_cache_creation = terminal_usage.get("cache_creation_input_tokens", 0)
        report.total_cache_read = terminal_usage.get("cache_read_input_tokens", 0)
        report.total_output_tokens = terminal_usage.get("output_tokens", 0)
        report.aggregate_usage_source = "terminal"
    else:
        for turn in report.turns:
            report.total_input_tokens += turn.input_tokens
            report.total_cache_creation += turn.cache_creation_input_tokens
            report.total_cache_read += turn.cache_read_input_tokens
            report.total_output_tokens += turn.output_tokens
        report.aggregate_usage_source = "envelope-partial"

    report.secret_words_lines = _extract_secret_words_lines("\n".join(first_assistant_texts))
    report.turn_units = len(turn_order)
    report.tool_turn_units = sum(1 for msg_id in turn_order if turn_has_tool.get(msg_id))
    report.empty_turns = [
        turn_empty_detail[msg_id]
        for msg_id in turn_order
        if not turn_has_tool.get(msg_id) and not turn_has_meaningful_text.get(msg_id)
    ]
    report.breadcrumb_applicability = _breadcrumb_applicability(report)

    return report


# ---------------------------------------------------------------------------
# Sparse format parser (Format B)
# ---------------------------------------------------------------------------


def _measure_sparse_item(item: dict) -> ContentItem:
    """Extract a ContentItem from a sparse-format item."""
    item_type = item.get("type", "unknown")
    item_id = item.get("id", "")
    text = ""

    if item_type == "command_execution":
        text = item.get("aggregated_output", "") or ""
        cmd = item.get("command", "")
        preview = f"[{cmd[:80]}] {text[:40]}"
    elif item_type == "reasoning":
        text = item.get("text", "")
        preview = text[:120]
    elif item_type == "agent_message":
        text = item.get("text", "")
        preview = text[:120]
    elif item_type == "file_change":
        changes = item.get("changes", [])
        text = json.dumps(changes)
        paths = [c.get("path", "") for c in changes]
        preview = ", ".join(paths)[:120]
    elif item_type == "mcp_tool_call":
        result = item.get("result", {})
        args = item.get("arguments", "")
        server = item.get("server", "")
        tool = item.get("tool", "")
        result_text = json.dumps(result) if isinstance(result, dict) else str(result)
        args_text = json.dumps(args) if isinstance(args, dict) else str(args)
        text = args_text + result_text
        preview = f"[{server}/{tool}] {result_text[:80]}"
    else:
        text = json.dumps(item)
        preview = text[:120]

    return ContentItem(
        item_type=item_type,
        item_id=item_id,
        chars=len(text),
        preview=preview.replace("\n", " "),
    )


def _extract_command_name(cmd: str) -> str:
    """Extract the underlying executable used to aggregate shell commands."""
    # Strip shell wrappers like `/usr/bin/zsh -lc "..."`
    for prefix in ("/usr/bin/zsh -lc ", "/bin/bash -lc ", "/bin/sh -c "):
        if cmd.startswith(prefix):
            cmd = cmd[len(prefix) :].strip().strip("'\"")
            # Skip recognized setup statements before the first substantive command.
            segments = re.split(r"\s*(?:&&|;)\s*", cmd)
            for segment in segments:
                segment = segment.strip()
                parts = segment.split()
                if parts and parts[0] not in ("set", "echo", "if", "cd"):
                    cmd = segment
                    break
            break

    parts = cmd.split()
    if parts and parts[0] == "rtk":
        parts = parts[1:]
    return parts[0].rsplit("/", 1)[-1] if parts else cmd


def parse_sparse(lines: list[str]) -> SessionReport:
    """Parse a sparse-format (Format B) log file."""
    report = SessionReport()
    report.meta.format = "sparse"
    first_assistant_texts: list[str] = []
    first_assistant_captured = False
    current_turn_open = False
    current_turn_num = 0
    current_turn_has_tool = False
    current_turn_has_meaningful_text = False
    current_turn_empty_detail: EmptyTurn | None = None
    sparse_outer_turns = 0

    def start_turn() -> None:
        nonlocal current_turn_empty_detail
        nonlocal current_turn_has_meaningful_text
        nonlocal current_turn_has_tool
        nonlocal current_turn_num
        nonlocal current_turn_open

        current_turn_num += 1
        current_turn_open = True
        current_turn_has_tool = False
        current_turn_has_meaningful_text = False
        current_turn_empty_detail = None

    def finalize_turn(failed_msg: str = "") -> None:
        nonlocal current_turn_open

        if not current_turn_open:
            return

        report.turn_units += 1
        if current_turn_has_tool:
            report.tool_turn_units += 1
        elif not current_turn_has_meaningful_text:
            detail = current_turn_empty_detail
            if detail is None:
                detail = EmptyTurn(
                    turn_num=current_turn_num,
                    item_type="turn.failed" if failed_msg else "turn",
                    detail="failed turn without completed actions" if failed_msg else "",
                    preview=failed_msg[:120].replace("\n", " "),
                )
            report.empty_turns.append(detail)
        current_turn_open = False

    for line in lines:
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue

        event_type = obj.get("type", "")

        if event_type == "thread.started":
            report.meta.session_id = obj.get("thread_id", "")
            report.meta.provider = obj.get("provider", "")
            # Display fallback only; provider remains the applicability identity.
            report.meta.model = obj.get("model", "") or obj.get("provider", "")

        elif event_type == "turn.started":
            if current_turn_open:
                finalize_turn()
            sparse_outer_turns += 1
            start_turn()

        elif event_type == "item.completed":
            item = obj.get("item", {})
            # Only count completed items (skip in_progress starts)
            if item.get("status") in ("completed", "failed", None):
                if not current_turn_open:
                    start_turn()
                ci = _measure_sparse_item(item)
                if ci.chars > 0:
                    report.items.append(ci)
                # Empty message envelopes do not define the initial assistant content.
                if item.get("type") == "agent_message":
                    raw = item.get("text", "")
                    if not first_assistant_captured and raw.strip():
                        first_assistant_texts.append(raw)
                        if (
                            _extract_secret_words_lines(raw)
                            or len(first_assistant_texts) >= _BREADCRUMB_SEARCH_TEXT_BLOCK_LIMIT
                        ):
                            first_assistant_captured = True
                # Track tool calls and build timeline actions
                itype = item.get("type", "")
                raw_text = item.get("text", "") if itype in ("agent_message", "reasoning") else ""
                if raw_text.strip():
                    current_turn_has_meaningful_text = True
                elif itype not in ("command_execution", "mcp_tool_call", "file_change"):
                    if current_turn_empty_detail is None:
                        current_turn_empty_detail = EmptyTurn(
                            turn_num=current_turn_num,
                            item_type=itype or "unknown",
                            detail=item.get("id", ""),
                            preview=ci.preview,
                        )
                if itype == "command_execution":
                    current_turn_has_tool = True
                    cmd = item.get("command", "")
                    # Strip shell wrapper for detail
                    detail = cmd
                    for prefix in ("/usr/bin/zsh -lc ", "/bin/bash -lc ", "/bin/sh -c "):
                        if cmd.startswith(prefix):
                            detail = cmd[len(prefix) :].strip().strip("'\"")
                            break
                    name = _extract_command_name(cmd)
                    report.tool_calls[name] = report.tool_calls.get(name, 0) + 1
                    output = item.get("aggregated_output", "") or ""
                    exit_code = item.get("exit_code", 0)
                    report.actions.append(
                        TurnAction(
                            turn_num=current_turn_num,
                            tool_name=name,
                            detail=detail[:80],
                            result_chars=len(output),
                            is_error=exit_code != 0 and not _is_benign_exit(name, exit_code),
                            result_preview=output[:120].replace("\n", " "),
                            result_hash=_hash_result(output),
                        )
                    )
                elif itype == "mcp_tool_call":
                    current_turn_has_tool = True
                    server = item.get("server", "")
                    tool = item.get("tool", "")
                    name = f"{server}/{tool}" if server else tool
                    report.tool_calls[name] = report.tool_calls.get(name, 0) + 1
                    args = item.get("arguments", {})
                    detail = ""
                    if isinstance(args, dict):
                        for v in args.values():
                            if isinstance(v, str) and v:
                                detail = v[:80]
                                break
                    # Detect skill file reads: read_text_file of skills/<name>/SKILL.md
                    if tool in ("read_text_file", "read_file") and isinstance(args, dict):
                        path = args.get("path", "")
                        skill_name = _extract_skill_from_path(path)
                        if skill_name:
                            report.skill_invocations[skill_name] = report.skill_invocations.get(skill_name, 0) + 1
                    result = item.get("result", {})
                    result_text = json.dumps(result) if isinstance(result, dict) else str(result)
                    report.actions.append(
                        TurnAction(
                            turn_num=current_turn_num,
                            tool_name=name,
                            detail=detail,
                            result_chars=len(result_text),
                            is_error=item.get("status") == "failed",
                            result_preview=result_text[:120].replace("\n", " "),
                            result_hash=_hash_result(result_text),
                        )
                    )
                elif itype == "file_change":
                    current_turn_has_tool = True
                    changes = item.get("changes", [])
                    paths = [c.get("path", "").rsplit("/", 1)[-1] for c in changes]
                    report.actions.append(
                        TurnAction(
                            turn_num=current_turn_num,
                            tool_name="file_change",
                            detail=", ".join(paths)[:80],
                            result_chars=0,
                            is_error=False,
                            result_preview="",
                        )
                    )

        elif event_type == "turn.failed":
            if not current_turn_open:
                start_turn()
            err = obj.get("error", {})
            msg = err.get("message", "") if isinstance(err, dict) else obj.get("message", "")
            finalize_turn(msg)

        elif event_type == "turn.completed":
            if current_turn_open:
                finalize_turn()
            usage = obj.get("usage", {})
            total_in = usage.get("input_tokens", 0)
            cached = usage.get("cached_input_tokens", 0)
            # In sparse format, input_tokens is the grand total (fresh + cached).
            # Decompose into fresh vs cached to match the rich-format model.
            report.total_input_tokens = total_in - cached
            report.total_cache_read = cached
            report.total_output_tokens = usage.get("output_tokens", 0)
            report.total_cache_creation = 0  # not available in sparse format
            report.aggregate_usage_source = "terminal"

    if current_turn_open:
        finalize_turn()

    report.meta.num_turns = sparse_outer_turns

    report.secret_words_lines = _extract_secret_words_lines("\n".join(first_assistant_texts))
    report.breadcrumb_applicability = _breadcrumb_applicability(report)

    return report


# ---------------------------------------------------------------------------
# Rendering
# ---------------------------------------------------------------------------


def _fmt_tokens(n: int) -> str:
    """Format token count with K/M suffix."""
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n / 1_000:.1f}K"
    return str(n)


def _est_tokens(chars: int) -> int:
    """Rough token estimate: ~4 chars per token."""
    return chars // 4


def render_header(report: SessionReport) -> str:
    lines = [
        "=" * 72,
        "SESSION HEADER",
        "=" * 72,
        f"  File:       {report.meta.file}",
        f"  Format:     {report.meta.format}",
        f"  Model:      {report.meta.model or 'unknown'}",
        f"  Session:    {report.meta.session_id or 'unknown'}",
    ]
    if report.meta.duration_ms:
        secs = report.meta.duration_ms / 1000
        lines.append(f"  Duration:   {secs:.1f}s")
    if report.meta.num_turns:
        lines.append(f"  Turns:      {report.meta.num_turns}")
    if report.meta.context_window:
        lines.append(f"  Ctx Window: {_fmt_tokens(report.meta.context_window)}")
    if report.turns and report.meta.context_window:
        ctx_window = report.meta.context_window
        max_fill = max(t.total_input / ctx_window * 100 for t in report.turns)
        lines.append(f"  Peak Fill:  {max_fill:.1f}%")
    return "\n".join(lines) + "\n"


def render_permission_friction(report: SessionReport) -> str:
    """Render permission/policy blocks near the top of a single-log report."""
    frictions = _permission_frictions_for_report(report)
    if not frictions:
        return ""

    by_category = Counter(f.category for f in frictions)
    examples = frictions[:5]
    lines = [
        "",
        "-" * 72,
        "PERMISSION & POLICY FRICTION",
        "-" * 72,
        f"  Blocks: {len(frictions)}",
        "",
        f"  {'Category':<38s} {'Count':>5s}",
        f"  {'-' * 38} {'-' * 5}",
    ]
    for category, count in by_category.most_common():
        lines.append(f"  {category:<38.38s} {count:>5d}")

    lines.extend(
        [
            "",
            f"  {'Example command/detail':<80s} {'Result':<80s}",
            f"  {'-' * 80} {'-' * 80}",
        ]
    )
    for friction in examples:
        detail = friction.detail[:80]
        result = friction.result_preview[:80]
        lines.append(f"  {detail:<80s} {result:<80s}")

    return "\n".join(lines) + "\n"


def render_operational_friction(report: SessionReport) -> str:
    """Render provider-forced foreground timeout/backgrounding facts."""
    if not report.operational_frictions:
        return ""

    lines = [
        "",
        "-" * 72,
        "OPERATIONAL FRICTION",
        "-" * 72,
        f"  Events: {len(report.operational_frictions)}",
        "",
        f"  {'Category':<42s} {'Tool':<14s} {'Duration':>10s}",
        f"  {'-' * 42} {'-' * 14} {'-' * 10}",
    ]
    for friction in report.operational_frictions:
        duration = f"{friction.duration_ms / 1000:.1f}s" if friction.duration_ms else "n/a"
        lines.append(f"  {friction.category:<42.42s} {friction.tool_name:<14.14s} {duration:>10s}")
        lines.append(f"    Command: {friction.command_preview[:120]}")
        lines.append(f"    Result:  {friction.result_preview[:120]}")
        if friction.log_file:
            lines.append(f"    Source:  {friction.log_file}")
    return "\n".join(lines) + "\n"


def render_token_summary(report: SessionReport) -> str:
    total_input = report.total_input_tokens + report.total_cache_creation + report.total_cache_read
    fresh = report.total_input_tokens
    cache_create = report.total_cache_creation
    cache_read = report.total_cache_read
    output = report.total_output_tokens

    cache_eligible = cache_create + cache_read
    hit_rate = (cache_read / cache_eligible * 100) if cache_eligible > 0 else 0.0
    # For sparse format, cache_creation is unavailable so hit rate is computed
    # as cached / total_input instead (a lower bound).
    if cache_create == 0 and cache_read > 0 and total_input > 0:
        hit_rate = cache_read / total_input * 100

    lines = [
        "",
        "-" * 72,
        "TOKEN SUMMARY",
        "-" * 72,
        (
            f"  Total Input:     {_fmt_tokens(total_input):>10s}"
            f"  (fresh: {_fmt_tokens(fresh)},"
            f" cache_create: {_fmt_tokens(cache_create)},"
            f" cache_read: {_fmt_tokens(cache_read)})"
        ),
        f"  Output:          {_fmt_tokens(output):>10s}",
        f"  Cache Hit Rate:  {hit_rate:>9.1f}%",
        f"  Usage Source:    {report.aggregate_usage_source}",
    ]
    return "\n".join(lines) + "\n"


def render_content_breakdown(report: SessionReport) -> str:
    # Group by type
    groups: dict[str, list[ContentItem]] = {}
    for item in report.items:
        groups.setdefault(item.item_type, []).append(item)

    total_chars = sum(it.chars for it in report.items)

    lines = [
        "",
        "-" * 72,
        "CONTENT BREAKDOWN",
        "-" * 72,
        f"  {'Type':<22s} {'Count':>6s} {'Chars':>10s} {'Est.Tok':>10s} {'Share':>7s}",
        f"  {'-' * 22} {'-' * 6} {'-' * 10} {'-' * 10} {'-' * 7}",
    ]

    for item_type in sorted(groups, key=lambda t: -sum(i.chars for i in groups[t])):
        items = groups[item_type]
        count = len(items)
        chars = sum(i.chars for i in items)
        est_tok = _est_tokens(chars)
        share = (chars / total_chars * 100) if total_chars > 0 else 0
        lines.append(f"  {item_type:<22s} {count:>6d} {chars:>10,d} {_fmt_tokens(est_tok):>10s} {share:>6.1f}%")

    lines.append(f"  {'-' * 22} {'-' * 6} {'-' * 10} {'-' * 10} {'-' * 7}")
    total_est = _fmt_tokens(_est_tokens(total_chars))
    lines.append(f"  {'TOTAL':<22s} {len(report.items):>6d} {total_chars:>10,d} {total_est:>10s} {'100.0':>6s}%")

    return "\n".join(lines) + "\n"


def render_top_items(report: SessionReport, n: int = 10) -> str:
    sorted_items = sorted(report.items, key=lambda i: -i.chars)[:n]

    lines = [
        "",
        "-" * 72,
        f"TOP {n} ITEMS BY SIZE",
        "-" * 72,
    ]

    for i, item in enumerate(sorted_items, 1):
        est_tok = _est_tokens(item.chars)
        lines.append(f"  {i:>2d}. [{item.item_type:<18s}] {item.chars:>8,d} chars (~{_fmt_tokens(est_tok)} tok)")
        preview = item.preview[:100]
        if preview:
            lines.append(f"      {preview}")

    return "\n".join(lines) + "\n"


def _turn_usage_coverage_lines(report: SessionReport) -> list[str]:
    """Describe the provenance and aggregate coverage of rich per-turn usage."""
    envelope_totals = (
        sum(turn.input_tokens for turn in report.turns),
        sum(turn.cache_creation_input_tokens for turn in report.turns),
        sum(turn.cache_read_input_tokens for turn in report.turns),
        sum(turn.output_tokens for turn in report.turns),
    )
    aggregate_totals = (
        report.total_input_tokens,
        report.total_cache_creation,
        report.total_cache_read,
        report.total_output_tokens,
    )
    source = report.aggregate_usage_source
    lines = ["  Turn Usage Source: assistant message envelopes"]
    if envelope_totals == aggregate_totals:
        lines.append(f"  Coverage: rows reconcile with {source} aggregate usage")
    else:
        lines.append(
            f"  Coverage: rows do not reconcile with {source} aggregate usage; "
            "use TOKEN SUMMARY for authoritative totals"
        )
    return lines


def render_per_turn_growth(report: SessionReport) -> str:
    """Rich format only: show per-API-call token progression."""
    if not report.turns:
        return ""

    ctx_window = report.meta.context_window

    lines = [
        "",
        "-" * 72,
        "PER-TURN CONTEXT GROWTH",
        "-" * 72,
        *_turn_usage_coverage_lines(report),
        "",
        (
            f"  {'#':>3s}  {'Input':>10s}  {'CacheCreate':>12s}"
            f"  {'CacheRead':>10s}  {'Output':>8s}"
            f"  {'TotalIn':>10s}  {'Fill%':>6s}"
        ),
        f"  {'-' * 3}  {'-' * 10}  {'-' * 12}  {'-' * 10}  {'-' * 8}  {'-' * 10}  {'-' * 6}",
    ]
    if not ctx_window:
        lines.append("  Context window unavailable; Fill% is n/a.")

    for i, turn in enumerate(report.turns, 1):
        total_in = turn.total_input
        fill = f"{total_in / ctx_window * 100:.1f}%" if ctx_window else "n/a"
        lines.append(
            f"  {i:>3d}  "
            f"{_fmt_tokens(turn.input_tokens):>10s}  "
            f"{_fmt_tokens(turn.cache_creation_input_tokens):>12s}  "
            f"{_fmt_tokens(turn.cache_read_input_tokens):>10s}  "
            f"{_fmt_tokens(turn.output_tokens):>8s}  "
            f"{_fmt_tokens(total_in):>10s}  "
            f"{fill:>6s}"
        )

    return "\n".join(lines) + "\n"


def render_longest_turns(report: SessionReport, n: int = 10) -> str:
    """Top longest turns by duration (rich format only)."""
    if not report.turns:
        return ""

    sorted_turns = sorted(report.turns, key=lambda t: -t.duration_ms)[:n]

    # Map turn number to a summary of actions
    turn_actions: dict[int, list[TurnAction]] = {}
    for action in report.actions:
        turn_actions.setdefault(action.turn_num, []).append(action)

    lines = [
        "",
        "-" * 132,
        f"TOP {n} LONGEST TURNS",
        "-" * 132,
        *_turn_usage_coverage_lines(report),
        "",
        f"  {'#':>3s}  {'Duration':>10s}  {'Input':>10s}  {'Output':>10s}  {'Detail'}",
        f"  {'-' * 3}  {'-' * 10}  {'-' * 10}  {'-' * 10}  {'-' * 80}",
    ]

    # Map message_id back to turn number (1-based index in the original list)
    id_to_num = {t.message_id: i + 1 for i, t in enumerate(report.turns)}

    for turn in sorted_turns:
        num = id_to_num.get(turn.message_id, 0)
        dur = f"{turn.duration_ms / 1000:.1f}s"

        actions = turn_actions.get(num, [])
        if actions:
            # Join tool names and details
            detail_parts = []
            for a in actions:
                part = f"{a.tool_name}({a.detail[:40]})"
                detail_parts.append(part)
            detail = " | ".join(detail_parts)
        else:
            # No tool calls - likely a text response or thinking block
            detail = "(text message)"

        lines.append(
            f"  {num:>3d}  {dur:>10s}  "
            f"{_fmt_tokens(turn.total_input):>10s}  "
            f"{_fmt_tokens(turn.output_tokens):>10s}  "
            f"{detail[:80]}"
        )

    return "\n".join(lines) + "\n"


def render_cost(report: SessionReport) -> str:
    """Rich format only: cost breakdown with system prompt multiplier."""
    if report.total_cost_usd == 0:
        return ""

    lines = [
        "",
        "-" * 72,
        "COST",
        "-" * 72,
        f"  Total:            ${report.total_cost_usd:.4f}",
    ]
    if report.turns:
        avg = report.total_cost_usd / len(report.turns)
        lines.append(f"  Per-turn avg:     ${avg:.4f}")
    lines.append(f"  Model:            {report.meta.model}")

    # System prompt cost multiplier
    if report.turns:
        first = report.turns[0]
        sys_prompt_est = first.cache_creation_input_tokens + first.cache_read_input_tokens
        if sys_prompt_est > 0:
            num_turns = len(report.turns)
            # cache_read price is 0.10× of base input price — estimate replay cost
            # Sonnet: $3/M input, $0.30/M cache-read → $0.30 per M per turn
            # Count error turns as "wasted"
            error_turns = sum(1 for a in report.actions if a.is_error)
            lines.append("")
            lines.append(f"  System Prompt:    ~{_fmt_tokens(sys_prompt_est)} tokens (from turn 1)")
            lines.append(
                f"  Cache-read cost:  {_fmt_tokens(sys_prompt_est)} × {num_turns} turns "
                f"= {_fmt_tokens(sys_prompt_est * num_turns)} token·turns"
            )
            if error_turns:
                lines.append(
                    f"  Wasted on errors: ~{error_turns} turn(s) "
                    f"× {_fmt_tokens(sys_prompt_est)} = {_fmt_tokens(sys_prompt_est * error_turns)} wasted"
                )

    return "\n".join(lines) + "\n"


def render_tool_calls(report: SessionReport) -> str:
    """Tool call frequency breakdown."""
    if not report.tool_calls:
        return ""

    lines = [
        "",
        "-" * 72,
        "TOOL USAGE",
        "-" * 72,
        f"  {'Tool':<40s} {'Calls':>6s}",
        f"  {'-' * 40} {'-' * 6}",
    ]

    for name, count in sorted(report.tool_calls.items(), key=lambda x: -x[1]):
        lines.append(f"  {name:<40s} {count:>6d}")

    lines.append(f"  {'-' * 40} {'-' * 6}")
    total = sum(report.tool_calls.values())
    lines.append(f"  {'TOTAL':<40s} {total:>6d}")
    return "\n".join(lines) + "\n"


def render_empty_turns(report: SessionReport) -> str:
    """Empty turn/unit ratio and detail list."""
    if report.turn_units == 0:
        return ""

    empty_count = len(report.empty_turns)
    tool_count = report.tool_turn_units
    text_only_count = report.turn_units - tool_count - empty_count
    empty_ratio = empty_count / report.turn_units * 100

    lines = [
        "",
        "-" * 72,
        "EMPTY TURNS",
        "-" * 72,
        "  Unit basis: API turns",
        "",
        f"  {'Total':>8s}  {'Tool':>8s}  {'Text-only':>8s}  {'Empty':>8s}  {'Empty %':>8s}",
        f"  {'-' * 8}  {'-' * 8}  {'-' * 8}  {'-' * 8}  {'-' * 8}",
        (
            f"  {report.turn_units:>8d}  {tool_count:>8d}  {text_only_count:>8d}"
            f"  {empty_count:>8d}  {empty_ratio:>7.2f}%"
        ),
        "",
        "  Empty turn details:",
    ]

    if not report.empty_turns:
        lines.append("    (none)")
        return "\n".join(lines) + "\n"

    lines.append(f"  {'#':>4s}  {'Type':<18s}  {'Detail':<34s}  Preview")
    lines.append(f"  {'-' * 4}  {'-' * 18}  {'-' * 34}  {'-' * 24}")
    for empty in report.empty_turns:
        detail = empty.detail[:34]
        preview = empty.preview[:100]
        lines.append(f"  {empty.turn_num:>4d}  {empty.item_type:<18s}  {detail:<34s}  {preview}")

    return "\n".join(lines) + "\n"


def render_skill_invocations(report: SessionReport) -> str:
    """Skill invocation breakdown (rich format only)."""
    if not report.skill_invocations:
        return ""

    lines = [
        "",
        "-" * 72,
        "SKILL INVOCATIONS",
        "-" * 72,
        f"  {'Skill':<40s} {'Calls':>6s}",
        f"  {'-' * 40} {'-' * 6}",
    ]

    for name, count in sorted(report.skill_invocations.items(), key=lambda x: -x[1]):
        lines.append(f"  {name:<40s} {count:>6d}")

    lines.append(f"  {'-' * 40} {'-' * 6}")
    total = sum(report.skill_invocations.values())
    lines.append(f"  {'TOTAL':<40s} {total:>6d}")
    return "\n".join(lines) + "\n"


def _parse_secret_words(line: str) -> list[str]:
    """Extract individual secret words from a line.

    Secret words are capitalized or hyphenated tokens. Stops at sentence
    punctuation or the first token that doesn't match (ignoring common prefixes).
    """
    if not line:
        return []
    # Strip "Secret word:" / "Secret words:" prefix if present
    stripped = re.sub(r"^Secret\s+words?:\s*", "", line).replace("/", " ")
    words = []
    for token in stripped.split():
        token_without_closer = token.rstrip("\"')]} ")
        is_terminator = token_without_closer.endswith((".", ";", ":", "!", "?"))
        clean = token.rstrip(".,;:!?")
        if not clean:
            continue
        if clean[0].isupper() or "-" in clean:
            words.append(clean)
            if is_terminator:
                break
        else:
            break
    return words


def render_secret_words(report: SessionReport) -> str:
    """Secret words detection from early assistant content."""
    lines = [
        "",
        "-" * 72,
        "SECRET WORDS",
        "-" * 72,
        f"  Applicability: {report.breadcrumb_applicability}",
    ]

    if report.secret_words_lines:
        for raw in report.secret_words_lines:
            lines.append(f"  Raw: {raw[:120]}")
        # Collect words from all matching lines
        all_words: list[str] = []
        for raw in report.secret_words_lines:
            all_words.extend(_parse_secret_words(raw))
        if all_words:
            lines.append(f"  Found: {', '.join(all_words)}")
        else:
            lines.append("  Found: (none)")
    else:
        lines.append(f"  (no lines starting with '{BRAND_NAME_TITLE}' or 'Secret' in first 30 lines)")
        lines.append("  Found: (none)")
        if report.breadcrumb_applicability == "required":
            lines.append("  Status: missing")
        elif report.breadcrumb_applicability == "unknown":
            lines.append("  Status: unknown")

    return "\n".join(lines) + "\n"


def render_mcp_status(report: SessionReport) -> str:
    """MCP server connection status (rich format only)."""
    if not report.mcp_servers:
        return ""

    lines = [
        "",
        "-" * 72,
        "MCP SERVERS",
        "-" * 72,
    ]

    for srv in report.mcp_servers:
        name = srv["name"]
        status = srv["status"]
        icon = "+" if status == "connected" else "x"
        lines.append(f"  [{icon}] {name:<30s} {status}")

    return "\n".join(lines) + "\n"


def render_mcp_usage(report: SessionReport) -> str:
    """MCP usage: per-server call count + error rate, result volume, top tools."""
    if not report.actions:
        return ""

    # Exclude the product coordination server; those calls are not MCP tool adoption.
    mcp_actions: list[tuple[str, str, TurnAction]] = []
    for action in report.actions:
        parsed = _parse_mcp_tool_name(action.tool_name)
        if parsed and parsed[0] != BRAND_MCP_SERVER:
            mcp_actions.append((parsed[0], parsed[1], action))

    if not mcp_actions:
        return ""

    # Per-server aggregation
    server_stats: dict[str, dict] = {}
    for server, tool, action in mcp_actions:
        if server not in server_stats:
            server_stats[server] = {
                "calls": 0,
                "errors": 0,
                "total_chars": 0,
                "max_chars": 0,
                "tools": {},
            }
        s = server_stats[server]
        s["calls"] += 1
        if action.is_error:
            s["errors"] += 1
        s["total_chars"] += action.result_chars
        s["max_chars"] = max(s["max_chars"], action.result_chars)
        if tool not in s["tools"]:
            s["tools"][tool] = {"calls": 0, "errors": 0, "total_chars": 0}
        t = s["tools"][tool]
        t["calls"] += 1
        if action.is_error:
            t["errors"] += 1
        t["total_chars"] += action.result_chars

    total_actions = len(report.actions)
    total_mcp = len(mcp_actions)

    lines = [
        "",
        "-" * 72,
        "MCP USAGE",
        "-" * 72,
        f"  MCP calls: {total_mcp}/{total_actions} ({total_mcp / total_actions * 100:.0f}% of all tool calls)",
        "",
        f"  {'Server':<25s} {'Calls':>6s} {'Errors':>12s} {'Total':>10s} {'Avg':>8s} {'Max':>8s}",
        f"  {'-' * 25} {'-' * 6} {'-' * 12} {'-' * 10} {'-' * 8} {'-' * 8}",
    ]

    for server in sorted(server_stats, key=lambda s: -server_stats[s]["calls"]):
        s = server_stats[server]
        avg = s["total_chars"] // s["calls"] if s["calls"] else 0
        err_str = f"{s['errors']} ({s['errors'] / s['calls'] * 100:.0f}%)" if s["errors"] else "0"
        lines.append(
            f"  {server:<25s} {s['calls']:>6d} {err_str:>12s}"
            f" {_fmt_tokens(s['total_chars']) + 'c':>10s}"
            f" {_fmt_tokens(avg) + 'c':>8s}"
            f" {_fmt_tokens(s['max_chars']) + 'c':>8s}"
        )

    # Top MCP tools across all servers
    tool_list: list[tuple[str, str, dict]] = []
    for server, s in server_stats.items():
        for tool, t in s["tools"].items():
            tool_list.append((server, tool, t))
    tool_list.sort(key=lambda x: -x[2]["calls"])

    lines.append("")
    lines.append(f"  {'Server/Tool':<45s} {'Calls':>6s} {'Errors':>7s} {'Total':>10s}")
    lines.append(f"  {'-' * 45} {'-' * 6} {'-' * 7} {'-' * 10}")

    for server, tool, t in tool_list[:15]:
        lines.append(
            f"  {server + '/' + tool:<45s} {t['calls']:>6d}"
            f" {t['errors']:>7d}"
            f" {_fmt_tokens(t['total_chars']) + 'c':>10s}"
        )

    return "\n".join(lines) + "\n"


def render_turn_timeline(report: SessionReport) -> str:
    """Turn-by-turn tool invocation timeline."""
    if not report.actions:
        return ""

    has_duration = report.meta.format == "rich"
    duration_header = f"  {'Duration':>10s}" if has_duration else ""
    duration_separator = f"  {'-' * 10}" if has_duration else ""
    timeline_width = 152 if has_duration else 140

    lines = [
        "",
        "-" * timeline_width,
        "TURN TIMELINE",
        "-" * timeline_width,
        f"  {'#':>3s}{duration_header}  {'Tool':<20s} {'Detail':<100s} {'Result':>8s} {'Err':>4s}",
        f"  {'-' * 3}{duration_separator}  {'-' * 20} {'-' * 100} {'-' * 8} {'-' * 4}",
    ]

    for i, action in enumerate(report.actions, 1):
        detail = action.detail[:100] if action.detail else ""
        result_size = f"{action.result_chars / 1024:.1f}K" if action.result_chars >= 1024 else f"{action.result_chars}"
        err = " ERR" if action.is_error else ""
        duration_value = f"  {action.duration_ms / 1000:>9.1f}s" if has_duration else ""
        lines.append(f"  {i:>3d}{duration_value}  {action.tool_name:<20s} {detail:<100s} {result_size:>8s} {err:>4s}")

    total_result = sum(a.result_chars for a in report.actions)
    error_count = sum(1 for a in report.actions if a.is_error)
    lines.append(f"  {'-' * 3}{duration_separator}  {'-' * 20} {'-' * 100} {'-' * 8} {'-' * 4}")

    duration_blank = f"  {'':>10s}" if has_duration else ""
    result_padding = " " * (102 if has_duration else 104)
    lines.append(
        f"  {'':>3s}{duration_blank}  {len(report.actions)} calls"
        f"{result_padding}{total_result / 1024:.0f}K total   {error_count} err"
    )

    return "\n".join(lines) + "\n"


def render_tool_result_breakdown(report: SessionReport) -> str:
    """Aggregate result sizes by tool name."""
    if not report.actions:
        return ""

    # Group by tool name, excluding MCP tools (reported in MCP USAGE)
    groups: dict[str, list[TurnAction]] = {}
    for action in report.actions:
        parsed = _parse_mcp_tool_name(action.tool_name)
        if parsed and parsed[0] != BRAND_MCP_SERVER:
            continue
        groups.setdefault(action.tool_name, []).append(action)

    lines = [
        "",
        "-" * 72,
        "TOOL RESULT BREAKDOWN",
        "-" * 72,
        f"  {'Tool':<25s} {'Calls':>6s} {'Total':>10s} {'Avg':>8s} {'Max':>8s}",
        f"  {'-' * 25} {'-' * 6} {'-' * 10} {'-' * 8} {'-' * 8}",
    ]

    sorted_groups = sorted(groups.items(), key=lambda kv: sum(a.result_chars for a in kv[1]), reverse=True)

    for name, actions in sorted_groups:
        total = sum(a.result_chars for a in actions)
        avg = total // len(actions) if actions else 0
        mx = max(a.result_chars for a in actions) if actions else 0
        lines.append(
            f"  {name:<25s} {len(actions):>6d}"
            f" {_fmt_tokens(total) + 'c':>10s}"
            f" {_fmt_tokens(avg) + 'c':>8s}"
            f" {_fmt_tokens(mx) + 'c':>8s}"
        )

    return "\n".join(lines) + "\n"


def render_efficiency_insights(report: SessionReport) -> str:
    """Detect waste: errors, exact duplicate results, repetitive low-value calls."""
    if not report.actions:
        return ""

    findings: list[str] = []

    # 1. Error count
    errors = [a for a in report.actions if a.is_error]
    if errors:
        tool_errs: dict[str, int] = {}
        for e in errors:
            tool_errs[e.tool_name] = tool_errs.get(e.tool_name, 0) + 1
        breakdown = ", ".join(f"{n}×{t}" for t, n in sorted(tool_errs.items(), key=lambda x: -x[1]))
        findings.append(f"  🔴 {len(errors)} error(s): {breakdown}")

    # 2. Exact duplicate results (>1KB with identical output payloads).
    # Prefix-based grouping produces false positives for shared YAML headers and
    # serialized MCP path prefixes, so use full result hashes instead.
    result_hashes: dict[str, list[TurnAction]] = {}
    for a in report.actions:
        if a.result_chars >= 1024 and a.result_hash:
            result_hashes.setdefault(a.result_hash, []).append(a)
    dupes = {k: v for k, v in result_hashes.items() if len(v) >= 2}
    if dupes:
        total_waste = sum(sum(a.result_chars for a in group[1:]) for group in dupes.values())
        findings.append(f"  🟠 {len(dupes)} duplicate result(s) (~{total_waste / 1024:.0f}KB repeated)")
        for _, group in list(dupes.items())[:3]:
            events = ", ".join(f"#{a.turn_num}" for a in group)
            tools = ", ".join(dict.fromkeys(a.tool_name for a in group))
            findings.append(f"      {len(group)}× {tools} at {events}: {group[0].result_preview[:60]}...")

    # 3. Repetitive low-value: same tool called 5+ times with avg result <200 chars
    groups: dict[str, list[TurnAction]] = {}
    for a in report.actions:
        groups.setdefault(a.tool_name, []).append(a)
    for name, actions in groups.items():
        if len(actions) >= 5:
            avg = sum(a.result_chars for a in actions) / len(actions)
            if avg < 200:
                findings.append(
                    f"  🔵 {name} called {len(actions)}× with avg result {avg:.0f} chars (low-value chatter?)"
                )

    if not findings:
        return ""

    lines = [
        "",
        "-" * 72,
        "EFFICIENCY INSIGHTS",
        "-" * 72,
    ] + findings

    return "\n".join(lines) + "\n"


def _find_struggle_sequences(actions: list[TurnAction]) -> list[dict]:
    """Find clusters of errors indicating the agent was stuck on one problem.

    A struggle sequence starts at an error and extends to include all actions
    up to 3 positions past the last error (context/diagnostic actions). A new
    error within that window extends the sequence further. The sequence ends
    when 4+ consecutive non-error actions follow the last error.
    """
    sequences: list[dict] = []
    i = 0
    n = len(actions)

    while i < n:
        if not actions[i].is_error:
            i += 1
            continue

        # Start of a potential sequence
        start = i
        last_error_idx = i
        error_count = 1
        j = i + 1

        while j < n:
            if actions[j].is_error:
                last_error_idx = j
                error_count += 1
                j += 1
            elif j - last_error_idx <= 3:
                # Allow up to 3 non-error actions after last error (diagnostics)
                j += 1
            else:
                break

        end = last_error_idx  # inclusive, last error position
        # Include up to 1 trailing non-error action for context
        if end + 1 < n and not actions[end + 1].is_error:
            end += 1

        span = actions[start : end + 1]
        total_actions = len(span)

        if error_count >= 2:
            # Extract distinct turn numbers for turn cost
            turn_nums = sorted(set(a.turn_num for a in span if a.turn_num > 0))
            # Summarize what tools were tried
            tool_attempts: dict[str, int] = {}
            for a in span:
                tool_attempts[a.tool_name] = tool_attempts.get(a.tool_name, 0) + 1

            # Try to identify a root cause from the first error's detail/preview
            first_error = next(a for a in span if a.is_error)
            root_hint = first_error.result_preview[:80] if first_error.result_preview else first_error.detail[:80]

            sequences.append(
                {
                    "start_idx": start,
                    "end_idx": end,
                    "start_action": start + 1,  # 1-based
                    "end_action": end + 1,
                    "total_actions": total_actions,
                    "error_count": error_count,
                    "turn_nums": turn_nums,
                    "num_turns": len(turn_nums),
                    "tool_attempts": tool_attempts,
                    "root_hint": root_hint,
                    "actions": span,
                }
            )

        i = end + 1

    return sequences


def render_struggle_sequences(report: SessionReport) -> str:
    """Detect and render struggle sequences — clusters of errors from one root cause."""
    if not report.actions:
        return ""

    sequences = _find_struggle_sequences(report.actions)
    if not sequences:
        return ""

    # Estimate system prompt size from first turn (if available)
    sys_prompt_tokens = 0
    if report.turns:
        first = report.turns[0]
        sys_prompt_tokens = first.cache_creation_input_tokens + first.cache_read_input_tokens

    lines = [
        "",
        "-" * 72,
        "STRUGGLE SEQUENCES",
        "-" * 72,
    ]

    for seq in sequences:
        lines.append(
            f"  #{seq['start_action']}–#{seq['end_action']} "
            f"({seq['total_actions']} actions, {seq['error_count']} errors, "
            f"{seq['num_turns']} turns)"
        )
        # Root cause hint
        if seq["root_hint"]:
            lines.append(f"    Root:    {seq['root_hint']}")
        # Tool attempts
        attempts = ", ".join(f"{n}×{t}" for t, n in sorted(seq["tool_attempts"].items(), key=lambda x: -x[1]))
        lines.append(f"    Retries: {attempts}")
        # Replay cost
        if sys_prompt_tokens > 0:
            wasted = sys_prompt_tokens * seq["num_turns"]
            lines.append(
                f"    Replay cost: {seq['num_turns']} turns × "
                f"{_fmt_tokens(sys_prompt_tokens)} sys prompt = "
                f"{_fmt_tokens(wasted)} cache-read tokens"
            )
        lines.append("")

    return "\n".join(lines)


def render_report(report: SessionReport) -> str:
    """Assemble all report sections."""
    sections = [
        render_header(report),
        render_operational_friction(report),
        render_permission_friction(report),
        render_token_summary(report),
        render_content_breakdown(report),
        render_top_items(report),
        render_tool_calls(report),
        render_empty_turns(report),
        render_skill_invocations(report),
        render_secret_words(report),
    ]

    if report.meta.format == "rich":
        sections.append(render_per_turn_growth(report))
        sections.append(render_longest_turns(report))
    else:
        sections.append("")
        sections.append("  Note: Per-turn growth unavailable in sparse format (single turn).")
        sections.append("")

    # Action-derived sections are available for both rich Claude logs and sparse Codex logs.
    sections.extend(
        [
            render_turn_timeline(report),
            render_tool_result_breakdown(report),
            render_mcp_usage(report),
            render_efficiency_insights(report),
            render_struggle_sequences(report),
        ]
    )

    if report.meta.format == "rich":
        sections.append(render_cost(report))
        sections.append(render_mcp_status(report))

    return "\n".join(s for s in sections if s)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def analyze_file(filepath: str) -> SessionReport | None:
    path = Path(filepath)
    if not path.exists():
        print(f"WARNING: File not found: {filepath}", file=sys.stderr)
        return None

    lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
    if not lines:
        print(f"WARNING: Empty file: {filepath}", file=sys.stderr)
        return None

    fmt = detect_format(lines)
    if fmt == "unknown":
        print(f"WARNING: Unknown format in {filepath}, skipping", file=sys.stderr)
        return None

    if fmt == "rich":
        report = parse_rich(lines)
    else:
        report = parse_sparse(lines)

    report.meta.file = filepath
    role = _role_from_log_path(filepath)
    for friction in report.operational_frictions:
        friction.log_file = path.name
        friction.role = role
    return report


def _role_from_log_path(filepath: str) -> str:
    """Infer role name from `<role>-YYYYMMDD-HHMMSS.txt` log filenames."""
    name = Path(filepath).name
    stem = name[:-4] if name.endswith(".txt") else Path(name).stem
    parts = stem.rsplit("-", 2)
    return parts[0] if len(parts) == 3 else stem


def _total_input_tokens(report: SessionReport) -> int:
    return report.total_input_tokens + report.total_cache_creation + report.total_cache_read


def _cache_hit_rate(report: SessionReport) -> float:
    total_input = _total_input_tokens(report)
    if report.total_cache_creation == 0 and report.total_cache_read > 0 and total_input > 0:
        return report.total_cache_read / total_input * 100
    cache_eligible = report.total_cache_creation + report.total_cache_read
    return report.total_cache_read / cache_eligible * 100 if cache_eligible else 0.0


def render_role_summary(reports: list[SessionReport]) -> str:
    """Render aggregate log metrics grouped by role."""
    by_role: dict[str, list[SessionReport]] = defaultdict(list)
    for report in reports:
        by_role[_role_from_log_path(report.meta.file)].append(report)

    total_logs = sum(len(role_reports) for role_reports in by_role.values())
    total_input = sum(_total_input_tokens(report) for report in reports)
    total_fresh = sum(report.total_input_tokens for report in reports)
    total_cache_create = sum(report.total_cache_creation for report in reports)
    total_cache_read = sum(report.total_cache_read for report in reports)
    total_output = sum(report.total_output_tokens for report in reports)
    total_errors = sum(1 for report in reports for action in report.actions if action.is_error)
    format_counts = Counter(report.meta.format for report in reports)
    usage_source_counts = Counter(report.aggregate_usage_source for report in reports)

    lines = [
        "=" * 72,
        "ROLE SUMMARY",
        "=" * 72,
        f"  Logs:          {total_logs}",
        f"  Formats:       {', '.join(f'{name}:{count}' for name, count in sorted(format_counts.items()))}",
        f"  Usage Sources: {', '.join(f'{name}:{count}' for name, count in sorted(usage_source_counts.items()))}",
        (
            f"  Total Input:   {_fmt_tokens(total_input)}"
            f"  (fresh: {_fmt_tokens(total_fresh)},"
            f" cache_create: {_fmt_tokens(total_cache_create)},"
            f" cache_read: {_fmt_tokens(total_cache_read)})"
        ),
        f"  Output:        {_fmt_tokens(total_output)}",
        f"  Errors:        {total_errors}",
        "",
        (
            f"  {'Role':<28s} {'Logs':>5s} {'Input':>9s} {'Fresh':>9s}"
            f" {'Cache':>9s} {'Output':>8s} {'Hit%':>6s} {'Err':>5s} {'Partial':>7s}"
        ),
        (f"  {'-' * 28} {'-' * 5} {'-' * 9} {'-' * 9} {'-' * 9} {'-' * 8} {'-' * 6} {'-' * 5} {'-' * 7}"),
    ]

    for role, role_reports in sorted(by_role.items()):
        role_input = sum(_total_input_tokens(report) for report in role_reports)
        role_fresh = sum(report.total_input_tokens for report in role_reports)
        role_cache = sum(report.total_cache_creation + report.total_cache_read for report in role_reports)
        role_output = sum(report.total_output_tokens for report in role_reports)
        role_errors = sum(1 for report in role_reports for action in report.actions if action.is_error)
        role_partial = sum(report.aggregate_usage_source == "envelope-partial" for report in role_reports)
        weighted_hit = (
            sum(_cache_hit_rate(report) * _total_input_tokens(report) for report in role_reports) / role_input
            if role_input
            else 0.0
        )
        lines.append(
            f"  {role:<28s} {len(role_reports):>5d}"
            f" {_fmt_tokens(role_input):>9s}"
            f" {_fmt_tokens(role_fresh):>9s}"
            f" {_fmt_tokens(role_cache):>9s}"
            f" {_fmt_tokens(role_output):>8s}"
            f" {weighted_hit:>5.1f}%"
            f" {role_errors:>5d}"
            f" {role_partial:>7d}"
        )

    operational_frictions = [friction for report in reports for friction in report.operational_frictions]
    if operational_frictions:
        by_category = Counter(friction.category for friction in operational_frictions)
        by_role_friction = Counter(friction.role for friction in operational_frictions)
        lines.extend(
            [
                "",
                "OPERATIONAL FRICTION",
                f"  Events: {len(operational_frictions)} ({len(by_category)} categories)",
                "",
                f"  {'Category':<42s} {'Count':>5s}",
                f"  {'-' * 42} {'-' * 5}",
            ]
        )
        for category, count in by_category.most_common():
            lines.append(f"  {category:<42.42s} {count:>5d}")
        lines.extend(["", f"  {'Role':<28s} {'Events':>6s}", f"  {'-' * 28} {'-' * 6}"])
        for role, count in by_role_friction.most_common():
            lines.append(f"  {role:<28s} {count:>6d}")
        lines.extend(
            [
                "",
                f"  {'Example log':<38s} {'Tool':<14s} {'Command':<60s}",
                f"  {'-' * 38} {'-' * 14} {'-' * 60}",
            ]
        )
        for operational_friction in operational_frictions[:5]:
            lines.append(
                f"  {operational_friction.log_file:<38.38s}"
                f" {operational_friction.tool_name:<14.14s}"
                f" {operational_friction.command_preview[:60]:<60s}"
            )

    permission_frictions = [friction for report in reports for friction in _permission_frictions_for_report(report)]
    if permission_frictions:
        by_category = Counter(friction.category for friction in permission_frictions)
        by_role_friction = Counter(friction.role for friction in permission_frictions)
        lines.extend(
            [
                "",
                "PERMISSION & POLICY FRICTION",
                f"  Blocks: {len(permission_frictions)} ({len(by_category)} categories)",
                "",
                f"  {'Category':<38s} {'Count':>5s}",
                f"  {'-' * 38} {'-' * 5}",
            ]
        )
        for category, count in by_category.most_common(12):
            lines.append(f"  {category:<38.38s} {count:>5d}")
        lines.extend(
            [
                "",
                f"  {'Role':<28s} {'Blocks':>6s}",
                f"  {'-' * 28} {'-' * 6}",
            ]
        )
        for role, count in by_role_friction.most_common():
            lines.append(f"  {role:<28s} {count:>6d}")
        lines.extend(
            [
                "",
                f"  {'Example log':<38s} {'Category':<32s} {'Command/detail':<60s}",
                f"  {'-' * 38} {'-' * 32} {'-' * 60}",
            ]
        )
        for permission_friction in permission_frictions[:5]:
            lines.append(
                f"  {permission_friction.log_file:<38.38s}"
                f" {permission_friction.category:<32.32s}"
                f" {permission_friction.detail[:60]:<60s}"
            )

    tool_chars: Counter[str] = Counter()
    tool_calls: Counter[str] = Counter()
    mcp_server_calls: Counter[str] = Counter()
    mcp_server_errors: Counter[str] = Counter()
    mcp_server_chars: Counter[str] = Counter()
    mcp_tool_calls: Counter[str] = Counter()
    mcp_tool_errors: Counter[str] = Counter()
    mcp_tool_chars: Counter[str] = Counter()
    skills: Counter[str] = Counter()
    for report in reports:
        skills.update(report.skill_invocations)
        for action in report.actions:
            tool_chars[action.tool_name] += action.result_chars
            tool_calls[action.tool_name] += 1
            parsed = _parse_mcp_tool_name(action.tool_name)
            if parsed and parsed[0] != BRAND_MCP_SERVER:
                server, tool = parsed
                server_tool = f"{server}/{tool}"
                mcp_server_calls[server] += 1
                mcp_server_chars[server] += action.result_chars
                mcp_tool_calls[server_tool] += 1
                mcp_tool_chars[server_tool] += action.result_chars
                if action.is_error:
                    mcp_server_errors[server] += 1
                    mcp_tool_errors[server_tool] += 1

    lines.extend(
        [
            "",
            "TOP TOOL RESULT VOLUME",
            f"  {'Tool':<25s} {'Calls':>6s} {'Total':>10s}",
            f"  {'-' * 25} {'-' * 6} {'-' * 10}",
        ]
    )
    for tool, chars in tool_chars.most_common(12):
        lines.append(f"  {tool:<25s} {tool_calls[tool]:>6d} {_fmt_tokens(chars) + 'c':>10s}")

    if mcp_server_calls:
        total_actions = sum(tool_calls.values())
        total_mcp = sum(mcp_server_calls.values())
        lines.extend(
            [
                "",
                "MCP USAGE",
                f"  MCP calls: {total_mcp}/{total_actions} ({total_mcp / total_actions * 100:.0f}% of all tool calls)",
                "",
                f"  {'Server':<25s} {'Calls':>6s} {'Errors':>7s} {'Total':>10s}",
                f"  {'-' * 25} {'-' * 6} {'-' * 7} {'-' * 10}",
            ]
        )
        for server, calls in mcp_server_calls.most_common():
            lines.append(
                f"  {server:<25s} {calls:>6d}"
                f" {mcp_server_errors[server]:>7d}"
                f" {_fmt_tokens(mcp_server_chars[server]) + 'c':>10s}"
            )
        lines.extend(
            [
                "",
                f"  {'Server/Tool':<45s} {'Calls':>6s} {'Errors':>7s} {'Total':>10s}",
                f"  {'-' * 45} {'-' * 6} {'-' * 7} {'-' * 10}",
            ]
        )
        for server_tool, calls in mcp_tool_calls.most_common(15):
            lines.append(
                f"  {server_tool:<45s} {calls:>6d}"
                f" {mcp_tool_errors[server_tool]:>7d}"
                f" {_fmt_tokens(mcp_tool_chars[server_tool]) + 'c':>10s}"
            )

    if skills:
        lines.extend(
            [
                "",
                "SKILL INVOCATIONS",
                f"  {'Skill':<35s} {'Calls':>6s}",
                f"  {'-' * 35} {'-' * 6}",
            ]
        )
        for skill, count in skills.most_common():
            lines.append(f"  {skill:<35s} {count:>6d}")

    return "\n".join(lines) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser(description=f"Analyze {BRAND_NAME_TITLE} agent log files.")
    parser.add_argument(
        "--summary-by-role",
        action="store_true",
        help="print one aggregate report grouped by inferred agent role instead of per-log reports",
    )
    parser.add_argument("logs", nargs="+", help="NDJSON agent log files to analyze")
    args = parser.parse_args()

    reports = [report for filepath in args.logs if (report := analyze_file(filepath))]
    if not reports:
        sys.exit(1)

    if args.summary_by_role:
        print(render_role_summary(reports))
        return

    for report in reports:
        print(render_report(report))
        print()


if __name__ == "__main__":
    main()
