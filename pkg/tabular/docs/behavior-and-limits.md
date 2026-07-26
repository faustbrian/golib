# Behavior and limits

Zero-valued limits select safe defaults:

| Limit | Default |
| --- | ---: |
| Fixed-width record | 1 MiB |
| XLS workbook | 64 MiB |
| ZIP entries | 1,000 |
| ZIP single entry | 1 GiB |
| ZIP expanded total | 4 GiB |

Applications should normally choose smaller values consistent with their
upload and job policies. ZIP limits use central-directory declarations and
entry reads still verify CRC data. Unsafe absolute, parent, backslash, empty,
and duplicate entry names are rejected. Callers may also reject symbolic links
and bound each entry's expanded-to-compressed byte ratio. Those two policies
are opt-in for compatibility. This package never writes ZIP entries to the
filesystem.

These payload limits are not heap guarantees. In particular, XLSX validation
and Excelize may allocate substantially more than the compressed workbook
size. Applications processing untrusted files should combine package limits
with job-level memory and execution limits. See
[performance and memory verification](performance.md).

Delimited record and field limits are opt-in so zero-valued configurations
preserve the package's existing behavior and throughput. Applications reading
untrusted delimited sources must set both limits explicitly. Record limits
count the bytes presented to the parser, including delimiters and line endings.
Quoted multiline fields remain part of one logical record. The reader stops
the raw stream before the CSV parser can allocate past the configured record
bound. Field limits count parsed UTF-8 bytes before optional normalization.
Both failures use `ErrorLimitExceeded`; row and field coordinates are one-based
when available.

Spreadsheet record and field limits are also opt-in. They are enforced on
parsed XLS and XLSX cell values before normalization or caller delivery.
Record limits count the sum of bytes that would be delivered for one worksheet
row, including preserved spreadsheet error text. Field limits apply the same
rule to one cell and report its one-based coordinate.
XLSX callers may also set an explicit maximum worksheet count before selecting
the first or named sheet.
Archive and workbook limits still bound the underlying parser; parsed limits
do not claim to prevent allocations inside the XLS or Excelize engines.

`Read` intentionally exposes the compatibility `[]string` shape. Callers whose
business contract distinguishes a missing cell from an explicitly stored empty
cell must set `PreserveCellPresence` and use `ReadCells`. Each returned
`SpreadsheetCell` reports its normalized value and whether the workbook stored
that position. The option is disabled by default so string-only XLSX reads
retain their optimized cell-type lookup behavior and neither format allocates
per-cell presence storage. Enabled XLSX reads stream the selected worksheet XML
alongside Excelize so numeric and stored-empty trailing cells remain present.

Row normalization is ordered: trim whitespace, then replace empty values.
Header normalization removes a UTF-8 BOM from field one, trims, changes case,
applies exact replacements, then validates empty and duplicate names.

Fixed-width offsets refer to bytes in the original encoding. They must not
split a multi-byte UTF-8 character. ISO-8859-1 and Windows-1252 map every byte;
invalid UTF-8 is rejected instead of replaced.

With a header configured, the first record is consumed once and never returned
by `Read`. Without a header configuration, `Header` returns nil and consumes
nothing. Fixed field counts reject long rows and pad short spreadsheet rows;
variable mode preserves source width.
