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
and duplicate entry names are rejected. This package never writes ZIP entries
to the filesystem.

These payload limits are not heap guarantees. In particular, XLSX validation
and Excelize may allocate substantially more than the compressed workbook
size. Applications processing untrusted files should combine package limits
with job-level memory and execution limits. See
[performance and memory verification](performance.md).

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
