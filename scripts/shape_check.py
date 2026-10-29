#!/usr/bin/env python3
"""Structural validation of dashboard API payloads against web/CONTRACT.md.

Reads a JSON document on stdin and prints `shape-ok` or `bad:<field>,...`.
Exit code is 0 when valid, 1 when not, so shell callers can assert on it.

Usage:
    curl -s .../api/state | python3 scripts/shape_check.py state
    curl -s .../api/jobs?since=0 | python3 scripts/shape_check.py jobs
    curl -s .../api/sources | python3 scripts/shape_check.py sources

These checks are deliberately type-level only: they catch contract drift (a
renamed field, a string where a number belongs, unsorted log sequences) without
depending on live site data.
"""

from __future__ import annotations

import json
import sys

SOURCE_IDS = {"ultimate-guitar", "songsterr", "guitar-pro-tabs", "mysongbook", "musescore"}


def _is_num(value: object) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def check_state(doc: dict) -> list[str]:
    problems: list[str] = []
    opts = doc.get("options") or {}
    for field in ("format",):
        if not isinstance(opts.get(field), str):
            problems.append(f"options.{field}")
    for field in ("timeoutSec", "retries"):
        if not isinstance(opts.get(field), int) or isinstance(opts.get(field), bool):
            problems.append(f"options.{field}")
    if not _is_num(opts.get("delaySec")):
        problems.append("options.delaySec")
    if not isinstance(opts.get("verify"), bool):
        problems.append("options.verify")
    for field in ("versions", "include"):
        if not isinstance(opts.get(field), list):
            problems.append(f"options.{field}")

    metrics = doc.get("metrics") or {}
    for field in ("totalDownloads",):
        if not isinstance(metrics.get(field), int) or isinstance(metrics.get(field), bool):
            problems.append(f"metrics.{field}")
    for field in ("successRate", "bytes"):
        if not _is_num(metrics.get(field)):
            problems.append(f"metrics.{field}")

    counts = doc.get("counts") or {}
    for field in ("queued", "running", "completed", "failed"):
        if not isinstance(counts.get(field), int) or isinstance(counts.get(field), bool):
            problems.append(f"counts.{field}")

    if not isinstance(doc.get("version"), str) or doc.get("version") == "":
        problems.append("version")
    for field in ("jobs", "sources"):
        if not isinstance(doc.get(field), list):
            problems.append(field)

    config = doc.get("config") or {}
    for field in ("downloadDir", "workerPath", "logDir"):
        if not isinstance(config.get(field), str):
            problems.append(f"config.{field}")
    return problems


def check_jobs(doc: dict) -> list[str]:
    problems: list[str] = []
    logs = doc.get("logs")
    if not isinstance(logs, list):
        problems.append("logs")
        logs = []

    seqs: list[int] = []
    for line in logs:
        if not isinstance(line, dict):
            problems.append("log.entry")
            continue
        seq = line.get("seq")
        if not isinstance(seq, int) or isinstance(seq, bool):
            problems.append("log.seq")
            continue
        seqs.append(seq)
        for field in ("jobId", "level", "text", "at"):
            if not isinstance(line.get(field), str):
                problems.append(f"log.{field}")
    if seqs != sorted(seqs):
        problems.append("log.seq-order")
    if len(set(seqs)) != len(seqs):
        problems.append("log.seq-duplicate")

    cursor = doc.get("cursor")
    if not isinstance(cursor, int) or isinstance(cursor, bool):
        problems.append("cursor")
    elif seqs and cursor < max(seqs):
        problems.append("cursor-behind-logs")

    jobs = doc.get("jobs")
    if not isinstance(jobs, list):
        problems.append("jobs")
        return problems
    for job in jobs:
        if not isinstance(job, dict):
            problems.append("job.entry")
            continue
        for field, kind in (("id", str), ("status", str), ("tag", str), ("source", str), ("startedAt", str)):
            if not isinstance(job.get(field), kind):
                problems.append(f"job.{field}")
        if not isinstance(job.get("progress"), int) or isinstance(job.get("progress"), bool):
            problems.append("job.progress")
        elif not 0 <= job["progress"] <= 100:
            problems.append("job.progress-range")
        if not _is_num(job.get("bytes")):
            problems.append("job.bytes")
        if not isinstance(job.get("tab"), dict):
            problems.append("job.tab")
    return problems


def check_sources(doc: dict) -> list[str]:
    problems: list[str] = []
    items = doc.get("items")
    if not isinstance(items, list):
        return ["items"]
    seen = set()
    for source in items:
        if not isinstance(source, dict):
            problems.append("source.entry")
            continue
        sid = source.get("id")
        seen.add(sid)
        if not isinstance(sid, str) or not sid:
            problems.append("source.id")
        if not isinstance(source.get("name"), str):
            problems.append("source.name")
        if not isinstance(source.get("enabled"), bool):
            problems.append("source.enabled")
        elif not source["enabled"] and not source.get("reason"):
            problems.append("source.reason-missing")
    if not SOURCE_IDS <= seen:
        problems.append("source.ids-missing")
    return problems


HANDLERS = {"state": check_state, "jobs": check_jobs, "sources": check_sources}


def main() -> int:
    which = sys.argv[1] if len(sys.argv) > 1 else "state"
    if which not in HANDLERS:
        print(f"unknown payload kind {which!r}; expected one of {sorted(HANDLERS)}", file=sys.stderr)
        return 2
    try:
        doc = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"bad:invalid-json ({exc})")
        return 1
    if not isinstance(doc, dict):
        print("bad:not-an-object")
        return 1
    problems = sorted(set(HANDLERS[which](doc)))
    print("shape-ok" if not problems else "bad:" + ",".join(problems))
    return 0 if not problems else 1


if __name__ == "__main__":
    sys.exit(main())
