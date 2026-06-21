#!/usr/bin/env python3
"""Analyze §BRAND_NAME_TITLE§ blackboard task state for recurring task frictions."""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any

import yaml

FRICTION_STATUSES = {"INTEGRATION_FAILED", "BLOCKED", "SUPERSEDED", "ABANDONED"}
REJECTION_EVENTS = {"rejected", "review_verdict_rejected"}


def _tasks_from_state(state: dict[str, Any]) -> list[dict[str, Any]]:
    tasks = state.get("tasks", [])
    if isinstance(tasks, dict):
        return [task for task in tasks.values() if isinstance(task, dict)]
    if isinstance(tasks, list):
        return [task for task in tasks if isinstance(task, dict)]
    return []


def _history(task: dict[str, Any]) -> list[dict[str, Any]]:
    history = task.get("history") or []
    return [event for event in history if isinstance(event, dict)] if isinstance(history, list) else []


def rejection_events(task: dict[str, Any]) -> list[dict[str, Any]]:
    return [event for event in _history(task) if event.get("event") in REJECTION_EVENTS]


def rejection_count(task: dict[str, Any]) -> int:
    cycles = task.get("review_cycles_total")
    if cycles is not None:
        try:
            return int(cycles)
        except (TypeError, ValueError):
            pass
    return len(rejection_events(task))


def _last_event(task: dict[str, Any], event_name: str) -> dict[str, Any] | None:
    return next((event for event in reversed(_history(task)) if event.get("event") == event_name), None)


def _last_rejection(task: dict[str, Any]) -> dict[str, Any] | None:
    return next((event for event in reversed(_history(task)) if event.get("event") in REJECTION_EVENTS), None)


def _summarize_text(value: Any, limit: int = 180) -> str:
    if value is None:
        return ""
    text = " ".join(str(value).split())
    return text[: limit - 3] + "..." if len(text) > limit else text


def _task_id(task: dict[str, Any]) -> str:
    return str(task.get("id") or "<unknown>")


def _supersede_bucket(reason: str) -> str:
    lowered = reason.lower()
    if "artifact" in lowered or "stale" in lowered or "ref" in lowered:
        return "artifact/ref drift"
    if "hypothesis" in lowered or "split" in lowered:
        return "hypothesis exhaustion/split"
    if "depend" in lowered:
        return "dependency repair"
    if "unknown" == lowered or not lowered:
        return "unknown"
    return "other"


def analyze_state(state: dict[str, Any]) -> dict[str, Any]:
    tasks = _tasks_from_state(state)
    status_counts = Counter(str(task.get("status") or "<missing>") for task in tasks)
    high_rejection = [task for task in tasks if rejection_count(task) >= 4]
    terminal = [task for task in tasks if task.get("status") in FRICTION_STATUSES]

    superseded_buckets: Counter[str] = Counter()
    superseded_examples: dict[str, list[dict[str, str]]] = defaultdict(list)
    for task in tasks:
        if task.get("status") != "SUPERSEDED":
            continue
        event = _last_event(task, "superseded") or {}
        reason = _summarize_text(event.get("reason") or task.get("blocked_reason") or "unknown", 260)
        bucket = _supersede_bucket(reason)
        superseded_buckets[bucket] += 1
        if len(superseded_examples[bucket]) < 5:
            superseded_examples[bucket].append({"id": _task_id(task), "reason": reason})

    return {
        "task_count": len(tasks),
        "status_counts": dict(sorted(status_counts.items())),
        "friction_counts": {
            "high_rejection_tasks": len(high_rejection),
            "INTEGRATION_FAILED": status_counts.get("INTEGRATION_FAILED", 0),
            "BLOCKED": status_counts.get("BLOCKED", 0),
            "SUPERSEDED": status_counts.get("SUPERSEDED", 0),
            "ABANDONED": status_counts.get("ABANDONED", 0),
        },
        "high_rejection_tasks": [
            {
                "id": _task_id(task),
                "status": str(task.get("status") or ""),
                "type": str(task.get("type") or ""),
                "role_pair": str(task.get("role_pair") or ""),
                "rejections": rejection_count(task),
                "attempt": task.get("attempt"),
                "iteration": task.get("iteration"),
                "last_rejection_agent": str((_last_rejection(task) or {}).get("agent") or ""),
                "last_rejection_summary": _summarize_text((_last_rejection(task) or {}).get("reason"), 220),
            }
            for task in high_rejection
        ],
        "terminal_tasks": [
            {
                "id": _task_id(task),
                "status": str(task.get("status") or ""),
                "type": str(task.get("type") or ""),
                "role_pair": str(task.get("role_pair") or ""),
                "attempt": task.get("attempt"),
                "iteration": task.get("iteration"),
                "blocked_reason": _summarize_text(task.get("blocked_reason"), 220),
                "last_event": str((_history(task)[-1] if _history(task) else {}).get("event") or ""),
                "last_event_summary": _summarize_text(
                    (_history(task)[-1] if _history(task) else {}).get("reason"),
                    220,
                ),
                "superseded_by": task.get("superseded_by") or [],
                "failed_by": task.get("failed_by") or [],
            }
            for task in terminal
        ],
        "superseded_reason_buckets": dict(superseded_buckets.most_common()),
        "superseded_reason_examples": dict(superseded_examples),
    }


def load_state(path: str) -> dict[str, Any]:
    with Path(path).open(encoding="utf-8") as f:
        loaded = yaml.safe_load(f) or {}
    if not isinstance(loaded, dict):
        raise ValueError(f"{path} must contain a YAML mapping")
    return loaded


def render_report(analysis: dict[str, Any], source: str) -> str:
    lines = [
        "=" * 72,
        "STATE FRICTION INVENTORY",
        "=" * 72,
        f"  Source: {source}",
        f"  Tasks:  {analysis['task_count']}",
        "",
        "STATUS COUNTS",
    ]
    for status, count in analysis["status_counts"].items():
        lines.append(f"  {status:<22s} {count:>5d}")

    lines.extend(["", "FRICTION COUNTS"])
    for category, count in analysis["friction_counts"].items():
        lines.append(f"  {category:<22s} {count:>5d}")

    lines.extend(
        [
            "",
            "HIGH-REJECTION TASKS",
            f"  {'Task':<55s} {'Status':<18s} {'Rej':>3s} {'Last reviewer':<22s} Summary",
            f"  {'-' * 55} {'-' * 18} {'-' * 3} {'-' * 22} {'-' * 40}",
        ]
    )
    high_rejection = analysis["high_rejection_tasks"]
    if high_rejection:
        for task in high_rejection:
            lines.append(
                f"  {task['id']:<55.55s} {task['status']:<18.18s} {task['rejections']:>3d}"
                f" {task['last_rejection_agent']:<22.22s} {task['last_rejection_summary']}"
            )
    else:
        lines.append("  (none)")

    lines.extend(["", "TERMINAL / STALLED TASKS"])
    terminal = analysis["terminal_tasks"]
    if terminal:
        for status in sorted(FRICTION_STATUSES):
            tasks = [task for task in terminal if task["status"] == status]
            lines.append(f"  {status}: {len(tasks)}")
            for task in tasks[:20]:
                summary = task["blocked_reason"] or task["last_event_summary"]
                lines.append(f"    - {task['id']} ({task['type'] or task['role_pair']}): {summary}")
            if len(tasks) > 20:
                lines.append(f"    ... {len(tasks) - 20} more")
    else:
        lines.append("  (none)")

    lines.extend(["", "SUPERSEDED REASON BUCKETS"])
    buckets = analysis["superseded_reason_buckets"]
    if buckets:
        for bucket, count in buckets.items():
            lines.append(f"  {bucket:<28s} {count:>5d}")
            for example in analysis["superseded_reason_examples"].get(bucket, []):
                lines.append(f"    - {example['id']}: {example['reason']}")
    else:
        lines.append("  (none)")

    return "\n".join(lines) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Analyze §BRAND_NAME_TITLE§ §BRAND_PROJECT_DIRNAME§/state.yaml task friction."
    )
    parser.add_argument("state_file", help="path to §BRAND_PROJECT_DIRNAME§/state.yaml")
    parser.add_argument("--json", action="store_true", help="emit structured JSON instead of text")
    args = parser.parse_args()

    try:
        analysis = analyze_state(load_state(args.state_file))
    except (OSError, ValueError, yaml.YAMLError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        sys.exit(1)

    if args.json:
        print(json.dumps(analysis, indent=2))
    else:
        print(render_report(analysis, args.state_file))


if __name__ == "__main__":
    main()
