from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
from typing import Any

import pytest


def load_watch_until() -> Any:
    path = Path(__file__).with_name("watch-until.py")
    spec = importlib.util.spec_from_file_location("watch_until", path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


wu = load_watch_until()
PROJECT_DIR = "§BRAND_PROJECT_DIRNAME§"


def test_new_alerts_matches_any_severity_marker_including_unlisted_types() -> None:
    data = (
        "[2026-10-12T04:35:08Z] 🚨 REGISTERED AGENT PROCESS: agent a-1 has active lease\n"
        "[2026-10-12T04:39:54Z] ⚠️ CHECKPOINT SUMMARY FAILED: not written\n"
        "[2026-10-12T04:40:00Z] 🚨 SOME FUTURE ALERT TYPE: x\n"
        "plain continuation line\n"
    ).encode()
    hits = wu.new_alerts(data, ())
    assert [h.split("] ", 1)[1][:4] for h in hits] == ["🚨 RE", "⚠️ C", "🚨 SO"]


def test_new_alerts_drops_noise() -> None:
    data = "[t] 🚨 INVALID STATE: zombie omni process detected\n[t] ⚠️ STALLED: no progress\n".encode()
    assert wu.new_alerts(data, wu.DEFAULT_NOISE) == ["[t] ⚠️ STALLED: no progress"]


def test_parse_progress() -> None:
    assert wu.parse_progress("Goal\nProgress: 843/979 tasks complete\n") == (843, 979)
    assert wu.parse_progress("no progress line") is None


class FakeRun:
    def __init__(self, root: Path, progress: str = "1/2", modes: list[str] | None = None) -> None:
        self.root, self.progress, self.modes = root, progress, modes or ["RUNNING"]

    def __call__(self, binary: str, root: Path, *args: str) -> str:
        if args == ("status",):
            return f"Progress: {self.progress} tasks complete\n"
        return (self.modes.pop(0) if len(self.modes) > 1 else self.modes[0]) + "\n"


def make_root(tmp_path: Path, initial: str = "") -> Path:
    (tmp_path / PROJECT_DIR).mkdir()
    (tmp_path / PROJECT_DIR / "alerts.log").write_text(initial)
    return tmp_path


def run(root: Path, fake: FakeRun, on_sleep: Any = None, tick: int = 1000) -> list[str]:
    clock = {"t": 0.0}

    def sleep(seconds: float) -> None:
        clock["t"] += seconds
        if on_sleep:
            on_sleep()

    return wu.watch(root, "cli", tick, 20, 3, wu.DEFAULT_NOISE, run_cli=fake, sleep=sleep, now=lambda: clock["t"])


def test_alert_appended_after_start_ends_the_leg_and_earlier_alerts_do_not(tmp_path: Path) -> None:
    root = make_root(tmp_path, "[old] 🚨 BLOCKED: before the leg\n")
    log = root / PROJECT_DIR / "alerts.log"

    def append() -> None:
        with log.open("a") as handle:
            handle.write("[new] ⚠️ LEASE EXPIRED: coder-1\n")

    out = run(root, FakeRun(root), on_sleep=append)
    assert out[0].startswith("EVENT ") and out[1:] == ["[new] ⚠️ LEASE EXPIRED: coder-1"]


def test_run_complete(tmp_path: Path) -> None:
    out = run(make_root(tmp_path), FakeRun(tmp_path, progress="979/979"))
    assert out[0].startswith("RUN COMPLETE ") and out[0].endswith("979/979")


def test_zero_tasks_is_not_complete(tmp_path: Path) -> None:
    out = run(make_root(tmp_path), FakeRun(tmp_path, progress="0/0"), tick=60)
    assert out[0].startswith("TICK ") and "(0/0, RUNNING)" in out[0]


def test_mode_change(tmp_path: Path) -> None:
    out = run(make_root(tmp_path), FakeRun(tmp_path, modes=["RUNNING", "PAUSED"]))
    assert out[0].startswith("MODE CHANGE ") and out[0].endswith("RUNNING -> PAUSED")


def test_tick_when_quiet(tmp_path: Path) -> None:
    out = run(make_root(tmp_path), FakeRun(tmp_path), tick=60)
    assert out[0].startswith("TICK ") and "(1/2, RUNNING)" in out[0]


@pytest.mark.parametrize("noise_line", ["[t] 🚨 INVALID STATE: zombie x\n"])
def test_noise_only_does_not_end_the_leg(tmp_path: Path, noise_line: str) -> None:
    root = make_root(tmp_path)
    log = root / PROJECT_DIR / "alerts.log"
    appended = {"done": False}

    def append_once() -> None:
        if not appended["done"]:
            with log.open("a") as handle:
                handle.write(noise_line)
            appended["done"] = True

    out = run(root, FakeRun(root), on_sleep=append_once, tick=60)
    assert out[0].startswith("TICK ")
