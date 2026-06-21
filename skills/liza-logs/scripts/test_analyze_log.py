from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path
from typing import Any


def load_analyzer() -> Any:
    path = Path(__file__).with_name("analyze-log.py")
    spec = importlib.util.spec_from_file_location("analyze_log", path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def as_lines(*events: dict[str, Any]) -> list[str]:
    return [json.dumps(event) for event in events]


def command_completed_event(item_id: str, command: str, output: str = "", exit_code: int = 0) -> dict[str, Any]:
    return {
        "type": "item.completed",
        "item": {
            "id": item_id,
            "type": "command_execution",
            "status": "completed",
            "command": command,
            "aggregated_output": output,
            "exit_code": exit_code,
        },
    }


def sparse_command_lines() -> list[str]:
    return as_lines(
        {"type": "thread.started", "thread_id": "t"},
        {"type": "turn.started"},
        command_completed_event("item_1", "echo ok", "ok\n"),
        {"type": "turn.completed", "usage": {"input_tokens": 1, "cached_input_tokens": 0, "output_tokens": 1}},
    )


def test_sparse_message_and_command_in_one_turn_counts_as_tool_turn() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            {
                "type": "item.completed",
                "item": {"id": "item_1", "type": "agent_message", "status": "completed", "text": "thinking aloud"},
            },
            command_completed_event("item_2", "echo ok", "ok\n"),
            {"type": "turn.completed", "usage": {"input_tokens": 1, "cached_input_tokens": 0, "output_tokens": 1}},
        )
    )

    assert report.meta.num_turns == 1
    assert report.turn_units == 1
    assert report.tool_turn_units == 1
    assert report.empty_turns == []


def test_sparse_failed_turn_after_tool_item_does_not_add_empty_turn() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            command_completed_event("item_1", "false", exit_code=1),
            {"type": "turn.failed", "error": {"message": "command failed"}},
        )
    )

    assert report.meta.num_turns == 1
    assert report.turn_units == 1
    assert report.tool_turn_units == 1
    assert report.empty_turns == []


def test_sparse_failed_turn_without_completed_items_counts_as_empty_turn() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            {"type": "turn.failed", "error": {"message": "usage limit"}},
        )
    )

    assert report.meta.num_turns == 1
    assert report.turn_units == 1
    assert report.tool_turn_units == 0
    assert len(report.empty_turns) == 1
    assert report.empty_turns[0].item_type == "turn.failed"
    assert report.empty_turns[0].preview == "usage limit"


def test_sparse_tool_actions_keep_codex_turn_numbers() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            command_completed_event("item_1", "echo one", "one\n"),
            command_completed_event("item_2", "echo two", "two\n"),
            {"type": "turn.completed", "usage": {"input_tokens": 2, "cached_input_tokens": 0, "output_tokens": 2}},
        )
    )

    assert report.meta.num_turns == 1
    assert report.turn_units == 2
    assert report.tool_turn_units == 2
    assert [action.turn_num for action in report.actions] == [1, 2]


def test_sparse_single_outer_turn_counts_each_action_item_as_turn() -> None:
    analyzer = load_analyzer()
    events: list[dict[str, Any]] = [
        {"type": "thread.started", "thread_id": "t"},
        {"type": "turn.started"},
        {
            "type": "item.completed",
            "item": {"id": "item_0", "type": "agent_message", "status": "completed", "text": "starting"},
        },
    ]
    events.extend(command_completed_event(f"item_{i}", f"echo {i}", f"{i}\n") for i in range(1, 43))
    events.append(
        {"type": "turn.completed", "usage": {"input_tokens": 1, "cached_input_tokens": 0, "output_tokens": 1}}
    )

    report = analyzer.parse_sparse(as_lines(*events))

    assert report.meta.num_turns == 1
    assert report.turn_units == 42
    assert report.tool_turn_units == 42
    assert report.empty_turns == []
    assert [action.turn_num for action in report.actions[-3:]] == [40, 41, 42]


def test_sparse_report_omits_longest_turns_section() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(sparse_command_lines())

    def sentinel_longest_turns(_: Any) -> str:
        return "SENTINEL LONGEST TURNS"

    analyzer.render_longest_turns = sentinel_longest_turns
    rendered = analyzer.render_report(report)

    assert "TOP 10 LONGEST TURNS" not in rendered
    assert "SENTINEL LONGEST TURNS" not in rendered
    assert "Note: Per-turn growth unavailable in sparse format" in rendered


def test_sparse_turn_timeline_omits_unavailable_duration_column() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(sparse_command_lines())

    rendered = analyzer.render_turn_timeline(report)

    assert "TURN TIMELINE" in rendered
    assert "Duration" not in rendered
    assert "0.0s" not in rendered


def test_sparse_rtk_command_shows_wrapped_command_in_tool_name() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            command_completed_event("item_1", "rtk git status --short"),
            command_completed_event("item_2", "/usr/bin/zsh -lc 'rtk pytest -q'"),
            {"type": "turn.completed", "usage": {"input_tokens": 1, "cached_input_tokens": 0, "output_tokens": 1}},
        )
    )

    assert report.tool_calls == {"rtk git": 1, "rtk pytest": 1}
    assert report.actions[0].tool_name == "rtk git"
    assert report.actions[1].tool_name == "rtk pytest"

    rendered = analyzer.render_turn_timeline(report)

    assert "rtk git" in rendered
    assert "rtk pytest" in rendered
    assert "rtk                  " not in rendered


def test_sparse_rtk_rg_exit_one_is_not_error() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            command_completed_event("item_1", 'rtk rg -n "missing" internal', exit_code=1),
            command_completed_event("item_2", "rtk go test ./internal/ops", "boom\n", exit_code=1),
            {"type": "turn.completed", "usage": {"input_tokens": 1, "cached_input_tokens": 0, "output_tokens": 1}},
        )
    )

    assert report.actions[0].tool_name == "rtk rg"
    assert report.actions[0].is_error is False
    assert report.actions[1].is_error is True


def test_rich_bash_rtk_rg_exit_one_empty_result_is_not_error() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude"},
            {
                "type": "assistant",
                "message": {
                    "id": "m1",
                    "usage": {},
                    "content": [
                        {
                            "type": "tool_use",
                            "id": "toolu_1",
                            "name": "Bash",
                            "input": {"command": 'rtk rg -n "missing" internal'},
                        }
                    ],
                },
            },
            {
                "type": "user",
                "message": {
                    "content": [
                        {
                            "type": "tool_result",
                            "tool_use_id": "toolu_1",
                            "is_error": True,
                            "content": "",
                        }
                    ]
                },
            },
        )
    )

    assert report.actions[0].is_error is False


def test_rich_report_highlights_permission_friction_near_top() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude"},
            {
                "type": "assistant",
                "message": {
                    "id": "m1",
                    "usage": {},
                    "content": [
                        {
                            "type": "tool_use",
                            "id": "toolu_1",
                            "name": "Bash",
                            "input": {"command": "mdtoc specs/story.md"},
                        }
                    ],
                },
            },
            {
                "type": "user",
                "message": {
                    "content": [
                        {
                            "type": "tool_result",
                            "tool_use_id": "toolu_1",
                            "is_error": True,
                            "content": "This command requires approval",
                        }
                    ]
                },
            },
        )
    )
    report.meta.file = "coder-1-20260523-140607.txt"

    rendered = analyzer.render_report(report)

    assert "PERMISSION & POLICY FRICTION" in rendered
    assert "generic approval-required command" in rendered
    assert rendered.index("PERMISSION & POLICY FRICTION") < rendered.index("TOKEN SUMMARY")


def test_rich_model_usage_does_not_set_session_model() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s"},
            {
                "type": "result",
                "modelUsage": {
                    "claude-haiku-4-5-20251001": {
                        "contextWindow": 200000,
                        "maxOutputTokens": 32000,
                    }
                },
            },
        )
    )

    assert report.meta.model == ""
    assert report.meta.context_window == 200000
    assert report.meta.max_output_tokens == 32000


def test_rich_system_event_sets_session_model() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude-opus-4-5-20251101"},
            {
                "type": "result",
                "modelUsage": {
                    "claude-haiku-4-5-20251001": {
                        "contextWindow": 200000,
                        "maxOutputTokens": 32000,
                    }
                },
            },
        )
    )

    assert report.meta.model == "claude-opus-4-5-20251101"
    assert report.meta.context_window == 200000
    assert report.meta.max_output_tokens == 32000


def test_rich_later_system_events_do_not_clear_session_model() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_rich(
        as_lines(
            {
                "type": "system",
                "subtype": "init",
                "session_id": "s",
                "model": "claude-opus-4-8[1m]",
                "mcp_servers": [{"name": "playwright", "status": "pending"}],
            },
            {"type": "system", "subtype": "thinking_tokens", "session_id": "s", "estimated_tokens": 50},
            {
                "type": "result",
                "modelUsage": {
                    "claude-haiku-4-5-20251001": {
                        "contextWindow": 200000,
                        "maxOutputTokens": 32000,
                    },
                    "claude-opus-4-8[1m]": {
                        "contextWindow": 1000000,
                        "maxOutputTokens": 64000,
                    },
                },
            },
        )
    )

    assert report.meta.model == "claude-opus-4-8[1m]"
    assert report.meta.session_id == "s"
    assert report.mcp_servers == [{"name": "playwright", "status": "pending"}]
    assert report.meta.context_window == 1000000
    assert report.meta.max_output_tokens == 64000


def test_efficiency_insights_ignore_shared_prefix_non_duplicates() -> None:
    analyzer = load_analyzer()
    report = analyzer.SessionReport()
    shared_prefix = '--- §BRAND_NAME_LOWER§_version: "0.2.0" §BRAND_NAME_LOWER§_git_commit: "abc" ---\n'
    first = shared_prefix + "first document\n" + ("a" * 1200)
    second = shared_prefix + "second document\n" + ("b" * 1200)
    report.actions = [
        analyzer.TurnAction(
            turn_num=1,
            tool_name="cat",
            result_chars=len(first),
            result_preview=first[:120].replace("\n", " "),
            result_hash=analyzer._hash_result(first),
        ),
        analyzer.TurnAction(
            turn_num=2,
            tool_name="cat",
            result_chars=len(second),
            result_preview=second[:120].replace("\n", " "),
            result_hash=analyzer._hash_result(second),
        ),
    ]

    rendered = analyzer.render_efficiency_insights(report)

    assert "duplicate result" not in rendered


def test_efficiency_insights_report_exact_duplicate_events() -> None:
    analyzer = load_analyzer()
    report = analyzer.SessionReport()
    output = "README.md | 28 +++++---\n" + ("diff body\n" * 200)
    report.actions = [
        analyzer.TurnAction(
            turn_num=17,
            tool_name="rtk git",
            result_chars=len(output),
            result_preview=output[:120].replace("\n", " "),
            result_hash=analyzer._hash_result(output),
        ),
        analyzer.TurnAction(
            turn_num=25,
            tool_name="rtk git",
            result_chars=len(output),
            result_preview=output[:120].replace("\n", " "),
            result_hash=analyzer._hash_result(output),
        ),
    ]

    rendered = analyzer.render_efficiency_insights(report)

    assert "1 duplicate result(s)" in rendered
    assert "#17, #25" in rendered
    assert "rtk git" in rendered


def test_role_summary_groups_logs_by_agent_role(tmp_path: Path) -> None:
    analyzer = load_analyzer()
    coder_log = tmp_path / "coder-1-20260523-140607.txt"
    reviewer_log = tmp_path / "code-reviewer-2-20260523-154625.txt"
    coder_log.write_text(
        "\n".join(
            sparse_command_lines()
            + [
                json.dumps(
                    command_completed_event(
                        "item_2",
                        "rtk go test ./internal/ops",
                        "boom\n",
                        exit_code=1,
                    )
                )
            ]
        ),
        encoding="utf-8",
    )
    reviewer_log.write_text("\n".join(sparse_command_lines()), encoding="utf-8")

    reports = [analyzer.analyze_file(str(coder_log)), analyzer.analyze_file(str(reviewer_log))]
    rendered = analyzer.render_role_summary([report for report in reports if report])

    assert "ROLE SUMMARY" in rendered
    assert "coder-1" in rendered
    assert "code-reviewer-2" in rendered
    assert "Errors:        1" in rendered
    assert "TOP TOOL RESULT VOLUME" in rendered


def test_role_summary_highlights_permission_friction() -> None:
    analyzer = load_analyzer()
    report = analyzer.SessionReport()
    report.meta.file = "coder-1-20260523-140607.txt"
    report.meta.format = "rich"
    report.actions = [
        analyzer.TurnAction(
            tool_name="Bash",
            detail="cd worktree && git status",
            is_error=True,
            result_preview="This command changes directory before running git",
        ),
        analyzer.TurnAction(
            tool_name="Bash",
            detail="uvx ruff@0.14.7 check file.py",
            is_error=True,
            result_preview="This command requires approval",
        ),
    ]

    rendered = analyzer.render_role_summary([report])

    assert "PERMISSION & POLICY FRICTION" in rendered
    assert "Blocks: 2" in rendered
    assert "cd before git" in rendered
    assert "generic approval-required command" in rendered
    assert rendered.index("PERMISSION & POLICY FRICTION") < rendered.index("TOP TOOL RESULT VOLUME")


def test_role_summary_includes_mcp_usage() -> None:
    analyzer = load_analyzer()
    report = analyzer.SessionReport()
    report.meta.file = "coder-1-20260523-140607.txt"
    report.meta.format = "sparse"
    report.actions = [
        analyzer.TurnAction(tool_name="github/list_issues", result_chars=1200),
        analyzer.TurnAction(tool_name="github/get_issue", result_chars=300, is_error=True),
        analyzer.TurnAction(tool_name=f"{analyzer.BRAND_MCP_SERVER}/get_state", result_chars=999),
        analyzer.TurnAction(tool_name="rtk git", result_chars=50),
    ]

    rendered = analyzer.render_role_summary([report])

    assert "MCP USAGE" in rendered
    assert "MCP calls: 2/4 (50% of all tool calls)" in rendered
    assert "github" in rendered
    assert "github/list_issues" in rendered
    assert "github/get_issue" in rendered
    mcp_section = rendered.split("MCP USAGE", 1)[1]
    assert f"{analyzer.BRAND_MCP_SERVER}/get_state" not in mcp_section


def test_mcp_parser_ignores_slash_in_command_display_name() -> None:
    analyzer = load_analyzer()

    assert analyzer._parse_mcp_tool_name("rtk /usr/bin/test") is None
    assert analyzer._parse_mcp_tool_name("github/list_issues") == ("github", "list_issues")
