from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path
from typing import Any

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
