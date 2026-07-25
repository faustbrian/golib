// Package tabular provides explicit, bounded readers for tabular ingest.
//
// CSV and configurable delimited input, fixed-width records, XLS, XLSX, and
// ZIP-backed sources share deterministic rows and typed errors. Parsers do not
// auto-detect formats or silently apply normalization. Callers choose the
// format, limits, header rules, and field transformations in configuration.
//
// Delimited, fixed-width, ZIP entry, and XLSX row processing stream input.
// Legacy XLS workbooks are materialized up to MaxWorkbookBytes because the
// OLE2/BIFF8 format requires random access to workbook structures.
package tabular
