# Fixture inventory

Fixtures are intentionally small enough for normal test runs while retaining
real format structure and import semantics. Large-input behavior is generated
deterministically by benchmarks instead of storing bulky derived files.

| Format | Realistic fixture | Malformed fixture | Contract proved |
| --- | --- | --- | --- |
| CSV | `delimited/realistic.csv` | `delimited/malformed.csv` | BOM, spaces, embedded delimiters, quoted newlines, Nordic text |
| Semicolon CSV | `delimited/semicolon.csv` | `delimited/malformed.csv` | locale delimiter, decimal commas, comments, trailing fields |
| Fixed-width | `fixedwidth/nordic-utf8.txt`, `fixedwidth/nordic-latin1.txt` | `fixedwidth/malformed-short.txt` | byte ranges, short rows, UTF-8 and Latin-1 Nordic text |
| XLS | `spreadsheet/table.xls` | `spreadsheet/malformed.xls` | real OLE2/BIFF8 workbook and invalid container rejection |
| XLSX | `spreadsheet/sample.xlsx` | `spreadsheet/malformed.xlsx` | real OOXML workbook and invalid container rejection |
| ZIP | `archive/import.zip` | `archive/broken.zip` | exact entry ingest and broken central-directory rejection |

The `.base64` siblings preserve binary fixture provenance and make accidental
binary changes reviewable. Tests consume the decoded binary files directly.
