#!/usr/bin/env python3
"""Display nonterminal task dependency trees with longest chains highlighted.

Prefer CLI snapshots: computed waiting_on blockers include provider prerequisites.
Raw YAML state snapshots are also accepted, using PyYAML when needed. Chain length
counts existing nonterminal tasks, not time or future tasks. This is a dependency
view, not a claimability assessment.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from graphlib import CycleError, TopologicalSorter
from pathlib import Path
from typing import Any

TERMINAL = {"MERGED", "ABANDONED", "SUPERSEDED"}


def parse_snapshot(raw: str) -> Any:
    """Read saved JSON or YAML without making PyYAML mandatory for JSON queries."""
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        try:
            import yaml
        except ImportError as exc:
            raise ValueError("YAML input requires PyYAML; install it with: python -m pip install pyyaml") from exc
        try:
            return yaml.safe_load(raw)
        except yaml.YAMLError as exc:
            raise ValueError(f"invalid JSON/YAML snapshot: {exc}") from exc


def task_rows(payload: Any) -> list[dict[str, Any]]:
    """Accept a CLI result, state mapping, or task list; reject uncertain input."""
    if isinstance(payload, dict) and "ok" in payload:
        if payload["ok"] is not True:
            raise ValueError(f"task query failed: {payload.get('error', 'unknown error')}")
        for warning in payload.get("warnings") or []:
            print(f"WARNING: {warning}", file=sys.stderr)
        payload = payload.get("result")
    elif isinstance(payload, dict) and "tasks" in payload:
        payload = payload["tasks"]
    if not isinstance(payload, list):
        raise ValueError("expected a JSON/YAML task list, state mapping with tasks, or successful CLI result envelope")
    seen = set()
    for row in payload:
        if not isinstance(row, dict):
            raise ValueError("each task must be an object")
        for field in ("id", "status"):
            if not isinstance(row.get(field), str) or not row[field].strip():
                raise ValueError(f"each task requires a nonempty {field}")
        if row["id"] in seen:
            raise ValueError(f"duplicate task ID: {row['id']}")
        seen.add(row["id"])
    return payload


def string_list(value: Any, field: str) -> list[str]:
    if value is None:
        return []
    if not isinstance(value, list) or any(not isinstance(item, str) or not item.strip() for item in value):
        raise ValueError(f"{field} must be a list of nonempty task IDs")
    return value


def dependency_graph(rows: list[dict[str, Any]]) -> tuple[dict[str, set[str]], list[str]]:
    """Return task -> prerequisites, limited to nonterminal tasks, plus diagnostics."""
    all_tasks = {row["id"]: row for row in rows}
    tasks = {key: row for key, row in all_tasks.items() if row["status"] not in TERMINAL}
    graph: dict[str, set[str]] = {key: set() for key in tasks}
    warnings: set[str] = set()
    for key, row in tasks.items():
        dependencies = set(string_list(row.get("depends_on"), f"{key}.depends_on"))
        wait = row.get("waiting_on")
        if wait is None and (row.get("provider_dependencies") or row.get("provider_reservations")):
            warnings.add(f"{key}: provider prerequisites not resolved; use CLI JSON with computed waiting_on")
        if wait is not None:
            if not isinstance(wait, dict) or not isinstance(wait.get("unmet"), list):
                raise ValueError(f"{key}.waiting_on requires an unmet list")
            # Use immediate blockers, not the flattened heads (which lose edges).
            for unmet in wait["unmet"]:
                if not isinstance(unmet, dict) or not isinstance(unmet.get("dependency"), str):
                    raise ValueError(f"{key}.waiting_on has an invalid unmet dependency")
                blockers = string_list(unmet.get("blocking"), f"{key}.waiting_on.blocking")
                dependencies.update(blockers)
                if not blockers:
                    warnings.add(f"{key}: unresolved {unmet['dependency']}: {unmet.get('reason') or unmet.get('kind')}")
        for dependency in dependencies:
            if dependency in tasks:
                graph[key].add(dependency)
            elif dependency not in all_tasks:
                warnings.add(f"{key}: missing dependency {dependency} (not counted)")
            elif all_tasks[dependency]["status"] != "MERGED":
                warnings.add(f"{key}: dependency {dependency} is {all_tasks[dependency]['status']} (excluded)")
            elif wait and any(dependency in (entry.get("blocking") or []) for entry in wait["unmet"]):
                warnings.add(f"{key}: MERGED provider {dependency} still holds admission (not counted)")
    return graph, sorted(warnings)


def render_tree(rows: list[dict[str, Any]]) -> str:
    graph, warnings = dependency_graph(rows)
    try:
        order = list(TopologicalSorter(graph).static_order())
    except CycleError as exc:
        raise ValueError("dependency cycle: " + " -> ".join(exc.args[1])) from exc
    tasks = {row["id"]: row for row in rows}
    children: dict[str, set[str]] = {key: set() for key in graph}
    for consumer, prerequisites in graph.items():
        for prerequisite in prerequisites:
            children[prerequisite].add(consumer)
    length: dict[str, int] = {}
    for key in reversed(order):
        length[key] = 1 + max((length[child] for child in children[key]), default=0)
    longest = max(length.values(), default=0)
    depth: dict[str, int] = {}
    for key in order:
        depth[key] = 1 + max((depth[parent] for parent in graph[key]), default=0)

    def ranked(ids: Any) -> list[str]:
        return sorted(ids, key=lambda key: (-length[key], key))

    lines = [
        f"Nonterminal tasks: {len(graph)} | Longest chain: {longest} task(s)",
        "Prerequisite -> consumers; * = longest chain (task count, not duration).",
    ]
    lines.extend(f"WARNING: {warning}" for warning in warnings)
    if warnings:
        lines.append("Lengths cover only the known nonterminal graph; unresolved holds are not counted.")
    if not graph:
        lines.append("(no nonterminal tasks)")
    expanded: set[str] = set()
    # Expand each task once; references preserve all edges without exponential output.
    roots = ranked(key for key, prerequisites in graph.items() if not prerequisites)
    stack = [(key, "", "", length[key] == longest) for key in reversed(roots)]
    while stack:
        key, prefix, connector, critical = stack.pop()
        reference = key in expanded
        defer = not critical and depth[key] + length[key] - 1 == longest and not reference
        label = f"{'*' if critical else ' '} {key} [{tasks[key]['status']}]"
        suffix = " (see above)" if reference else " (see longest-chain occurrence)" if defer else ""
        lines.append(prefix + connector + label + suffix)
        if reference or defer:
            continue
        expanded.add(key)
        next_prefix = prefix + ("    " if connector == "`-- " else "|   " if connector else "")
        dependents = ranked(children[key])
        for index in reversed(range(len(dependents))):
            child = dependents[index]
            stack.append(
                (
                    child,
                    next_prefix,
                    "`-- " if index == len(dependents) - 1 else "+-- ",
                    critical and length[child] == length[key] - 1,
                )
            )
    return "\n".join(lines) + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=(__doc__ or "").splitlines()[0])
    parser.add_argument("--project-root", type=Path, default=Path.cwd())
    parser.add_argument("--binary", default="§BRAND_BINARY_NAME§")
    parser.add_argument("--tasks-file", type=Path, help="saved CLI JSON or YAML state snapshot; '-' for stdin")
    args = parser.parse_args(argv)
    try:
        if args.tasks_file:
            raw = sys.stdin.read() if str(args.tasks_file) == "-" else args.tasks_file.read_text(encoding="utf-8")
            payload = parse_snapshot(raw)
        else:
            result = subprocess.run(
                [args.binary, "-C", str(args.project_root), "get", "tasks", "--all", "--summary", "--json"],
                capture_output=True,
                text=True,
                check=False,
                timeout=60,
            )
            if result.returncode:
                raise ValueError(
                    f"task query exited {result.returncode}: {result.stderr.strip() or result.stdout.strip()}"
                )
            raw = result.stdout
            payload = json.loads(raw)
        report = render_tree(task_rows(payload))
    except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    print(report, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
