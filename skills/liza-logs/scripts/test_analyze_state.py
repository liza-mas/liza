from __future__ import annotations

import hashlib
import importlib.util
import json
import sys
from pathlib import Path
from typing import Any

import pytest
import yaml


def load_state_analyzer() -> Any:
    path = Path(__file__).with_name("analyze-state.py")
    spec = importlib.util.spec_from_file_location("analyze_state", path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def test_analyze_state_counts_high_rejection_and_terminal_tasks() -> None:
    analyzer = load_state_analyzer()
    state = {
        "tasks": [
            {
                "id": "task-a",
                "status": "MERGED",
                "type": "coding",
                "history": [{"event": "rejected", "agent": "reviewer-1", "reason": "first"}] * 4,
            },
            {
                "id": "task-b",
                "status": "SUPERSEDED",
                "type": "planning",
                "blocked_reason": "hypothesis_exhaustion: artifact refs stale",
                "history": [
                    {
                        "event": "superseded",
                        "agent": "orchestrator-1",
                        "reason": "Superseded after artifact-ref preservation race",
                    }
                ],
                "superseded_by": ["task-b-repair-0"],
            },
            {
                "id": "task-c",
                "status": "BLOCKED",
                "type": "coding",
                "blocked_reason": "Needs human decision",
            },
        ]
    }

    analysis = analyzer.analyze_state(state)

    assert analysis["task_count"] == 3
    assert analysis["status_counts"] == {"BLOCKED": 1, "MERGED": 1, "SUPERSEDED": 1}
    assert analysis["friction_counts"]["high_rejection_tasks"] == 1
    assert analysis["friction_counts"]["SUPERSEDED"] == 1
    assert analysis["friction_counts"]["BLOCKED"] == 1
    assert analysis["high_rejection_tasks"][0]["id"] == "task-a"
    assert analysis["high_rejection_tasks"][0]["rejections"] == 4
    assert analysis["superseded_reason_buckets"]["artifact/ref drift"] == 1


def test_render_report_includes_state_sections() -> None:
    analyzer = load_state_analyzer()
    analysis = analyzer.analyze_state({"tasks": []})
    rendered = analyzer.render_report(analysis, "§BRAND_PROJECT_DIRNAME§/state.yaml")

    assert "STATE FRICTION INVENTORY" in rendered
    assert "STATUS COUNTS" in rendered
    assert "HIGH-REJECTION TASKS" in rendered
    assert "SUPERSEDED REASON BUCKETS" in rendered


def test_load_state_accepts_yaml_task_mapping(tmp_path: Path) -> None:
    analyzer = load_state_analyzer()
    path = tmp_path / "state.yaml"
    path.write_text(
        yaml.safe_dump({"tasks": {"task-a": {"id": "task-a", "status": "ABANDONED"}}}),
        encoding="utf-8",
    )

    analysis = analyzer.analyze_state(analyzer.load_state(str(path)))

    assert analysis["task_count"] == 1
    assert analysis["friction_counts"]["ABANDONED"] == 1


def test_json_payload_is_serializable() -> None:
    analyzer = load_state_analyzer()
    analysis = analyzer.analyze_state({"tasks": [{"id": "task-a", "status": "MERGED"}]})

    payload = json.loads(json.dumps(analysis))

    assert payload["task_count"] == 1


def terminal_snapshot(tmp_path: Path) -> tuple[Path, Path, dict[str, Any]]:
    task = {
        "id": "retained-task",
        "status": "SUPERSEDED",
        "created": "2026-10-06T10:00:00Z",
        "history": [{"event": "superseded", "reason": "artifact refs stale"}],
        "output": [{"opaque": 17}],
        "unknown_extension": {"counter": 12},
    }
    raw = json.dumps(
        {
            "format_version": 2,
            "task_id": task["id"],
            "field": "terminal_task",
            "value_yaml": yaml.safe_dump(task),
        }
    ).encode()
    digest = hashlib.sha256(raw).hexdigest()
    object_path = tmp_path / "archive" / "objects" / digest[:2] / f"{digest}.json"
    object_path.parent.mkdir(parents=True)
    object_path.write_bytes(raw)
    stub = {key: task[key] for key in ("id", "status", "created")}
    stub["terminal_archive"] = {"sha256": digest, "archived_at": "2026-10-06T10:01:00Z"}
    state_path = tmp_path / "state-snapshot.yaml"
    state_path.write_text(yaml.safe_dump({"tasks": [stub]}), encoding="utf-8")
    return state_path, object_path, task


def test_terminal_archive_snapshot_preserves_report_and_scalar_types(tmp_path: Path) -> None:
    analyzer = load_state_analyzer()
    state_path, _, task = terminal_snapshot(tmp_path)
    loaded = analyzer.load_state(str(state_path))
    restored = dict(loaded["tasks"][0])
    restored.pop("terminal_archive")
    assert restored == task
    report = analyzer.analyze_state(loaded)
    assert report["superseded_reason_buckets"] == {"artifact/ref drift": 1}


@pytest.mark.parametrize("corrupt", [False, True])
def test_terminal_archive_missing_or_corrupt_evidence_fails(tmp_path: Path, corrupt: bool) -> None:
    analyzer = load_state_analyzer()
    state_path, object_path, _ = terminal_snapshot(tmp_path)
    if corrupt:
        object_path.write_bytes(b"tampered evidence")
    else:
        object_path.unlink()
    with pytest.raises((ValueError, OSError), match=object_path.name):
        analyzer.load_state(str(state_path))


def test_terminal_archive_detached_snapshot_requires_explicit_objects(tmp_path: Path) -> None:
    analyzer = load_state_analyzer()
    state_path, object_path, _ = terminal_snapshot(tmp_path)
    detached = tmp_path / "detached" / "copy.yaml"
    detached.parent.mkdir()
    detached.write_bytes(state_path.read_bytes())
    with pytest.raises(OSError, match="archive"):
        analyzer.load_state(str(detached))
    loaded = analyzer.load_state(str(detached), str(object_path.parents[2]))
    assert len(loaded["tasks"][0]["history"]) == 1


@pytest.mark.parametrize("defect", ["version", "field", "envelope_id", "payload_id", "status", "created", "extra"])
def test_terminal_archive_valid_digest_does_not_authorize_invalid_payload(tmp_path: Path, defect: str) -> None:
    analyzer = load_state_analyzer()
    state_path, object_path, task = terminal_snapshot(tmp_path)
    envelope = json.loads(object_path.read_bytes())
    if defect in {"payload_id", "status", "created"}:
        key, value = {
            "payload_id": ("id", "other-task"),
            "status": ("status", "READY"),
            "created": ("created", "2026-10-07T10:00:00Z"),
        }[defect]
        task[key] = value
        envelope["value_yaml"] = yaml.safe_dump(task)
    else:
        envelope_key, envelope_value = {
            "version": ("format_version", 1),
            "field": ("field", "acceptance_receipt"),
            "envelope_id": ("task_id", "other-task"),
            "extra": ("unknown", True),
        }[defect]
        envelope[envelope_key] = envelope_value
    raw = json.dumps(envelope).encode()
    digest = hashlib.sha256(raw).hexdigest()
    changed_path = object_path.parents[1] / digest[:2] / f"{digest}.json"
    changed_path.parent.mkdir(exist_ok=True)
    changed_path.write_bytes(raw)
    physical = yaml.safe_load(state_path.read_text())
    physical["tasks"][0]["terminal_archive"]["sha256"] = digest
    state_path.write_text(yaml.safe_dump(physical), encoding="utf-8")
    with pytest.raises(ValueError, match="invalid terminal archive|identity/status/created mismatch"):
        analyzer.load_state(str(state_path))
