#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CORE_DOCUMENTS = [
    Path("README.md"),
    Path("docs/README.md"),
    Path("docs/quickstart.md"),
    Path("docs/api.md"),
    Path("docs/architecture.md"),
    Path("docs/pool-and-lifecycle.md"),
    Path("docs/tls.md"),
    Path("docs/transactions.md"),
    Path("docs/errors.md"),
    Path("docs/sqlc.md"),
    Path("docs/observability.md"),
    Path("docs/migrations.md"),
    Path("docs/testing.md"),
    Path("docs/kubernetes.md"),
    Path("docs/faq.md"),
    Path("docs/compatibility.md"),
    Path("docs/migration.md"),
    Path("docs/security.md"),
    Path("docs/hardening.md"),
    Path("docs/performance.md"),
    Path("docs/releasing.md"),
    Path("docs/repository-standards.md"),
    Path("CONTRIBUTING.md"),
    Path("SECURITY.md"),
    Path("ROADMAP.md"),
    Path("CHANGELOG.md"),
]


def title(path: Path) -> str:
    for line in (ROOT / path).read_text(encoding="utf-8").splitlines():
        if line.startswith("# "):
            return line[2:]
    return path.stem.replace("-", " ").title()


def render_index() -> str:
    lines = [
        "# postgres",
        "",
        "> Documentation map for users, operators, and contributors.",
        "",
        "- [Complete documentation bundle](llms-full.txt)",
    ]
    lines.extend(f"- [{title(path)}]({path.as_posix()})" for path in CORE_DOCUMENTS)
    return "\n".join(lines) + "\n"


def render_full() -> str:
    sections = ["# postgres Complete Documentation", ""]
    for path in CORE_DOCUMENTS:
        content = (ROOT / path).read_text(encoding="utf-8").rstrip()
        sections.extend([f"## Source: {path.as_posix()}", "", content, ""])
    return "\n".join(sections).rstrip() + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    outputs = {
        ROOT / "llms.txt": render_index(),
        ROOT / "llms-full.txt": render_full(),
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
