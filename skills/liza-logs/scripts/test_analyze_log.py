from __future__ import annotations

import importlib.util
import json
import subprocess
import sys
from pathlib import Path
from typing import Any

# The output path is representative; the fixed text and suffix structure come
# from the reported provider result whose raw log is not retained in the repo.
PROVIDER_BACKGROUND_RESULT_WITH_SUFFIX = (
    "Command did not complete within its 180s timeout and was moved to the background "
    "(ID: bdcgot1os). Output is being written to: /tmp/provider-output/bdcgot1os.log"
)
NATIVE_CLAUDE_RICH_INIT_FIXTURE = Path(__file__).parent / "testdata" / "claude-rich-init-redacted.ndjson"
BROWSER_ANALYZER = next((Path(__file__).parents[1] / "tools").glob("*-session-analyzer.html"))


def load_analyzer() -> Any:
    path = Path(__file__).with_name("analyze-log.py")
    spec = importlib.util.spec_from_file_location("analyze_log", path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def probe_browser_command_normalization(commands: list[str]) -> list[dict[str, Any]]:
    source = BROWSER_ANALYZER.read_text()
    start = source.index("// Commands where exit code 1")
    end = source.index("function extractCommandDetail")
    script = (
        source[start:end]
        + r"""
const commands = JSON.parse(require("fs").readFileSync(0, "utf8"));
const results = commands.map(command => ({
  name: extractCommandName(command),
  usageName: toolUsageName("Bash", { command }),
  benignExitOne: isBenignExit(command, 1),
}));
console.log(JSON.stringify(results));
"""
    )
    completed = subprocess.run(
        ["node", "-e", script],
        input=json.dumps(commands),
        check=True,
        capture_output=True,
        text=True,
    )
    return json.loads(completed.stdout)


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


def test_sparse_tool_actions_share_outer_turn_number() -> None:
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
    assert report.turn_units == 1
    assert report.tool_turn_units == 1
    assert [action.turn_num for action in report.actions] == [1, 1]


def test_sparse_single_outer_turn_counts_once_for_empty_turn_accounting() -> None:
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
    assert report.turn_units == 1
    assert report.tool_turn_units == 1
    assert report.empty_turns == []
    assert [action.turn_num for action in report.actions[-3:]] == [1, 1, 1]


def test_sparse_empty_turn_accounting_uses_completed_outer_turns() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            {
                "type": "item.completed",
                "item": {"id": "text", "type": "agent_message", "status": "completed", "text": "summary"},
            },
            {"type": "turn.completed", "usage": {}},
            {"type": "turn.started"},
            {"type": "turn.completed", "usage": {}},
            {"type": "turn.started"},
            command_completed_event("tool-1", "echo one", "one\n"),
            command_completed_event("tool-2", "echo two", "two\n"),
            {"type": "turn.completed", "usage": {}},
        )
    )

    assert report.meta.num_turns == 3
    assert report.turn_units == 3
    assert report.tool_turn_units == 1
    assert [(empty.turn_num, empty.item_type) for empty in report.empty_turns] == [(2, "turn")]
    rendered = analyzer.render_empty_turns(report)
    assert "Text-only" in rendered
    assert "         3         1         1         1    33.33%" in rendered


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


def test_sparse_commands_aggregate_by_underlying_executable() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            command_completed_event("item_1", "rtk git status --short"),
            command_completed_event("item_2", "/usr/bin/zsh -lc 'rtk pytest -q'"),
            command_completed_event("item_3", "git diff --stat"),
            command_completed_event("item_4", "/bin/bash -lc 'rtk git log --oneline'"),
            command_completed_event("item_5", "/usr/bin/git branch --show-current"),
            {"type": "turn.completed", "usage": {"input_tokens": 1, "cached_input_tokens": 0, "output_tokens": 1}},
        )
    )

    assert report.tool_calls == {"git": 4, "pytest": 1}
    assert [action.tool_name for action in report.actions] == ["git", "pytest", "git", "git", "git"]

    rendered = analyzer.render_tool_calls(report)

    assert "rtk git" not in rendered
    assert "rtk pytest" not in rendered


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

    assert report.actions[0].tool_name == "rg"
    assert report.actions[0].is_error is False
    assert report.actions[1].is_error is True


def test_sparse_wrapped_setup_preamble_rtk_rg_exit_one_is_not_error() -> None:
    analyzer = load_analyzer()

    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t"},
            {"type": "turn.started"},
            command_completed_event(
                "item_1",
                "/bin/bash -lc 'set +e; rtk rg -n missing internal'",
                exit_code=1,
            ),
            {"type": "turn.completed", "usage": {"input_tokens": 1, "cached_input_tokens": 0, "output_tokens": 1}},
        )
    )

    assert report.tool_calls == {"rg": 1}
    assert report.actions[0].tool_name == "rg"
    assert report.actions[0].is_error is False


def test_browser_command_normalization_matches_python() -> None:
    analyzer = load_analyzer()
    commands = [
        "rtk git status --short",
        "/usr/bin/zsh -lc 'rtk pytest -q'",
        "/bin/bash -lc 'set +e; rtk rg -n missing internal'",
        "/bin/sh -c 'git diff --stat'",
    ]

    browser_results = probe_browser_command_normalization(commands)
    expected_names = [analyzer._extract_command_name(command) for command in commands]

    assert expected_names == ["git", "pytest", "rg", "git"]
    assert [result["name"] for result in browser_results] == expected_names
    assert [result["usageName"] for result in browser_results] == expected_names
    assert browser_results[2]["benignExitOne"] is True


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

    assert report.tool_calls == {"rg": 1}
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


def test_per_turn_growth_uses_reported_200k_context_window() -> None:
    analyzer = load_analyzer()
    report = analyzer.SessionReport()
    report.meta.context_window = 200000
    report.turns = [analyzer.TurnUsage(input_tokens=100000)]

    rendered = analyzer.render_per_turn_growth(report)

    assert "50.0%" in rendered
    assert "n/a" not in rendered


def test_per_turn_growth_does_not_assume_unknown_context_window() -> None:
    analyzer = load_analyzer()
    report = analyzer.SessionReport()
    report.turns = [analyzer.TurnUsage(input_tokens=100000)]

    rendered = analyzer.render_per_turn_growth(report)

    assert "Context window unavailable; Fill% is n/a." in rendered
    assert "50.0%" not in rendered


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


def test_rich_terminal_usage_owns_aggregates_without_changing_envelope_turns() -> None:
    analyzer = load_analyzer()
    events: list[dict[str, Any]] = [{"type": "system", "session_id": "s", "model": "claude-opus"}]
    for index in range(45):
        tool_id = f"toolu_{index}"
        events.extend(
            [
                {
                    "type": "assistant",
                    "message": {
                        "id": f"m{index}",
                        "usage": {
                            "input_tokens": index + 1,
                            "cache_creation_input_tokens": index + 2,
                            "cache_read_input_tokens": index + 3,
                            "output_tokens": index + 4,
                        },
                        "content": [
                            {
                                "type": "tool_use",
                                "id": tool_id,
                                "name": "Read",
                                "input": {"file_path": f"specs/{index}.md"},
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
                                "tool_use_id": tool_id,
                                "content": (
                                    f"result-{index}"
                                    if index % 2 == 0
                                    else [{"type": "text", "text": f"result-{index}"}]
                                ),
                            }
                        ]
                    },
                },
            ]
        )
    events.extend(
        [
            {
                "type": "user",
                "message": {"content": [{"type": "text", "text": "operator follow-up"}]},
            },
            {
                "type": "result",
                "is_error": True,
                "usage": {
                    "input_tokens": 12_345,
                    "cache_creation_input_tokens": 23_456,
                    "cache_read_input_tokens": 34_567,
                    "output_tokens": 107_599,
                },
                "total_cost_usd": 8.0589,
            },
        ]
    )

    report = analyzer.parse_rich(as_lines(*events))

    assert (
        report.total_input_tokens,
        report.total_cache_creation,
        report.total_cache_read,
        report.total_output_tokens,
    ) == (12_345, 23_456, 34_567, 107_599)
    assert report.total_cost_usd == 8.0589
    assert report.aggregate_usage_source == "terminal"
    assert len(report.turns) == 45
    assert report.turns[0].output_tokens == 4
    assert len([item for item in report.items if item.item_type == "tool_result"]) == 45
    assert len([item for item in report.items if item.item_type == "text"]) == 1
    assert len(report.actions) == 45
    assert "107.6K" not in analyzer.render_per_turn_growth(report)


def test_rich_per_turn_usage_reports_when_envelopes_match_terminal_aggregate() -> None:
    analyzer = load_analyzer()
    report = analyzer.SessionReport(
        total_input_tokens=10,
        total_cache_creation=20,
        total_cache_read=30,
        total_output_tokens=40,
        aggregate_usage_source="terminal",
        turns=[
            analyzer.TurnUsage(
                message_id="m1",
                input_tokens=10,
                cache_creation_input_tokens=20,
                cache_read_input_tokens=30,
                output_tokens=40,
            )
        ],
    )

    for rendered in (analyzer.render_per_turn_growth(report), analyzer.render_longest_turns(report)):
        assert "Turn Usage Source: assistant message envelopes" in rendered
        assert "Coverage: rows reconcile with terminal aggregate usage" in rendered


def test_rich_per_turn_usage_warns_when_envelopes_diverge_from_terminal_aggregate() -> None:
    analyzer = load_analyzer()
    report = analyzer.SessionReport(
        total_input_tokens=12_345,
        total_cache_creation=23_456,
        total_cache_read=34_567,
        total_output_tokens=107_599,
        aggregate_usage_source="terminal",
        turns=[
            analyzer.TurnUsage(
                message_id="m1",
                input_tokens=1,
                cache_creation_input_tokens=2,
                cache_read_input_tokens=3,
                output_tokens=428,
            )
        ],
    )

    for rendered in (analyzer.render_per_turn_growth(report), analyzer.render_longest_turns(report)):
        assert "Turn Usage Source: assistant message envelopes" in rendered
        assert "Coverage: rows do not reconcile with terminal aggregate usage" in rendered
        assert "use TOKEN SUMMARY for authoritative totals" in rendered


def test_rich_incomplete_usage_is_envelope_derived_and_explicitly_partial() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude-opus"},
            {
                "type": "assistant",
                "message": {
                    "id": "m1",
                    "usage": {
                        "input_tokens": 10,
                        "cache_creation_input_tokens": 20,
                        "cache_read_input_tokens": 30,
                        "output_tokens": 40,
                    },
                    "content": [{"type": "text", "text": "still working"}],
                },
            },
        )
    )

    assert (
        report.total_input_tokens,
        report.total_cache_creation,
        report.total_cache_read,
        report.total_output_tokens,
    ) == (10, 20, 30, 40)
    assert report.aggregate_usage_source == "envelope-partial"
    assert "partial" in analyzer.render_token_summary(report).lower()


def test_rich_unusable_terminal_usage_preserves_envelope_partial_totals() -> None:
    analyzer = load_analyzer()
    assistant = {
        "type": "assistant",
        "message": {
            "id": "m1",
            "usage": {
                "input_tokens": 10,
                "cache_creation_input_tokens": 20,
                "cache_read_input_tokens": 30,
                "output_tokens": 40,
            },
            "content": [],
        },
    }

    for result in (
        {"type": "result"},
        {"type": "result", "usage": "malformed"},
        {"type": "result", "usage": {}},
        {"type": "result", "usage": {"unrecognized_tokens": 999}},
        {"type": "result", "usage": {"output_tokens": 999}},
        {"type": "result", "usage": {"output_tokens": "malformed"}},
        {"type": "result", "usage": {"output_tokens": -1}},
    ):
        report = analyzer.parse_rich(as_lines(assistant, result))

        assert (
            report.total_input_tokens,
            report.total_cache_creation,
            report.total_cache_read,
            report.total_output_tokens,
        ) == (10, 20, 30, 40)
        assert report.aggregate_usage_source == "envelope-partial"


def test_rich_large_tool_result_is_measured_once_everywhere() -> None:
    analyzer = load_analyzer()
    payload = "unique-large-prefix " + ("x" * 5_000)
    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude-opus"},
            {
                "type": "assistant",
                "message": {
                    "id": "m1",
                    "usage": {},
                    "content": [{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": {}}],
                },
            },
            {
                "type": "user",
                "message": {
                    "content": [{"type": "tool_result", "tool_use_id": "toolu_1", "content": [{"text": payload}]}]
                },
            },
        )
    )

    tool_results = [item for item in report.items if item.item_type == "tool_result"]
    assert [(item.chars, item.preview) for item in tool_results] == [(len(payload), payload[:120])]
    tool_result_row = next(
        line
        for line in analyzer.render_content_breakdown(report).splitlines()
        if line.strip().startswith("tool_result")
    )
    assert tool_result_row.split()[1:3] == ["1", f"{len(payload):,}"]
    assert analyzer.render_top_items(report).count("unique-large-prefix") == 1


def test_rich_provider_control_result_with_suffix_is_operational_friction(tmp_path: Path) -> None:
    analyzer = load_analyzer()
    path = tmp_path / "coder-1-20260810-144354.txt"
    path.write_text(
        "\n".join(
            as_lines(
                {"type": "system", "session_id": "s", "model": "claude-opus"},
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
                                "input": {
                                    "command": "§BRAND_BINARY_NAME§ await-verdict task-111 --agent-id coder-1 --json"
                                },
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
                                "is_error": False,
                                "content": PROVIDER_BACKGROUND_RESULT_WITH_SUFFIX,
                            }
                        ]
                    },
                },
                {"type": "result", "usage": {}, "total_cost_usd": 1.25},
            )
        ),
        encoding="utf-8",
    )

    report = analyzer.analyze_file(str(path))
    assert report is not None
    assert len(report.operational_frictions) == 1
    friction = report.operational_frictions[0]
    assert friction.category == "provider foreground timeout/backgrounding"
    assert friction.tool_name == "Bash"
    assert "await-verdict" in friction.command_preview
    assert friction.duration_ms == 180_000
    assert friction.log_file == path.name
    assert friction.role == "coder-1"
    assert friction.result_preview == PROVIDER_BACKGROUND_RESULT_WITH_SUFFIX[:120]
    assert report.actions[0].is_error is False
    assert analyzer._permission_frictions_for_report(report) == []

    rendered = analyzer.render_report(report)
    assert rendered.count("provider foreground timeout/backgrounding") == 1
    assert rendered.index("OPERATIONAL FRICTION") < rendered.index("TOKEN SUMMARY")
    role_summary = analyzer.render_role_summary([report])
    assert "OPERATIONAL FRICTION" in role_summary
    assert "coder-1" in role_summary
    assert path.name in role_summary
    assert "Errors:        0" in role_summary


def test_rich_timestamp_duration_takes_precedence_over_provider_duration() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude-opus"},
            {
                "type": "assistant",
                "timestamp": "2026-08-10T14:43:54Z",
                "message": {
                    "id": "m1",
                    "usage": {},
                    "content": [
                        {
                            "type": "tool_use",
                            "id": "toolu_1",
                            "name": "Bash",
                            "input": {"command": "build"},
                        }
                    ],
                },
            },
            {
                "type": "user",
                "timestamp": "2026-08-10T14:46:55Z",
                "message": {
                    "content": [
                        {
                            "type": "tool_result",
                            "tool_use_id": "toolu_1",
                            "is_error": False,
                            "content": PROVIDER_BACKGROUND_RESULT_WITH_SUFFIX,
                        }
                    ]
                },
            },
        )
    )

    assert report.operational_frictions[0].duration_ms == 181_000


def test_rich_operational_friction_without_reported_duration_renders_na() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude-opus"},
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
                            "input": {"command": "build"},
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
                            "is_error": False,
                            "content": "Command timed out and was moved to the background.",
                        }
                    ]
                },
            },
        )
    )

    assert report.operational_frictions[0].duration_ms == 0
    assert "n/a" in analyzer.render_operational_friction(report)
    category, duration_ms = analyzer._operational_friction_details(
        "Command did not complete within its " + ("9" * 400) + "s timeout and was moved to the background"
    )
    assert category == "provider foreground timeout/backgrounding"
    assert duration_ms == 0


def test_rich_operational_friction_ignores_errors_and_deliberate_backgrounding() -> None:
    analyzer = load_analyzer()
    commands_and_results = [
        ("false", "process exited with code 1", True),
        ("mdtoc specs/story.md", "This command requires approval", True),
        ("echo ok", "ok", False),
        ("nohup build >build.log 2>&1 &", "started background job", False),
        (
            "gh issue view 111",
            (
                f"Issue evidence quotes: {PROVIDER_BACKGROUND_RESULT_WITH_SUFFIX}. "
                "This successful output is not a provider control message."
            ),
            False,
        ),
        (
            "build",
            "Command did not complete within its 180s timeout but remained in the foreground",
            False,
        ),
    ]
    events: list[dict[str, Any]] = [{"type": "system", "session_id": "s", "model": "claude-opus"}]
    for index, (command, result, is_error) in enumerate(commands_and_results):
        tool_id = f"toolu_{index}"
        events.extend(
            [
                {
                    "type": "assistant",
                    "message": {
                        "id": f"m{index}",
                        "usage": {},
                        "content": [{"type": "tool_use", "id": tool_id, "name": "Bash", "input": {"command": command}}],
                    },
                },
                {
                    "type": "user",
                    "message": {
                        "content": [
                            {
                                "type": "tool_result",
                                "tool_use_id": tool_id,
                                "is_error": is_error,
                                "content": result,
                            }
                        ]
                    },
                },
            ]
        )

    report = analyzer.parse_rich(as_lines(*events))

    assert report.operational_frictions == []
    assert sum(action.is_error for action in report.actions) == 2


def test_rich_empty_turns_require_no_meaningful_text_and_no_tool_activity() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude-opus"},
            {
                "type": "assistant",
                "message": {"id": "m1", "usage": {}, "content": [{"type": "text", "text": "final summary"}]},
            },
            {
                "type": "assistant",
                "message": {"id": "m2", "usage": {}, "content": [{"type": "text", "text": "   "}]},
            },
            {"type": "assistant", "message": {"id": "m3", "usage": {}, "content": []}},
        )
    )

    assert [empty.detail for empty in report.empty_turns] == ["m2", "m3"]
    rendered = analyzer.render_empty_turns(report)
    assert "66.67%" in rendered
    assert "final summary" not in rendered


def test_breadcrumb_applicability_and_separator_normalization() -> None:
    analyzer = load_analyzer()
    claude_report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "provider": "claude", "model": "claude-opus"},
            {"type": "assistant", "message": {"id": "m1", "usage": {}, "content": []}},
        )
    )
    non_claude_report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "provider": "opencode", "model": "claude-opus"},
            {"type": "assistant", "message": {"id": "m1", "usage": {}, "content": []}},
        )
    )
    native_rich_report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "model": "claude-opus"},
            {"type": "assistant", "message": {"id": "m1", "usage": {}, "content": []}},
        )
    )
    sparse_report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t", "provider": "cursor", "model": "claude-opus"},
            {"type": "turn.started"},
            {
                "type": "item.completed",
                "item": {"type": "agent_message", "status": "completed", "text": "meaningful final text"},
            },
            {"type": "turn.completed", "usage": {}},
        )
    )

    assert claude_report.breadcrumb_applicability == "not required"
    assert "Applicability: not required" in analyzer.render_secret_words(claude_report)
    assert "Status: missing" not in analyzer.render_secret_words(claude_report)
    assert non_claude_report.breadcrumb_applicability == "required"
    assert "Status: missing" in analyzer.render_secret_words(non_claude_report)
    assert native_rich_report.breadcrumb_applicability == "not required"
    assert "Status: missing" not in analyzer.render_secret_words(native_rich_report)
    assert sparse_report.breadcrumb_applicability == "required"
    assert sparse_report.empty_turns == []
    assert analyzer._parse_secret_words("Acme / MAS / Empowered / On-rails") == [
        "Acme",
        "MAS",
        "Empowered",
        "On-rails",
    ]
    assert analyzer._parse_secret_words("Acme MAS Empowered On-rails") == [
        "Acme",
        "MAS",
        "Empowered",
        "On-rails",
    ]


def test_native_claude_rich_init_fixture_is_exempt_but_explicit_opencode_is_not() -> None:
    analyzer = load_analyzer()
    # Redacted to the identity-bearing fields from the reported native Claude
    # stream-json init event; the source log itself is not retained in the repo.
    native_report = analyzer.parse_rich(NATIVE_CLAUDE_RICH_INIT_FIXTURE.read_text().splitlines())
    opencode_report = analyzer.parse_rich(
        as_lines(
            {
                "type": "system",
                "subtype": "init",
                "session_id": "opencode",
                "provider": "opencode",
                "model": "claude-opus-5[1m]",
            }
        )
    )

    assert native_report.meta.provider == ""
    assert native_report.breadcrumb_applicability == "not required"
    assert opencode_report.breadcrumb_applicability == "required"


def test_rich_provider_only_metadata_controls_rendered_breadcrumb_applicability() -> None:
    analyzer = load_analyzer()
    claude_report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "claude", "provider": "claude"},
            {"type": "assistant", "message": {"id": "m1", "usage": {}, "content": []}},
        )
    )
    non_claude_report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "codex", "provider": "gpt"},
            {"type": "assistant", "message": {"id": "m1", "usage": {}, "content": []}},
        )
    )

    assert claude_report.breadcrumb_applicability == "not required"
    assert "Applicability: not required" in analyzer.render_secret_words(claude_report)
    assert "Status: missing" not in analyzer.render_secret_words(claude_report)
    assert non_claude_report.breadcrumb_applicability == "required"
    assert "Applicability: required" in analyzer.render_secret_words(non_claude_report)
    assert "Status: missing" in analyzer.render_secret_words(non_claude_report)


def test_rich_breadcrumb_detection_skips_initial_tool_only_envelope() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "provider": "gpt"},
            {
                "type": "assistant",
                "message": {
                    "id": "m1",
                    "usage": {},
                    "content": [{"type": "tool_use", "id": "tool-1", "name": "Read", "input": {}}],
                },
            },
            {
                "type": "assistant",
                "message": {
                    "id": "m2",
                    "usage": {},
                    "content": [{"type": "text", "text": "Secret words: Acme / MAS / Empowered / On-rails"}],
                },
            },
        )
    )

    assert report.secret_words_lines == ["Secret words: Acme / MAS / Empowered / On-rails"]
    assert "Found: Acme, MAS, Empowered, On-rails" in analyzer.render_secret_words(report)


def test_rich_breadcrumb_detection_combines_first_message_envelopes() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "s", "provider": "gpt"},
            {
                "type": "assistant",
                "message": {"id": "m1", "usage": {}, "content": [{"type": "text", "text": "Initializing"}]},
            },
            {
                "type": "assistant",
                "message": {
                    "id": "m1",
                    "usage": {},
                    "content": [{"type": "text", "text": "Secret words: Acme / MAS / Empowered / On-rails"}],
                },
            },
            {
                "type": "assistant",
                "message": {"id": "m2", "usage": {}, "content": [{"type": "text", "text": "Later response"}]},
            },
        )
    )

    assert report.secret_words_lines == ["Secret words: Acme / MAS / Empowered / On-rails"]
    assert "Found: Acme, MAS, Empowered, On-rails" in analyzer.render_secret_words(report)


def test_sparse_breadcrumb_detection_skips_empty_initial_message_envelope() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t", "provider": "gpt"},
            {"type": "turn.started"},
            {
                "type": "item.completed",
                "item": {"type": "agent_message", "status": "completed", "text": ""},
            },
            {"type": "turn.completed", "usage": {}},
            {"type": "turn.started"},
            {
                "type": "item.completed",
                "item": {
                    "type": "agent_message",
                    "status": "completed",
                    "text": "Secret words: Acme / MAS / Empowered / On-rails",
                },
            },
            {"type": "turn.completed", "usage": {}},
        )
    )

    assert report.secret_words_lines == ["Secret words: Acme / MAS / Empowered / On-rails"]
    assert "Found: Acme, MAS, Empowered, On-rails" in analyzer.render_secret_words(report)


def test_secret_word_parsing_stops_at_sentence_terminator() -> None:
    analyzer = load_analyzer()

    assert analyzer._parse_secret_words(
        "Secret words: Acme / MAS / Empowered / On-rails. Initialization complete."
    ) == ["Acme", "MAS", "Empowered", "On-rails"]


def test_sparse_breadcrumb_detection_combines_messages_in_first_assistant_turn() -> None:
    analyzer = load_analyzer()
    report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "t", "provider": "gpt"},
            {"type": "turn.started"},
            {
                "type": "item.completed",
                "item": {"type": "agent_message", "status": "completed", "text": "Initializing"},
            },
            {
                "type": "item.completed",
                "item": {
                    "type": "agent_message",
                    "status": "completed",
                    "text": "Secret words: Acme / MAS / Empowered / On-rails",
                },
            },
            {"type": "turn.completed", "usage": {}},
            {"type": "turn.started"},
            {
                "type": "item.completed",
                "item": {"type": "agent_message", "status": "completed", "text": "Later response"},
            },
            {"type": "turn.completed", "usage": {}},
        )
    )

    assert report.secret_words_lines == ["Secret words: Acme / MAS / Empowered / On-rails"]
    assert "Found: Acme, MAS, Empowered, On-rails" in analyzer.render_secret_words(report)


def test_breadcrumb_search_stops_after_five_meaningful_text_blocks() -> None:
    analyzer = load_analyzer()
    preface = [f"initialization text {index}" for index in range(5)]
    secret_words = "Secret words: Acme / MAS / Empowered / On-rails"

    rich_report = analyzer.parse_rich(
        as_lines(
            {"type": "system", "session_id": "rich", "provider": "opencode"},
            *(
                {
                    "type": "assistant",
                    "message": {
                        "id": f"m{index}",
                        "usage": {},
                        "content": [{"type": "text", "text": text}],
                    },
                }
                for index, text in enumerate([*preface, secret_words])
            ),
        )
    )
    sparse_report = analyzer.parse_sparse(
        as_lines(
            {"type": "thread.started", "thread_id": "sparse", "provider": "cursor"},
            {"type": "turn.started"},
            *(
                {
                    "type": "item.completed",
                    "item": {"type": "agent_message", "status": "completed", "text": text},
                }
                for text in [*preface, secret_words]
            ),
            {"type": "turn.completed", "usage": {}},
        )
    )

    assert rich_report.secret_words_lines == []
    assert sparse_report.secret_words_lines == []
