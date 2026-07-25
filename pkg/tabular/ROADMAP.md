# Roadmap

## First stable release

- complete and verify the CSV, XLS, XLSX, fixed-width, and ZIP-backed ingest
  surface;
- complete CI, security, documentation, and tagged-release automation;
- publish benchmark baselines and compatibility promises;
- resolve API feedback found during real service adoption;
- tag `v1.0.0` only when no known supported-format gap remains.

## After core stabilization

Potential work is evaluated independently and is not promised for `v1`:

- TSV-first convenience helpers;
- export helpers;
- schema mapping helpers;
- archive-format expansion beyond ZIP;
- carefully justified additional tabular formats.

The core will not become a workflow engine, ETL platform, queue layer, or
application-specific transformation framework.
