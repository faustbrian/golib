# Third-Party Notices

## Policy

This file is mandatory even when no copied or vendored code requires a notice.
It records attribution for copied, forked, generated, or vendored source.
Ordinary Go module dependencies retain their upstream licenses and notices.

## XLS Reader Provenance

The internal XLS reader is informed by `github.com/millken/xls`, originally
licensed under the Apache License, Version 2.0. It has been substantially
reduced and modified for bounded, read-only BIFF8 ingestion. Preserve
[`internal/xls/NOTICE.md`](internal/xls/NOTICE.md) with that code.

## Module Dependencies

XLSX support uses `github.com/xuri/excelize/v2`; text conversion uses
`golang.org/x/text`. These dependencies retain their upstream licenses and
notices.
