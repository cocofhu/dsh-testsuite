#!/usr/bin/env python3
"""Seed and patch a dsh profile's pnpm-workspace.yaml so git plugins can prepare."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

ALL_BUILDS_LINE = "dangerouslyAllowAllBuilds: true"

# pnpm example:
#   @cocofhu/anime-find@https://codeload.github.com/.../tar.gz/<sha>: true
EXAMPLE_KEY = re.compile(r"(?m)^\s+(\S+): true\s*$")
GIT_PACKAGE = re.compile(r'git-hosted package "([^"]+)"')
FETCHED_FROM = re.compile(r'fetched from "([^"]+)"')
CODELOAD = re.compile(
    r"https://codeload\.(github|gitlab)\.com/([^/]+)/([^/]+)/tar\.gz/"
)


def strip_trailing_version(spec: str) -> str:
    """@scope/name@1.2.3 -> @scope/name; name@1.2.3 -> name."""
    return re.sub(r"@[^@/]+$", "", spec) if spec.count("@") >= 1 else spec


def parse_allow_keys(log: str) -> list[str]:
    keys: list[str] = []
    for match in EXAMPLE_KEY.finditer(log):
        key = match.group(1).strip().strip("\"'")
        if "@" in key and (
            "http" in key or "git+" in key or "github.com" in key or "gitlab.com" in key
        ):
            keys.append(key)

    pkg = GIT_PACKAGE.search(log)
    fetched = FETCHED_FROM.search(log)
    if pkg and fetched:
        name = strip_trailing_version(pkg.group(1))
        url = fetched.group(1)
        keys.append(f"{name}@{url}")
        host = CODELOAD.search(url)
        if host:
            keys.append(f"{name}@git+https://{host.group(1)}.com/{host.group(2)}/{host.group(3)}.git")

    seen: set[str] = set()
    out: list[str] = []
    for key in keys:
        if key not in seen:
            seen.add(key)
            out.append(key)
    return out


def ensure_all_builds(path: Path) -> bool:
    path.parent.mkdir(parents=True, exist_ok=True)
    text = path.read_text() if path.exists() else ""
    if re.search(r"(?m)^dangerouslyAllowAllBuilds:\s*true\s*$", text):
        return False
    if text and not text.endswith("\n"):
        text += "\n"
    if text:
        text += "\n"
    text += ALL_BUILDS_LINE + "\n"
    path.write_text(text)
    return True


def quote_key(key: str) -> str:
    escaped = key.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def add_allow_builds(path: Path, keys: list[str]) -> list[str]:
    if not keys:
        return []
    path.parent.mkdir(parents=True, exist_ok=True)
    text = path.read_text() if path.exists() else ""
    existing = set()
    for match in re.finditer(r'(?m)^  (?:"([^"]+)"|(\S+)): true\s*$', text):
        existing.add(match.group(1) or match.group(2))

    added = [key for key in keys if key not in existing]
    if not added:
        return []

    block = "".join(f"  {quote_key(key)}: true\n" for key in added)
    header = re.search(r"(?m)^allowBuilds:\s*\n", text)
    if header:
        at = header.end()
        text = text[:at] + block + text[at:]
    else:
        if text and not text.endswith("\n"):
            text += "\n"
        if text:
            text += "\n"
        text += "allowBuilds:\n" + block
    path.write_text(text)
    return added


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="cmd", required=True)

    ensure_p = sub.add_parser("ensure", help="set dangerouslyAllowAllBuilds")
    ensure_p.add_argument("workspace")

    allow_p = sub.add_parser("allow", help="parse a pnpm log and merge allowBuilds")
    allow_p.add_argument("workspace")
    allow_p.add_argument("log")

    args = parser.parse_args(argv)
    if args.cmd == "ensure":
        path = Path(args.workspace)
        if ensure_all_builds(path):
            print(f"set {ALL_BUILDS_LINE} in {path}", file=sys.stderr)
        return 0

    workspace = Path(args.workspace)
    log = Path(args.log).read_text(errors="replace")
    keys = parse_allow_keys(log)
    added = add_allow_builds(workspace, keys)
    if not added:
        return 1
    for key in added:
        print(f"allowBuilds: {key}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
