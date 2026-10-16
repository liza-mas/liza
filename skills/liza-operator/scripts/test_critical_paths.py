from __future__ import annotations

import builtins
import importlib.util
import json
import subprocess
import sys
from pathlib import Path
from typing import Any

import pytest

SCRIPT = Path(__file__).with_name("critical-paths.py")
spec = importlib.util.spec_from_file_location("critical_paths", SCRIPT)
assert spec and spec.loader
cp = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cp)


def task(key: str, status: str = "DRAFT_CODE", dependencies: list[str] | None = None, **extra: Any) -> dict[str, Any]:
    return {"id": key, "status": status, "depends_on": dependencies or [], **extra}


def test_all_tied_longest_chains_and_shorter_branch() -> None:
    rows = [task("a"), task("b", dependencies=["a"]), task("c", dependencies=["a"])]
    rows += [task("d", dependencies=["b", "c"]), task("short", dependencies=["a"]), task("alone")]
    report = cp.render_tree(rows)
    assert report == (
        "Nonterminal tasks: 6 | Longest chain: 3 task(s)\n"
        "Prerequisite -> consumers; * = longest chain (task count, not duration).\n"
        "* a [DRAFT_CODE]\n"
        "+-- * b [DRAFT_CODE]\n"
        "|   `-- * d [DRAFT_CODE]\n"
        "+-- * c [DRAFT_CODE]\n"
        "|   `-- * d [DRAFT_CODE] (see above)\n"
        "`--   short [DRAFT_CODE]\n"
        "  alone [DRAFT_CODE]\n"
    )
    assert report == cp.render_tree(list(reversed(rows)))


def test_only_the_three_named_statuses_are_excluded() -> None:
    rows = [task("merged", "MERGED"), task("abandoned", "ABANDONED"), task("superseded", "SUPERSEDED")]
    rows += [task("blocked", "BLOCKED", ["merged"]), task("failed", "INTEGRATION_FAILED", ["blocked"])]
    rows += [task("future", "NEW_PIPELINE_STATUS", ["failed"])]
    report = cp.render_tree(rows)
    assert "Nonterminal tasks: 3 | Longest chain: 3" in report
    assert "* blocked [BLOCKED]" in report
    assert "* failed [INTEGRATION_FAILED]" in report
    assert "* future [NEW_PIPELINE_STATUS]" in report
    assert "merged [MERGED]" not in report
    assert "abandoned [ABANDONED]" not in report
    assert "superseded [SUPERSEDED]" not in report


def test_provider_wait_uses_immediate_blocker_instead_of_flattened_head() -> None:
    rows = [task("plan", "MERGED"), task("head"), task("child", dependencies=["head"])]
    rows += [
        task(
            "consumer",
            waiting_on={
                "unmet": [{"dependency": "plan", "blocking": ["child"], "kind": "unsatisfied_pending"}],
                "heads": [{"id": "head"}],
            },
        )
    ]
    report = cp.render_tree(rows)
    assert "Longest chain: 3" in report
    assert "* head [DRAFT_CODE]\n`-- * child [DRAFT_CODE]\n    `-- * consumer [DRAFT_CODE]" in report


def test_missing_retired_and_unresolved_provider_holds_are_diagnosed() -> None:
    rows = [task("old", "SUPERSEDED"), task("gone", "ABANDONED"), task("plan", "MERGED")]
    rows += [
        task(
            "consumer",
            dependencies=["old", "gone", "missing"],
            waiting_on={
                "unmet": [
                    {"dependency": "plan", "blocking": ["plan"], "reason": "handoff held"},
                    {"dependency": "bad", "kind": "invalid_provider", "reason": "retired provider"},
                ]
            },
        )
    ]
    report = cp.render_tree(rows)
    for message in (
        "dependency old is SUPERSEDED (excluded)",
        "dependency gone is ABANDONED (excluded)",
        "missing dependency missing (not counted)",
        "MERGED provider plan still holds admission (not counted)",
        "unresolved bad: retired provider",
        "unresolved holds are not counted",
    ):
        assert message in report
    assert "Nonterminal tasks: 1 | Longest chain: 1" in report


def test_shared_subtrees_are_bounded_and_shorter_occurrences_are_not_highlighted() -> None:
    rows = [task("a"), task("b", dependencies=["a"]), task("c", dependencies=["a", "b"])]
    rows += [task("d", dependencies=["c"])]
    report = cp.render_tree(rows)
    assert report.count("d [DRAFT_CODE]") == 1
    assert "`--   c [DRAFT_CODE] (see above)" in report
    assert "Longest chain: 4" in report


def test_deep_chain_does_not_hit_python_recursion_limit() -> None:
    rows = [task(str(i), dependencies=[str(i - 1)] if i else []) for i in range(1100)]
    report = cp.render_tree(rows)
    assert "Longest chain: 1100" in report
    assert report.rstrip().endswith("* 1099 [DRAFT_CODE]")


def test_short_branch_does_not_hide_another_roots_critical_shared_subtree() -> None:
    rows = [task("a"), task("b", dependencies=["a"]), task("c", dependencies=["b"])]
    rows += [task("d", dependencies=["c"]), task("z"), task("y", dependencies=["z"])]
    rows += [task("shared", dependencies=["a", "y"]), task("end", dependencies=["shared"])]
    report = cp.render_tree(rows)
    assert "Longest chain: 4" in report
    assert "  shared [DRAFT_CODE] (see longest-chain occurrence)" in report
    assert "* z [DRAFT_CODE]\n`-- * y [DRAFT_CODE]\n    `-- * shared [DRAFT_CODE]\n        `-- * end" in report
    assert report.count("end [DRAFT_CODE]") == 1


@pytest.mark.parametrize("rows", [[], [task("done", "MERGED")]])
def test_no_nonterminal_tasks(rows: list[dict[str, Any]]) -> None:
    assert "Longest chain: 0" in cp.render_tree(rows)
    assert cp.render_tree(rows).endswith("(no nonterminal tasks)\n")


@pytest.mark.parametrize("deps", [["a"], ["b"]])
def test_cycle_fails_with_explicit_diagnostic(deps: list[str]) -> None:
    with pytest.raises(ValueError, match="dependency cycle: .*a"):
        cp.render_tree([task("a", dependencies=deps), task("b", dependencies=["a"])])


@pytest.mark.parametrize(
    "payload, message",
    [
        ({"ok": False, "error": "denied"}, "task query failed"),
        ({"ok": True, "result": None}, "expected a JSON/YAML task list"),
        ({}, "expected a JSON/YAML task list"),
        ([None], "each task must be an object"),
        ([{"id": "x"}], "nonempty status"),
        ([task("x"), task("x")], "duplicate task ID"),
    ],
)
def test_bad_input_is_not_reported_as_an_empty_run(payload: Any, message: str) -> None:
    with pytest.raises(ValueError, match=message):
        cp.task_rows(payload)


@pytest.mark.parametrize("extra", [{"depends_on": "abc"}, {"waiting_on": []}, {"waiting_on": {"unmet": [None]}}])
def test_malformed_dependency_fields_fail(extra: dict[str, Any]) -> None:
    with pytest.raises(ValueError, match="must be a list|requires an unmet list|invalid unmet dependency"):
        cp.render_tree([task("a", **extra)])


def test_cli_reads_saved_envelope_and_stdin(tmp_path: Path) -> None:
    payload = json.dumps({"ok": True, "result": [task("root")], "warnings": ["snapshot warning"]})
    snapshot = tmp_path / "tasks.json"
    snapshot.write_text(payload, encoding="utf-8")
    for source in (str(snapshot), "-"):
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--tasks-file", source], input=payload, capture_output=True, text=True
        )
        assert result.returncode == 0, result.stderr
        assert "* root [DRAFT_CODE]" in result.stdout
        assert "WARNING: snapshot warning" in result.stderr


def test_live_query_uses_all_tasks_and_computed_waits(monkeypatch: pytest.MonkeyPatch, capsys: Any) -> None:
    def run(command: list[str], **kwargs: Any) -> subprocess.CompletedProcess[str]:
        assert command == ["custom-cli", "-C", "project", "get", "tasks", "--all", "--summary", "--json"]
        return subprocess.CompletedProcess(command, 0, json.dumps({"ok": True, "result": [task("live")]}), "")

    monkeypatch.setattr(cp.subprocess, "run", run)
    assert cp.main(["--project-root", "project", "--binary", "custom-cli"]) == 0
    assert "* live [DRAFT_CODE]" in capsys.readouterr().out


@pytest.mark.parametrize("failure", ["exit", "timeout", "missing"])
def test_query_failures_do_not_print_a_success_report(
    failure: str, monkeypatch: pytest.MonkeyPatch, capsys: Any
) -> None:
    def run(command: list[str], **kwargs: Any) -> subprocess.CompletedProcess[str]:
        if failure == "timeout":
            raise subprocess.TimeoutExpired(command, 60)
        if failure == "missing":
            raise FileNotFoundError("binary unavailable")
        return subprocess.CompletedProcess(command, 2, "", "query refused")

    monkeypatch.setattr(cp.subprocess, "run", run)
    assert cp.main([]) == 1
    output = capsys.readouterr()
    assert output.out == ""
    assert "ERROR:" in output.err


def test_invalid_json_fails_without_a_traceback(tmp_path: Path, capsys: Any) -> None:
    snapshot = tmp_path / "bad.json"
    snapshot.write_text("{", encoding="utf-8")
    assert cp.main(["--tasks-file", str(snapshot)]) == 1
    output = capsys.readouterr()
    assert output.out == ""
    assert "ERROR:" in output.err


@pytest.mark.parametrize("state_mapping", [True, False])
def test_cli_reads_yaml_state_or_task_list_from_file_and_stdin(tmp_path: Path, state_mapping: bool) -> None:
    payload = """- id: done
  status: MERGED
- id: schema
  status: IMPLEMENTING_CODE
  depends_on: [done]
  created: 2026-10-16T08:00:00Z
- id: api
  status: DRAFT_CODE
  depends_on: [schema]
"""
    if state_mapping:
        payload = "version: 1\nconfig: {mode: RUNNING}\ntasks:\n" + payload
    snapshot = tmp_path / "state.yaml"
    snapshot.write_text(payload, encoding="utf-8")
    for source in (str(snapshot), "-"):
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--tasks-file", source], input=payload, capture_output=True, text=True
        )
        assert result.returncode == 0, result.stderr
        assert "Nonterminal tasks: 2 | Longest chain: 2 task(s)" in result.stdout
        assert "* schema [IMPLEMENTING_CODE]\n`-- * api [DRAFT_CODE]" in result.stdout
        assert "done [MERGED]" not in result.stdout


def test_json_state_mapping_is_accepted() -> None:
    rows = cp.task_rows(cp.parse_snapshot(json.dumps({"tasks": [task("a")]})))
    assert "* a [DRAFT_CODE]" in cp.render_tree(rows)


def test_raw_provider_declarations_are_not_silently_ignored() -> None:
    rows = [
        task("plan", "MERGED"),
        task("consumer", provider_dependencies=[{"provider_task": "plan", "transition": "code", "outputs": [0]}]),
        task("reserved", provider_reservations=[{"provider_task": "plan"}]),
    ]
    report = cp.render_tree(rows)
    assert "consumer: provider prerequisites not resolved; use CLI JSON with computed waiting_on" in report
    assert "reserved: provider prerequisites not resolved; use CLI JSON with computed waiting_on" in report
    assert "unresolved holds are not counted" in report


def test_yaml_input_does_not_deserialize_python_objects() -> None:
    with pytest.raises(ValueError, match="invalid JSON/YAML snapshot"):
        cp.parse_snapshot("!!python/object:builtins.object {}")


def test_missing_pyyaml_has_actionable_error_and_json_still_works(monkeypatch: pytest.MonkeyPatch) -> None:
    original_import = builtins.__import__

    def without_yaml(name: str, *args: Any, **kwargs: Any) -> Any:
        if name == "yaml":
            raise ImportError("not installed")
        return original_import(name, *args, **kwargs)

    monkeypatch.setattr(builtins, "__import__", without_yaml)
    assert cp.parse_snapshot("[]") == []
    with pytest.raises(ValueError, match="YAML input requires PyYAML"):
        cp.parse_snapshot("tasks: []")
