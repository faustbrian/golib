# Adoption guide

1. Identify the source format and encoding from its contract. Do not infer
   either from filenames alone.
2. Set file, archive, and record limits from the narrowest upstream policy.
3. Decide whether the first row is a header and make duplicate/empty handling
   explicit.
4. Decide row-width policy before processing data.
5. Keep normalization minimal and record every data-changing option.
6. Match `ErrorKind` values for operational decisions; log the full `Error`
   for coordinates and causes.
7. Drain or close readers on every path and retain the caller-owned source
   until spreadsheet processing is complete.

For ZIP-backed imports, call `OpenZIP`, select an exact entry from `Entries`,
and pass the opened entry to a streaming text reader. XLS/XLSX require
`io.ReaderAt`; copy only the selected ZIP member into a bounded random-access
store if spreadsheet files are archived.

Roll out against captured, sanitized production fixtures. Compare row counts,
headers, empty values, numeric strings, error classes, and rejected records
with the existing importer before replacing it.
