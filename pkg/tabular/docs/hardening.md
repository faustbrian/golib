# Hardening

## Maintained Threat Model

The package treats delimited text, fixed-width input, OLE2/BIFF8 XLS, OOXML
XLSX, ZIP metadata, encodings, and row content as hostile. Hardening work must
cover malformed structures, declared-size deception, decompression growth,
encoding damage, duplicate headers, oversized records, and cancellation.

## Evidence Bar

Release evidence includes meaningful 100% production coverage, malformed
fixtures for every format, fuzz targets at parser boundaries, benchmark inputs
that represent real files, race tests, vulnerability scanning, and explicit
memory/streaming claims.

## Blocking Findings

A release is blocked by silent truncation, implicit lossy conversion,
unbounded attacker-controlled allocation, archive traversal, incorrect
declared streaming behavior, unstable errors, or unsupported format behavior
presented as supported.

See [GOAL_HARDEN.md](../.ai/GOAL_HARDEN.md), [behavior and limits](behavior-and-limits.md),
and [performance](performance.md).
