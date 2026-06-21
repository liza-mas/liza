#!/usr/bin/env python3
"""Extract bounded evidence windows from §BRAND_NAME_TITLE§ agent logs.

This complements analyze-log.py. The analyzer gives full session summaries;
this script keeps the decision-relevant event sequence and trims large payloads
so a human can refine a specific suspicion without reading raw logs wholesale.
"""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

_BENIGN_EXIT1_COMMANDS = frozenset({"rg", "grep", "egrep", "fgrep", "ag", "ack", "diff"})


@dataclass
class EvidenceEvent:
    index: int
    kind: str
    label: str
    detail: str = ""
    result: str = ""
    is_error: bool = False

    def searchable_text(self) -> str:
        return "\n".join((self.kind, self.label, self.detail, self.result))


def trim_text(text: str, max_chars: int) -> str:
    """Return text capped to max_chars while preserving both ends."""
    normalized = text.replace("\r\n", "\n")
    if len(normalized) <= max_chars:
        return normalized
    if max_chars < 40:
        return normalized[:max_chars]
    keep = max_chars // 2
    omitted = len(normalized) - (keep * 2)
    return f"{normalized[:keep]}\n<trimmed {omitted} chars>\n{normalized[-keep:]}"


def compact_json(value: Any) -> str:
    if isinstance(value, str):
        return value
    return json.dumps(value, ensure_ascii=False, sort_keys=True)


def command_name(command: str) -> str:
    stripped = unwrap_shell_command(command)
    parts = stripped.split()
    if not parts:
        return ""
    if parts[0] == "rtk" and len(parts) > 1:
        return f"rtk {parts[1]}"
    return parts[0].rsplit("/", 1)[-1]


def unwrap_shell_command(command: str) -> str:
    for prefix in ("/usr/bin/zsh -lc ", "/bin/bash -lc ", "/bin/sh -c "):
        if command.startswith(prefix):
            return command[len(prefix) :].strip().strip("'\"")
    return command


def is_benign_command_exit(command: str, exit_code: int) -> bool:
    parts = command_name(command).split()
    name = parts[1] if parts and parts[0] == "rtk" and len(parts) > 1 else parts[0] if parts else ""
    return exit_code == 1 and name in _BENIGN_EXIT1_COMMANDS


def read_json_lines(path: Path) -> list[dict[str, Any]]:
    events: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        if not line.strip():
            continue
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return events


def parse_sparse(objects: list[dict[str, Any]]) -> list[EvidenceEvent]:
    result: list[EvidenceEvent] = []
    next_index = 1

    def add(kind: str, label: str, detail: str = "", body: str = "", is_error: bool = False) -> None:
        nonlocal next_index
        result.append(EvidenceEvent(next_index, kind, label, detail, body, is_error))
        next_index += 1

    for obj in objects:
        event_type = obj.get("type", "")
        if event_type == "item.completed":
            item = obj.get("item", {})
            item_type = item.get("type", "")

            if item_type == "agent_message":
                add("assistant", "agent_message", body=item.get("text", ""))
            elif item_type == "reasoning":
                add("assistant", "reasoning", body=item.get("text", ""))
            elif item_type == "command_execution":
                command = item.get("command", "")
                output = item.get("aggregated_output", "") or ""
                exit_code = item.get("exit_code", 0)
                add(
                    "tool",
                    command_name(command),
                    unwrap_shell_command(command),
                    output,
                    exit_code != 0 and not is_benign_command_exit(command, exit_code),
                )
            elif item_type == "mcp_tool_call":
                server = item.get("server", "")
                tool = item.get("tool", "")
                label = f"{server}/{tool}" if server else tool
                detail = compact_json(item.get("arguments", ""))
                body = compact_json(item.get("result", ""))
                add("tool", label, detail, body, item.get("status") == "failed")
            elif item_type == "file_change":
                paths = [change.get("path", "") for change in item.get("changes", [])]
                add("file_change", "file_change", ", ".join(paths))

        elif event_type == "turn.failed":
            error = obj.get("error", {})
            body = compact_json(error)
            add("error", "turn.failed", body=body, is_error=True)

    return result


def parse_rich(objects: list[dict[str, Any]]) -> list[EvidenceEvent]:
    result: list[EvidenceEvent] = []
    pending_tools: dict[str, tuple[str, str]] = {}
    next_index = 1

    def add(kind: str, label: str, detail: str = "", body: str = "", is_error: bool = False) -> None:
        nonlocal next_index
        result.append(EvidenceEvent(next_index, kind, label, detail, body, is_error))
        next_index += 1

    for obj in objects:
        event_type = obj.get("type", "")
        if event_type == "assistant":
            message = obj.get("message", {})
            for block in message.get("content", []):
                block_type = block.get("type", "")
                if block_type == "text":
                    add("assistant", "text", body=block.get("text", ""))
                elif block_type == "thinking":
                    add("assistant", "thinking", body=block.get("thinking", ""))
                elif block_type == "tool_use":
                    tool_id = block.get("id", "")
                    name = block.get("name", "tool")
                    detail = compact_json(block.get("input", {}))
                    pending_tools[tool_id] = (name, detail)
                    add("tool_request", name, detail)
        elif event_type == "user":
            message = obj.get("message", {})
            for block in message.get("content", []):
                if not isinstance(block, dict) or block.get("type") != "tool_result":
                    continue
                tool_id = block.get("tool_use_id", "")
                name, detail = pending_tools.pop(tool_id, ("tool_result", ""))
                content = block.get("content", "")
                if isinstance(content, list):
                    body = "\n".join(part.get("text", "") if isinstance(part, dict) else str(part) for part in content)
                else:
                    body = str(content)
                add("tool_result", name, detail, body, bool(block.get("is_error")))

    return result


def detect_format(objects: list[dict[str, Any]]) -> str:
    for obj in objects[:10]:
        event_type = obj.get("type", "")
        if event_type == "system":
            return "rich"
        if event_type == "thread.started":
            return "sparse"
    return "unknown"


def parse_events(path: Path) -> list[EvidenceEvent]:
    objects = read_json_lines(path)
    fmt = detect_format(objects)
    if fmt == "rich":
        return parse_rich(objects)
    if fmt == "sparse":
        return parse_sparse(objects)
    return []


def windows_around_errors(events: list[EvidenceEvent], radius: int, task: str | None) -> list[list[EvidenceEvent]]:
    spans: list[tuple[int, int]] = []
    for i, event in enumerate(events):
        if not event.is_error:
            continue
        start = max(0, i - radius)
        end = min(len(events), i + radius + 1)
        window = events[start:end]
        if task and not any(task in e.searchable_text() for e in window):
            continue
        spans.append((start, end))

    merged_spans: list[tuple[int, int]] = []
    for start, end in spans:
        if not merged_spans or start > merged_spans[-1][1]:
            merged_spans.append((start, end))
            continue
        previous_start, previous_end = merged_spans[-1]
        merged_spans[-1] = (previous_start, max(previous_end, end))

    return [events[start:end] for start, end in merged_spans]


def render_event(event: EvidenceEvent, center_index: int, max_field: int) -> str:
    offset = event.index - center_index
    marker = "ERROR" if event.is_error else ""
    lines = [f"{offset:+d} event {event.index}: {event.kind} {marker}".rstrip()]
    if event.label:
        lines.append(f"  label: {trim_text(event.label, max_field)}")
    if event.detail:
        lines.append(f"  detail: {trim_text(event.detail, max_field)}")
    if event.result:
        lines.append(f"  result: {trim_text(event.result, max_field)}")
    return "\n".join(lines)


def render_error_windows(path: Path, windows: list[list[EvidenceEvent]], max_field: int) -> str:
    chunks: list[str] = []
    for number, window in enumerate(windows, 1):
        errors = [event for event in window if event.is_error]
        center = errors[0]
        error_labels = ", ".join(f"{event.index}:{event.label}" for event in errors)
        chunks.append(f"ERROR CLUSTER {number}")
        chunks.append(f"log: {path}")
        chunks.append(f"errors: {error_labels}")
        chunks.append("")
        chunks.extend(render_event(event, center.index, max_field) for event in window)
        chunks.append("")
    return "\n".join(chunks).rstrip()


def render_json(path: Path, windows: list[list[EvidenceEvent]], max_field: int) -> str:
    payload = {
        "log": str(path),
        "windows": [
            [
                {
                    **asdict(event),
                    "label": trim_text(event.label, max_field),
                    "detail": trim_text(event.detail, max_field),
                    "result": trim_text(event.result, max_field),
                }
                for event in window
            ]
            for window in windows
        ],
    }
    return json.dumps(payload, ensure_ascii=False, indent=2)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("logs", nargs="+", help="NDJSON agent log files to query")
    parser.add_argument("--around-errors", type=int, metavar="N", help="show N events before/after each error")
    parser.add_argument("--task", help="only include windows whose bounded text mentions this task id")
    parser.add_argument("--max-field", type=int, default=800, help="maximum displayed chars per field")
    parser.add_argument("--json", action="store_true", help="emit JSON instead of text")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.around_errors is None:
        print("query-log.py currently requires --around-errors N", file=sys.stderr)
        return 2

    outputs: list[str] = []
    for raw_path in args.logs:
        path = Path(raw_path)
        if not path.exists():
            print(f"WARNING: File not found: {path}", file=sys.stderr)
            continue
        events = parse_events(path)
        windows = windows_around_errors(events, args.around_errors, args.task)
        rendered = (
            render_json(path, windows, args.max_field)
            if args.json
            else render_error_windows(path, windows, args.max_field)
        )
        if rendered:
            outputs.append(rendered)

    if outputs:
        print("\n\n".join(outputs))
    return 0


if __name__ == "__main__":
    sys.exit(main())
