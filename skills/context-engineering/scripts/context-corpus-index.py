#!/usr/bin/env python3
"""Index §BRAND_NAME_TITLE§ prompt/output corpora for context-engineering audits.

The script intentionally does mechanical discovery only: corpus inventory,
prompt/output pairing, pressure metrics, outcome signals, and a sampling plan.
It does not decide causality.
"""

from __future__ import annotations

import argparse
import json
import re
import shlex
from collections import Counter, defaultdict
from dataclasses import asdict, dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Any

SESSION_RE = re.compile(r"^(?P<role>.+)-(?P<date>\d{8})-(?P<time>\d{6})(?P<suffix>\.[^.]+)$")

VERDICT_RE = re.compile(r'"verdict"\s*:\s*"(APPROVED|REJECTED)"')
STATUS_RE = re.compile(r'"status"\s*:\s*"(BLOCKED|CODE_APPROVED|IMPLEMENTING_CODE|MERGED|REVIEWING_CODE|SUPERSEDED)"')
BRAND_NAME_TITLE = "§BRAND_NAME_TITLE§"
BRAND_PROJECT_DIRNAME = "§BRAND_PROJECT_DIRNAME§"


@dataclass
class FileInfo:
    path: str
    name: str
    role: str | None
    timestamp: str | None
    size_bytes: int
    suffix: str


@dataclass
class OutputMetrics:
    path: str
    name: str
    role: str | None
    timestamp: str | None
    size_bytes: int
    log_format: str = "unknown"
    parse_errors: int = 0
    event_counts: dict[str, int] = field(default_factory=dict)
    item_counts: dict[str, int] = field(default_factory=dict)
    tool_counts: dict[str, int] = field(default_factory=dict)
    mcp_counts: dict[str, int] = field(default_factory=dict)
    content_chars: dict[str, int] = field(default_factory=dict)
    top_items: list[dict[str, Any]] = field(default_factory=list)
    errors: int = 0
    verdicts: dict[str, int] = field(default_factory=dict)
    statuses: dict[str, int] = field(default_factory=dict)
    handoff_markers: int = 0
    total_input_tokens: int = 0
    cache_read_tokens: int = 0
    cache_create_tokens: int = 0
    output_tokens: int = 0
    context_window: int | None = None
    peak_context_tokens: int = 0
    model: str | None = None
    session_id: str | None = None
    terminal_reason: str | None = None


@dataclass
class PairInfo:
    output: str
    output_role: str
    output_timestamp: str
    prompt: str | None
    prompt_timestamp: str | None
    delta_seconds: int | None
    confidence: str
    prompt_size_bytes: int | None
    output_size_bytes: int


def parse_file_info(path: Path, base: Path) -> FileInfo:
    match = SESSION_RE.match(path.name)
    role = None
    timestamp = None
    if match:
        role = match.group("role")
        timestamp = datetime.strptime(match.group("date") + match.group("time"), "%Y%m%d%H%M%S").isoformat(
            timespec="seconds"
        )
    return FileInfo(
        path=str(path.relative_to(base)),
        name=path.name,
        role=role,
        timestamp=timestamp,
        size_bytes=path.stat().st_size,
        suffix=path.suffix,
    )


def timestamp_dt(info: FileInfo | OutputMetrics) -> datetime | None:
    if not info.timestamp:
        return None
    return datetime.fromisoformat(info.timestamp)


def detect_log_format(event_type: str) -> str:
    if event_type == "system":
        return "rich"
    if event_type == "thread.started":
        return "sparse"
    return "unknown"


def resolve_agent_data_dir(path: Path) -> Path:
    if (path / "agent-prompts").is_dir() and (path / "agent-outputs").is_dir():
        return path
    project_data_dir = path / BRAND_PROJECT_DIRNAME
    if (project_data_dir / "agent-prompts").is_dir() and (project_data_dir / "agent-outputs").is_dir():
        return project_data_dir
    raise SystemExit(f"not a {BRAND_NAME_TITLE} data directory: {path}")


def add_text_signals(text: str, verdicts: Counter[str], statuses: Counter[str]) -> int:
    for verdict in VERDICT_RE.findall(text):
        verdicts[verdict] += 1
    for status in STATUS_RE.findall(text):
        statuses[status] += 1
    lowered = text.lower()
    return sum(
        marker in lowered
        for marker in (
            "handoff",
            "context exhaustion",
            "handoff_pending",
            "retry_loop",
            "review_exhaustion",
        )
    )


def usage_total(usage: dict[str, Any]) -> tuple[int, int, int, int]:
    cache_read = int(usage.get("cache_read_input_tokens", 0))
    cache_create = int(usage.get("cache_creation_input_tokens", 0))
    output = int(usage.get("output_tokens", 0))
    if "cached_input_tokens" in usage:
        total_input = int(usage.get("input_tokens", 0))
        cache_read = max(cache_read, int(usage.get("cached_input_tokens", 0)))
    else:
        total_input = int(usage.get("input_tokens", 0)) + cache_read + cache_create
    return total_input, cache_read, cache_create, output


def usage_context_window(obj: dict[str, Any]) -> int | None:
    model_usage = obj.get("modelUsage")
    if isinstance(model_usage, dict):
        for stats in model_usage.values():
            if isinstance(stats, dict) and stats.get("contextWindow"):
                return int(stats["contextWindow"])
    return None


def update_usage(metrics: OutputMetrics, usage: Any, envelope: dict[str, Any]) -> None:
    if not isinstance(usage, dict):
        return
    total_input, cache_read, cache_create, output = usage_total(usage)
    metrics.total_input_tokens = max(metrics.total_input_tokens, total_input)
    metrics.cache_read_tokens = max(metrics.cache_read_tokens, cache_read)
    metrics.cache_create_tokens = max(metrics.cache_create_tokens, cache_create)
    metrics.output_tokens = max(metrics.output_tokens, output)
    metrics.peak_context_tokens = max(metrics.peak_context_tokens, total_input)
    context_window = usage_context_window(envelope)
    if context_window:
        metrics.context_window = context_window


def item_label(item: dict[str, Any], item_type: str) -> str:
    if item_type == "command_execution":
        return str(item.get("command", ""))[:160]
    if item_type == "mcp_tool_call":
        server = item.get("server") or item.get("mcp_server") or ""
        tool = item.get("tool") or item.get("name") or ""
        return f"{server}/{tool}".strip("/")[:160]
    if item_type == "tool_use":
        name = item.get("name") or item.get("tool") or ""
        return str(name)[:160]
    if item_type == "file_change":
        return str(item.get("path") or item.get("file") or item.get("name") or "")[:160]
    return item_type


def command_tool(command: str) -> str:
    try:
        parts = shlex.split(command)
    except ValueError:
        parts = command.split()
    if not parts:
        return "command"

    executable = Path(parts[0]).name
    if executable in {"bash", "sh", "zsh"} and "-lc" in parts:
        idx = parts.index("-lc")
        if idx + 1 < len(parts):
            try:
                inner = shlex.split(parts[idx + 1])
            except ValueError:
                inner = parts[idx + 1].split()
            if inner:
                return Path(inner[0]).name
    return executable


def record_item(
    content_chars: Counter[str],
    top_items: list[dict[str, Any]],
    item_type: str,
    label: str,
    chars: int,
    error: bool = False,
) -> None:
    content_chars[item_type] += chars
    top_items.append(
        {
            "type": item_type,
            "label": label,
            "chars": chars,
            "error": error,
        }
    )


def scan_output(path: Path, base: Path) -> OutputMetrics:
    info = parse_file_info(path, base)
    events: Counter[str] = Counter()
    items: Counter[str] = Counter()
    tools: Counter[str] = Counter()
    mcps: Counter[str] = Counter()
    content_chars: Counter[str] = Counter()
    verdicts: Counter[str] = Counter()
    statuses: Counter[str] = Counter()
    top_items: list[dict[str, Any]] = []
    tool_use_labels: dict[str, str] = {}

    metrics = OutputMetrics(
        path=info.path,
        name=info.name,
        role=info.role,
        timestamp=info.timestamp,
        size_bytes=info.size_bytes,
    )

    with path.open("r", encoding="utf-8", errors="replace") as handle:
        for line in handle:
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                metrics.parse_errors += 1
                continue

            event_type = str(obj.get("type", "unknown"))
            if metrics.log_format == "unknown":
                metrics.log_format = detect_log_format(event_type)
            events[event_type] += 1
            if obj.get("session_id") and not metrics.session_id:
                metrics.session_id = str(obj["session_id"])
            if event_type == "thread.started" and obj.get("thread_id"):
                metrics.session_id = str(obj["thread_id"])
            if event_type == "result":
                metrics.terminal_reason = str(obj.get("terminal_reason") or obj.get("subtype") or "")
                update_usage(metrics, obj.get("usage"), obj)
                if obj.get("modelUsage") and not metrics.model:
                    metrics.model = next(iter(obj["modelUsage"].keys()))
            if event_type == "turn.completed":
                update_usage(metrics, obj.get("usage"), obj)

            if isinstance(obj.get("message"), dict):
                message = obj["message"]
                if message.get("model") and not metrics.model:
                    metrics.model = str(message["model"])
                update_usage(metrics, message.get("usage"), obj)
                content = message.get("content")
                if isinstance(content, list):
                    for part in content:
                        if not isinstance(part, dict):
                            continue
                        part_type = str(part.get("type", "message_part"))
                        if part_type == "tool_use":
                            name = str(part.get("name", "tool_use"))
                            label = name
                            if name == "Bash" and isinstance(part.get("input"), dict):
                                label = command_tool(str(part["input"].get("command", "")))
                                tools[label] += 1
                            else:
                                tools[name] += 1
                            if part.get("id"):
                                tool_use_labels[str(part["id"])] = label
                            chars = len(json.dumps(part.get("input", {}), sort_keys=True))
                            record_item(content_chars, top_items, part_type, label, chars)
                        elif part_type == "tool_result":
                            text = str(part.get("content", ""))
                            metrics.handoff_markers += add_text_signals(text, verdicts, statuses)
                            label = tool_use_labels.get(str(part.get("tool_use_id")), "tool_result")
                            record_item(
                                content_chars,
                                top_items,
                                part_type,
                                label,
                                len(text),
                                bool(part.get("is_error")),
                            )
                            if part.get("is_error"):
                                metrics.errors += 1
                        else:
                            text = str(part.get(part_type) or part.get("text") or "")
                            metrics.handoff_markers += add_text_signals(text, verdicts, statuses)
                            record_item(content_chars, top_items, part_type, part_type, len(text))

            if event_type == "item.completed" and isinstance(obj.get("item"), dict):
                item = obj["item"]
                item_type = str(item.get("type", "item"))
                items[item_type] += 1
                if item_type == "command_execution":
                    command = str(item.get("command", ""))
                    tools[command_tool(command)] += 1
                    text = str(item.get("aggregated_output", ""))
                    error = item.get("exit_code") not in (0, None) or item.get("status") == "failed"
                    metrics.handoff_markers += add_text_signals(text, verdicts, statuses)
                    if error:
                        metrics.errors += 1
                    record_item(
                        content_chars,
                        top_items,
                        item_type,
                        item_label(item, item_type),
                        len(text),
                        error,
                    )
                elif item_type == "mcp_tool_call":
                    server = str(item.get("server") or item.get("mcp_server") or "mcp")
                    tool = str(item.get("tool") or item.get("name") or "tool")
                    mcps[f"{server}/{tool}"] += 1
                    text = json.dumps(item, sort_keys=True)
                    metrics.handoff_markers += add_text_signals(text, verdicts, statuses)
                    record_item(
                        content_chars,
                        top_items,
                        item_type,
                        item_label(item, item_type),
                        len(text),
                        bool(item.get("is_error")),
                    )
                else:
                    text = str(item.get("text") or item.get("aggregated_output") or "")
                    metrics.handoff_markers += add_text_signals(text, verdicts, statuses)
                    record_item(
                        content_chars,
                        top_items,
                        item_type,
                        item_label(item, item_type),
                        len(text),
                        bool(item.get("is_error")),
                    )

    metrics.event_counts = dict(events)
    metrics.item_counts = dict(items)
    metrics.tool_counts = dict(tools)
    metrics.mcp_counts = dict(mcps)
    metrics.content_chars = dict(content_chars)
    metrics.top_items = sorted(top_items, key=lambda x: x["chars"], reverse=True)[:10]
    metrics.verdicts = dict(verdicts)
    metrics.statuses = dict(statuses)
    return metrics


def confidence_label(delta_seconds: int | None, exact: bool) -> str:
    if exact:
        return "exact-stem"
    if delta_seconds is None:
        return "no-pair"
    if delta_seconds <= 5 * 60:
        return "within-5m"
    if delta_seconds <= 30 * 60:
        return "within-30m"
    if delta_seconds <= 2 * 60 * 60:
        return "within-2h"
    return "low-confidence"


def pair_outputs(prompts: list[FileInfo], outputs: list[OutputMetrics], max_seconds: int) -> list[PairInfo]:
    by_role: dict[str, list[FileInfo]] = defaultdict(list)
    by_name = {Path(prompt.name).stem: prompt for prompt in prompts}
    for prompt in prompts:
        if prompt.role and prompt.timestamp:
            by_role[prompt.role].append(prompt)
    for role_prompts in by_role.values():
        role_prompts.sort(key=lambda p: timestamp_dt(p) or datetime.min)

    pairs: list[PairInfo] = []
    for output in outputs:
        output_stem = Path(output.name).stem
        exact_prompt = by_name.get(output_stem)
        if exact_prompt:
            pairs.append(
                PairInfo(
                    output=output.path,
                    output_role=output.role or "unknown",
                    output_timestamp=output.timestamp or "",
                    prompt=exact_prompt.path,
                    prompt_timestamp=exact_prompt.timestamp,
                    delta_seconds=0,
                    confidence="exact-stem",
                    prompt_size_bytes=exact_prompt.size_bytes,
                    output_size_bytes=output.size_bytes,
                )
            )
            continue

        candidates = by_role.get(output.role or "", [])
        output_time = timestamp_dt(output)
        if not candidates or output_time is None:
            pairs.append(
                PairInfo(
                    output=output.path,
                    output_role=output.role or "unknown",
                    output_timestamp=output.timestamp or "",
                    prompt=None,
                    prompt_timestamp=None,
                    delta_seconds=None,
                    confidence="no-pair",
                    prompt_size_bytes=None,
                    output_size_bytes=output.size_bytes,
                )
            )
            continue

        prior = []
        for prompt in candidates:
            prompt_time = timestamp_dt(prompt)
            if prompt_time is not None and prompt_time <= output_time:
                prior.append(prompt)
        search = prior or candidates
        assert output_time is not None  # guarded above
        ot = output_time  # bind for lambda capture
        best = min(
            search,
            key=lambda prompt: abs(((timestamp_dt(prompt) or ot) - ot).total_seconds()),
        )
        delta = int(abs(((timestamp_dt(best) or ot) - ot).total_seconds()))
        if delta > max_seconds:
            prompt_path = None
            prompt_timestamp = None
            prompt_size = None
            confidence = "no-pair"
        else:
            prompt_path = best.path
            prompt_timestamp = best.timestamp
            prompt_size = best.size_bytes
            confidence = confidence_label(delta, False)
        pairs.append(
            PairInfo(
                output=output.path,
                output_role=output.role or "unknown",
                output_timestamp=output.timestamp or "",
                prompt=prompt_path,
                prompt_timestamp=prompt_timestamp,
                delta_seconds=delta if prompt_path else None,
                confidence=confidence,
                prompt_size_bytes=prompt_size,
                output_size_bytes=output.size_bytes,
            )
        )
    return pairs


def human_bytes(value: int | None) -> str:
    if value is None:
        return "-"
    size = float(value)
    for unit in ("B", "KB", "MB", "GB"):
        if size < 1024 or unit == "GB":
            return f"{size:.1f}{unit}" if unit != "B" else f"{int(size)}B"
        size /= 1024
    return f"{value}B"


def human_tokens(value: int | None) -> str:
    if not value:
        return "-"
    if value >= 1_000_000:
        return f"{value / 1_000_000:.1f}M"
    if value >= 1_000:
        return f"{value / 1_000:.1f}K"
    return str(value)


def truncate_label(value: str, limit: int = 96) -> str:
    if len(value) <= limit:
        return value
    return value[: limit - 1] + "..."


def top_by_role(files: list[FileInfo], limit: int) -> list[FileInfo]:
    selected: list[FileInfo] = []
    by_role: dict[str, list[FileInfo]] = defaultdict(list)
    for file in files:
        by_role[file.role or "unknown"].append(file)
    for group in by_role.values():
        selected.extend(sorted(group, key=lambda f: f.size_bytes, reverse=True)[:1])
    return sorted(selected, key=lambda f: f.size_bytes, reverse=True)[:limit]


def build_sampling_plan(
    prompts: list[FileInfo],
    outputs: list[OutputMetrics],
    pairs: list[PairInfo],
    limit: int,
) -> list[dict[str, Any]]:
    by_output = {pair.output: pair for pair in pairs}
    candidates: list[tuple[str, OutputMetrics]] = []

    if outputs:
        candidates.append(("largest-output", max(outputs, key=lambda o: o.size_bytes)))
        candidates.append(
            (
                "highest-tool-output-pressure",
                max(
                    outputs,
                    key=lambda o: o.content_chars.get("command_execution", 0)
                    + o.content_chars.get("tool_result", 0)
                    + o.content_chars.get("mcp_tool_call", 0),
                ),
            )
        )
        candidates.append(("most-errors", max(outputs, key=lambda o: o.errors)))
        rejected = [o for o in outputs if o.verdicts.get("REJECTED")]
        if rejected:
            candidates.append(("rejected-output", max(rejected, key=lambda o: o.verdicts["REJECTED"])))
        blocked = [o for o in outputs if o.statuses.get("BLOCKED")]
        if blocked:
            candidates.append(("blocked-output", max(blocked, key=lambda o: o.statuses["BLOCKED"])))
        approved = [o for o in outputs if o.verdicts.get("APPROVED")]
        if approved:
            candidates.append(("approved-output", max(approved, key=lambda o: o.verdicts["APPROVED"])))

    prompt_by_path = {prompt.path: prompt for prompt in prompts}
    pair_with_largest_prompt = max(
        (pair for pair in pairs if pair.prompt_size_bytes),
        key=lambda p: p.prompt_size_bytes or 0,
        default=None,
    )

    plan: list[dict[str, Any]] = []
    seen_outputs: set[str] = set()
    for reason, output in candidates:
        if output.path in seen_outputs:
            continue
        seen_outputs.add(output.path)
        pair = by_output.get(output.path)
        plan.append(
            {
                "reason": reason,
                "output": output.path,
                "prompt": pair.prompt if pair else None,
                "pair_confidence": pair.confidence if pair else "no-pair",
                "output_size": output.size_bytes,
                "prompt_size": pair.prompt_size_bytes if pair else None,
                "signals": {
                    "errors": output.errors,
                    "verdicts": output.verdicts,
                    "statuses": output.statuses,
                    "total_input_tokens": output.total_input_tokens,
                    "output_tokens": output.output_tokens,
                },
            }
        )

    if pair_with_largest_prompt and pair_with_largest_prompt.output not in seen_outputs:
        prompt = prompt_by_path.get(pair_with_largest_prompt.prompt or "")
        plan.append(
            {
                "reason": "largest-paired-prompt",
                "output": pair_with_largest_prompt.output,
                "prompt": pair_with_largest_prompt.prompt,
                "pair_confidence": pair_with_largest_prompt.confidence,
                "output_size": pair_with_largest_prompt.output_size_bytes,
                "prompt_size": prompt.size_bytes if prompt else pair_with_largest_prompt.prompt_size_bytes,
                "signals": {},
            }
        )

    return plan[:limit]


@dataclass
class RoleStat:
    role: str
    prompt_count: int = 0
    output_count: int = 0
    prompt_avg_bytes: int = 0
    prompt_min_bytes: int = 0
    prompt_max_bytes: int = 0
    output_avg_bytes: int = 0
    output_min_bytes: int = 0
    output_max_bytes: int = 0
    output_to_prompt_ratio: float = 0.0
    prompt_size_trend: str = "stable"
    prompt_sizes_over_time: list[dict[str, Any]] = field(default_factory=list)


def detect_trend(sizes: list[int]) -> str:
    if len(sizes) < 4:
        return "too-few-samples"
    first_quarter = sizes[: len(sizes) // 4]
    last_quarter = sizes[-len(sizes) // 4 :]
    avg_first = sum(first_quarter) / len(first_quarter)
    avg_last = sum(last_quarter) / len(last_quarter)
    if avg_first == 0:
        return "stable"
    change = (avg_last - avg_first) / avg_first
    if change > 0.15:
        return "growing"
    if change < -0.15:
        return "shrinking"
    return "stable"


def build_role_stats(prompts: list[FileInfo], outputs: list[OutputMetrics]) -> list[dict[str, Any]]:
    prompt_by_role: dict[str, list[FileInfo]] = defaultdict(list)
    output_by_role: dict[str, list[OutputMetrics]] = defaultdict(list)
    for p in prompts:
        prompt_by_role[p.role or "unknown"].append(p)
    for o in outputs:
        output_by_role[o.role or "unknown"].append(o)

    all_roles = sorted(set(prompt_by_role) | set(output_by_role))
    stats: list[dict[str, Any]] = []

    for role in all_roles:
        rp = prompt_by_role.get(role, [])
        ro = output_by_role.get(role, [])
        rp_sorted = sorted(rp, key=lambda f: f.timestamp or "")
        rp_sizes = [f.size_bytes for f in rp_sorted]
        ro_sizes = [o.size_bytes for o in ro]

        p_avg = sum(rp_sizes) // len(rp_sizes) if rp_sizes else 0
        o_avg = sum(ro_sizes) // len(ro_sizes) if ro_sizes else 0

        stat = RoleStat(
            role=role,
            prompt_count=len(rp),
            output_count=len(ro),
            prompt_avg_bytes=p_avg,
            prompt_min_bytes=min(rp_sizes) if rp_sizes else 0,
            prompt_max_bytes=max(rp_sizes) if rp_sizes else 0,
            output_avg_bytes=o_avg,
            output_min_bytes=min(ro_sizes) if ro_sizes else 0,
            output_max_bytes=max(ro_sizes) if ro_sizes else 0,
            output_to_prompt_ratio=round(o_avg / p_avg, 1) if p_avg else 0.0,
            prompt_size_trend=detect_trend(rp_sizes),
            prompt_sizes_over_time=[{"timestamp": f.timestamp, "size_bytes": f.size_bytes} for f in rp_sorted],
        )
        stats.append(asdict(stat))

    return stats


def summarize(agent_data_dir: Path, max_pair_seconds: int, sample_limit: int) -> dict[str, Any]:
    prompts_dir = agent_data_dir / "agent-prompts"
    outputs_dir = agent_data_dir / "agent-outputs"
    prompts = [parse_file_info(path, agent_data_dir) for path in sorted(prompts_dir.glob("*")) if path.is_file()]
    output_files = [path for path in sorted(outputs_dir.glob("*")) if path.is_file() and path.suffix == ".txt"]
    outputs = [scan_output(path, agent_data_dir) for path in output_files]
    other_outputs = [
        parse_file_info(path, agent_data_dir)
        for path in sorted(outputs_dir.glob("*"))
        if path.is_file() and path.suffix != ".txt"
    ]
    pairs = pair_outputs([prompt for prompt in prompts if prompt.suffix == ".txt"], outputs, max_pair_seconds)
    pair_conf = Counter(pair.confidence for pair in pairs)
    format_counts = Counter(output.log_format for output in outputs)
    prompt_roles = Counter(prompt.role or "unknown" for prompt in prompts)
    output_roles = Counter(output.role or "unknown" for output in outputs)
    suffix_counts = Counter(file.suffix for file in prompts + other_outputs)
    suffix_counts[".txt_outputs"] = len(outputs)

    totals = {
        "prompt_count": len(prompts),
        "output_txt_count": len(outputs),
        "other_output_count": len(other_outputs),
        "prompt_bytes": sum(prompt.size_bytes for prompt in prompts),
        "output_txt_bytes": sum(output.size_bytes for output in outputs),
        "other_output_bytes": sum(file.size_bytes for file in other_outputs),
    }

    top_outputs = sorted(outputs, key=lambda o: o.size_bytes, reverse=True)[:sample_limit]
    top_token_outputs = sorted(outputs, key=lambda o: o.total_input_tokens, reverse=True)[:sample_limit]
    high_tool_pressure = sorted(
        outputs,
        key=lambda o: o.content_chars.get("command_execution", 0)
        + o.content_chars.get("tool_result", 0)
        + o.content_chars.get("mcp_tool_call", 0),
        reverse=True,
    )[:sample_limit]
    top_prompts = sorted(prompts, key=lambda p: p.size_bytes, reverse=True)[:sample_limit]

    aggregate_tools: Counter[str] = Counter()
    aggregate_mcp: Counter[str] = Counter()
    aggregate_verdicts: Counter[str] = Counter()
    aggregate_statuses: Counter[str] = Counter()
    for output in outputs:
        aggregate_tools.update(output.tool_counts)
        aggregate_mcp.update(output.mcp_counts)
        aggregate_verdicts.update(output.verdicts)
        aggregate_statuses.update(output.statuses)

    role_stats = build_role_stats(prompts, outputs)

    return {
        "agent_data_dir": str(agent_data_dir),
        "totals": totals,
        "roles": {
            "prompts": dict(prompt_roles),
            "outputs": dict(output_roles),
        },
        "suffix_counts": dict(suffix_counts),
        "pairing": {
            "max_pair_seconds": max_pair_seconds,
            "confidence_counts": dict(pair_conf),
            "pairs": [asdict(pair) for pair in pairs],
        },
        "format_counts": dict(format_counts),
        "top_prompts": [asdict(prompt) for prompt in top_prompts],
        "largest_prompt_per_role": [asdict(file) for file in top_by_role(prompts, sample_limit)],
        "top_outputs": [asdict(output) for output in top_outputs],
        "top_token_outputs": [asdict(output) for output in top_token_outputs],
        "high_tool_pressure_outputs": [asdict(output) for output in high_tool_pressure],
        "aggregate_tools": dict(aggregate_tools.most_common(sample_limit)),
        "aggregate_mcp": dict(aggregate_mcp.most_common(sample_limit)),
        "aggregate_verdicts": dict(aggregate_verdicts),
        "aggregate_statuses": dict(aggregate_statuses),
        "role_stats": role_stats,
        "sampling_plan": build_sampling_plan(prompts, outputs, pairs, sample_limit),
    }


def render_table(headers: list[str], rows: list[list[str]]) -> str:
    if not rows:
        return "_None._\n"
    lines = [
        "| " + " | ".join(headers) + " |",
        "| " + " | ".join("---" for _ in headers) + " |",
    ]
    for row in rows:
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines) + "\n"


def render_prompt_trends(role_stats: list[dict[str, Any]]) -> list[str]:
    lines: list[str] = []
    interesting = [s for s in role_stats if s["prompt_size_trend"] != "stable" or s["prompt_count"] >= 10]
    interesting.sort(key=lambda s: s["prompt_count"], reverse=True)

    for stat in interesting:
        sizes = stat["prompt_sizes_over_time"]
        if not sizes:
            continue
        role = stat["role"]
        trend = stat["prompt_size_trend"]
        lines.append(
            f"**{role}** ({stat['prompt_count']} prompts, trend: {trend}, "
            f"range: {human_bytes(stat['prompt_min_bytes'])}–{human_bytes(stat['prompt_max_bytes'])})"
        )
        lines.append("")
        step = max(1, len(sizes) // 8)
        sampled = sizes[::step]
        if sizes[-1] not in [s for s in sizes[::step]]:
            sampled.append(sizes[-1])
        lines.append(
            render_table(
                ["Timestamp", "Size"],
                [[s["timestamp"] or "", human_bytes(s["size_bytes"])] for s in sampled],
            )
        )
    if not interesting:
        lines.append("_No roles with ≥10 prompts or non-stable trends._\n")
    return lines


def render_markdown(report: dict[str, Any]) -> str:
    totals = report["totals"]
    lines = [
        "# Context Corpus Index",
        "",
        f"{BRAND_NAME_TITLE} dir: `{report['agent_data_dir']}`",
        "",
        "## Inventory",
        "",
        render_table(
            ["Metric", "Value"],
            [
                ["Prompts", str(totals["prompt_count"])],
                ["Output `.txt` logs", str(totals["output_txt_count"])],
                ["Other output files", str(totals["other_output_count"])],
                ["Prompt bytes", human_bytes(totals["prompt_bytes"])],
                ["Output `.txt` bytes", human_bytes(totals["output_txt_bytes"])],
                ["Output formats", json.dumps(report["format_counts"], sort_keys=True)],
            ],
        ),
        "## Pairing",
        "",
        render_table(
            ["Confidence", "Count"],
            [[confidence, str(count)] for confidence, count in sorted(report["pairing"]["confidence_counts"].items())],
        ),
        "## Largest Prompts",
        "",
        render_table(
            ["Size", "Role", "Timestamp", "Path"],
            [
                [
                    human_bytes(prompt["size_bytes"]),
                    prompt["role"] or "unknown",
                    prompt["timestamp"] or "",
                    f"`{prompt['path']}`",
                ]
                for prompt in report["top_prompts"]
            ],
        ),
        "## Largest Outputs",
        "",
        render_table(
            ["Size", "Input", "Output", "Errors", "Format", "Role", "Path"],
            [
                [
                    human_bytes(output["size_bytes"]),
                    human_tokens(output["total_input_tokens"]),
                    human_tokens(output["output_tokens"]),
                    str(output["errors"]),
                    output["log_format"],
                    output["role"] or "unknown",
                    f"`{output['path']}`",
                ]
                for output in report["top_outputs"]
            ],
        ),
        "## High Tool-Output Pressure",
        "",
        render_table(
            ["Tool chars", "Errors", "Format", "Top tool/command", "Top item", "Path"],
            [
                [
                    human_bytes(
                        output["content_chars"].get("command_execution", 0)
                        + output["content_chars"].get("tool_result", 0)
                        + output["content_chars"].get("mcp_tool_call", 0)
                    ),
                    str(output["errors"]),
                    output["log_format"],
                    f"`{truncate_label(output['top_items'][0]['label'])}`" if output["top_items"] else "-",
                    (f"{output['top_items'][0]['type']} {human_bytes(output['top_items'][0]['chars'])}")
                    if output["top_items"]
                    else "-",
                    f"`{output['path']}`",
                ]
                for output in report["high_tool_pressure_outputs"]
            ],
        ),
        "## Outcome Signal Mentions",
        "",
        render_table(
            ["Signal", "Counts"],
            [
                ["Verdict mentions", json.dumps(report["aggregate_verdicts"], sort_keys=True)],
                ["Status mentions", json.dumps(report["aggregate_statuses"], sort_keys=True)],
            ],
        ),
        "## Role Distribution",
        "",
        render_table(
            ["Role", "Prompts", "Outputs", "Prompt Avg", "Output Avg", "Out/Prompt", "Prompt Trend"],
            [
                [
                    stat["role"],
                    str(stat["prompt_count"]),
                    str(stat["output_count"]),
                    human_bytes(stat["prompt_avg_bytes"]),
                    human_bytes(stat["output_avg_bytes"]),
                    f"{stat['output_to_prompt_ratio']}x",
                    stat["prompt_size_trend"],
                ]
                for stat in sorted(report["role_stats"], key=lambda s: s["output_avg_bytes"], reverse=True)
            ],
        ),
        "## Prompt Size Trends",
        "",
        *(render_prompt_trends(report["role_stats"])),
        "## Common Tools",
        "",
        render_table(
            ["Tool", "Calls"],
            [[tool, str(count)] for tool, count in report["aggregate_tools"].items()],
        ),
        "## MCP Usage",
        "",
        render_table(
            ["Server/Tool", "Calls"],
            [[tool, str(count)] for tool, count in report["aggregate_mcp"].items()],
        ),
        "## Sampling Plan",
        "",
        render_table(
            ["Reason", "Pair", "Output", "Prompt", "Signals"],
            [
                [
                    item["reason"],
                    item["pair_confidence"],
                    f"`{item['output']}`",
                    f"`{item['prompt']}`" if item.get("prompt") else "-",
                    json.dumps(item["signals"], sort_keys=True),
                ]
                for item in report["sampling_plan"]
            ],
        ),
    ]
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser(
        description=(
            f"Index {BRAND_PROJECT_DIRNAME}/agent-prompts and "
            f"{BRAND_PROJECT_DIRNAME}/agent-outputs for context-engineering audits."
        )
    )
    parser.add_argument(
        "path",
        type=Path,
        help=(f"Path to a project root containing {BRAND_PROJECT_DIRNAME}, or to a {BRAND_PROJECT_DIRNAME} directory."),
    )
    parser.add_argument(
        "--max-pair-minutes",
        type=int,
        default=120,
        help="Maximum same-role prompt/output timestamp delta to count as a pair.",
    )
    parser.add_argument(
        "--sample-limit",
        type=int,
        default=15,
        help="Maximum rows in top lists and sampling plan.",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Emit full JSON instead of compact Markdown.",
    )
    args = parser.parse_args()

    agent_data_dir = resolve_agent_data_dir(args.path)
    report = summarize(agent_data_dir, args.max_pair_minutes * 60, args.sample_limit)
    if args.json:
        print(json.dumps(report, indent=2, sort_keys=True))
    else:
        print(render_markdown(report))


if __name__ == "__main__":
    main()
