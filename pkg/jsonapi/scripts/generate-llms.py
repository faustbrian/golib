#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CORE_DOCUMENTS = [
    Path("README.md"),
    Path("docs/README.md"),
    Path("docs/quickstart.md"),
    Path("docs/adoption.md"),
    Path("docs/api.md"),
    Path("docs/architecture.md"),
    Path("docs/examples.md"),
    Path("docs/cookbook.md"),
    Path("docs/faq.md"),
    Path("docs/troubleshooting.md"),
    Path("docs/migration.md"),
    Path("docs/compatibility.md"),
    Path("docs/performance.md"),
    Path("docs/hardening.md"),
    Path("docs/security.md"),
    Path("docs/releasing.md"),
    Path("CONTRIBUTING.md"),
    Path("SECURITY.md"),
    Path("ROADMAP.md"),
    Path("CHANGELOG.md"),
]


def documents() -> list[Path]:
    selected = list(CORE_DOCUMENTS)
    selected_set = set(selected)
    selected.extend(
        path.relative_to(ROOT)
        for path in sorted((ROOT / "docs").glob("*.md"))
        if path.relative_to(ROOT) not in selected_set
    )
    return selected


def title(path: Path) -> str:
    for line in (ROOT / path).read_text(encoding="utf-8").splitlines():
        if line.startswith("# "):
            return line[2:]
    return path.stem.replace("-", " ").title()


def render_index(paths: list[Path]) -> str:
    package = (ROOT / "go.mod").read_text(encoding="utf-8").splitlines()[0].split("/")[-1]
    lines = [
        f"# {package}",
        "",
        "> Documentation map for package users, adopters, operators, and contributors.",
        "",
        "- [Complete documentation bundle](llms-full.txt)",
    ]
    lines.extend(f"- [{title(path)}]({path.as_posix()})" for path in paths)
    return "\n".join(lines) + "\n"


def render_full(paths: list[Path]) -> str:
    package = (ROOT / "go.mod").read_text(encoding="utf-8").splitlines()[0].split("/")[-1]
    sections = [f"# {package} Complete Documentation", ""]
    for path in paths:
        content = (ROOT / path).read_text(encoding="utf-8").rstrip()
        sections.extend([f"## Source: {path.as_posix()}", "", content, ""])
    return "\n".join(sections).rstrip() + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()

    paths = documents()
    outputs = {
        ROOT / "llms.txt": render_index(paths),
        ROOT / "llms-full.txt": render_full(paths),
    }

    if args.check:
        stale = [
            path.name
            for path, content in outputs.items()
            if not path.exists() or path.read_text(encoding="utf-8") != content
        ]
        if stale:
            raise SystemExit("generated documentation is stale: " + ", ".join(stale))
        print("generated documentation is current")
        return 0

    for path, content in outputs.items():
        path.write_text(content, encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
