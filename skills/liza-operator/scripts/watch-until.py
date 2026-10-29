#!/usr/bin/env python3
"""One operator watch leg for a running §BRAND_NAME_TITLE§ run.

Exits on the first event worth an operator turn and prints one header line
(EVENT / RUN COMPLETE / MODE CHANGE / TICK) plus any alert lines:

- any severity-marked alert line (🚨 or ⚠️) appended to alerts.log since the leg
  started, except lines matching a noise pattern;
- a run-mode change;
- run completion (status progress N/N);
- the round tick.

Relaunch it after handling each exit, so the watch lives for the run instead of
a timer. Alerts are matched by severity marker rather than by an allow-list of
alert types, so a new alert type cannot slip through unnoticed.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
import time
from collections.abc import Callable
from datetime import UTC, datetime
from pathlib import Path

ALERT_RE = re.compile(r"^\[[^\]]+\] (🚨|⚠️)")
PROGRESS_RE = re.compile(r"Progress:\s*(\d+)/(\d+)")
DEFAULT_NOISE = ("INVALID STATE: zombie",)


def new_alerts(data: bytes, noise: tuple[str, ...]) -> list[str]:
    """Return severity-marked alert lines in `data` that match no noise pattern."""
    hits = []
    for line in data.decode("utf-8", errors="replace").splitlines():
        if ALERT_RE.match(line) and not any(n in line for n in noise):
            hits.append(line[:260])
    return hits


def parse_progress(status_text: str) -> tuple[int, int] | None:
    match = PROGRESS_RE.search(status_text)
    return (int(match.group(1)), int(match.group(2))) if match else None


def cli_output(binary: str, root: Path, *args: str) -> str:
    # The CLI prints human output on stderr; read both streams.
    result = subprocess.run([binary, "-C", str(root), *args], capture_output=True, text=True, check=False)
    return result.stdout + result.stderr


def stamp() -> str:
    return datetime.now(UTC).strftime("%H:%MZ")


def watch(
    root: Path,
    binary: str,
    tick: int,
    poll: int,
    probe_every: int,
    noise: tuple[str, ...],
    run_cli: Callable[..., str] = cli_output,
    sleep: Callable[[float], None] = time.sleep,
    now: Callable[[], float] = time.monotonic,
) -> list[str]:
    log = root / "§BRAND_PROJECT_DIRNAME§" / "alerts.log"
    offset = log.stat().st_size if log.exists() else 0
    start = now()
    mode0 = run_cli(binary, root, "get", "config.mode").strip().splitlines()[-1:] or [""]
    progress, mode = None, mode0[0]
    passes = 0
    while True:
        size = log.stat().st_size if log.exists() else 0
        if size < offset:
            offset = 0
        if size > offset:
            with log.open("rb") as handle:
                handle.seek(offset)
                hits = new_alerts(handle.read(size - offset), noise)
            offset = size
            if hits:
                return [f"EVENT {stamp()}", *hits]
        if passes % probe_every == 0:
            progress = parse_progress(run_cli(binary, root, "status"))
            if progress and progress[1] > 0 and progress[0] == progress[1]:
                return [f"RUN COMPLETE {stamp()} {progress[0]}/{progress[1]}"]
            lines = run_cli(binary, root, "get", "config.mode").strip().splitlines()
            mode = lines[-1] if lines else mode
            if mode and mode != mode0[0]:
                return [f"MODE CHANGE {stamp()} {mode0[0]} -> {mode}"]
        passes += 1
        if now() - start >= tick:
            shown = f"{progress[0]}/{progress[1]}" if progress else "?"
            return [f"TICK {stamp()} watch round due ({shown}, {mode})"]
        sleep(poll)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=(__doc__ or "").splitlines()[0])
    parser.add_argument("--project-root", type=Path, default=Path.cwd())
    parser.add_argument("--binary", default="§BRAND_BINARY_NAME§")
    parser.add_argument("--tick", type=int, default=600, help="seconds until a round tick")
    parser.add_argument("--poll", type=int, default=20, help="seconds between alerts.log polls")
    parser.add_argument("--probe-every", type=int, default=3, help="CLI state probe every N polls (heavy CLI)")
    parser.add_argument("--noise", action="append", help="substring of alert lines to ignore (repeatable)")
    args = parser.parse_args(argv)
    noise = tuple(args.noise) if args.noise else DEFAULT_NOISE
    for line in watch(args.project_root, args.binary, args.tick, args.poll, args.probe_every, noise):
        print(line, flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
