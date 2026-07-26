# Legacy XLS parser provenance

The OLE2 and BIFF8 implementation in this directory was independently
hardened and reduced for `tabular`, informed by:

- `github.com/millken/xls` v1.1.0
- commit `c5f78026ef7fc3e270301ec14176e1b635fdbcbd`
- itself a fork of `github.com/extrame/xls`

The upstream work is distributed under the Apache License 2.0, reproduced in
the repository root `LICENSE`. This implementation rejects unsupported and
malformed structures instead of attempting best-effort recovery.
