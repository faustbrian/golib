# Public API reference

The authoritative signature documentation is available through
`go doc github.com/faustbrian/golib/pkg/tabular`. This page groups the surface by
task and records compatibility-sensitive semantics.

## Rows and normalization

- `Row []string`: ordered source fields.
- `NormalizationConfig`: optional whitespace trimming and empty replacement.
- `NormalizeRow`: returns a copy.
- `HeaderConfig`, `HeaderCase`, and `NormalizeHeader`: BOM removal, trimming,
  case conversion, replacements, and empty/duplicate validation.

## Delimited text

- `DelimitedConfig`: delimiter, comments, quote policy, row shape, header, and
  normalization.
- `NewCSVReader`: always selects comma, regardless of `Delimiter`.
- `NewDelimitedReader`: requires a valid explicit delimiter.
- `DelimitedReader.Header` and `Read`: cached header and streaming rows.

## Fixed-width text

- `FixedWidthField`: named half-open byte interval `[Start, End)`.
- `FixedWidthConfig`: layout, source encoding, short/trailing record policy,
  maximum record size, and normalization.
- `NewFixedWidthReader`, `Fields`, and `Read`: validated streaming parser.
- `ExtractBytes`: non-copying checked byte slice.

## Encodings

- `EncodingUTF8`, `EncodingISO88591`, and `EncodingWindows1252` are supported.
- `DecodeBytes` validates/converts a complete value.
- `DecodeReader` returns a validating/converting streaming reader.

## Archives

- `ZIPConfig`: maximum entries, per-entry bytes, and total expanded bytes.
- `OpenZIP`: validates and indexes a random-access ZIP source.
- `ZIPArchive.Entries`, `Open`, and `Extract`: copied metadata, exact entry
  streaming, and writer-based extraction.

## Spreadsheets

- `FormatXLS` and `FormatXLSX` must be selected explicitly.
- `SpreadsheetConfig`: sheet, headers, row shape, errors, workbook limit, and
  XLSX ZIP limits.
- `OpenSpreadsheet`, `Header`, `Read`, and `Close`: common ingest lifecycle.

## Errors

`Error` includes `Kind`, operation, format, one-based row/field coordinates,
and a wrapped cause. Match stable categories with `errors.Is(err,
ErrorMalformedRow)` and inspect details with `errors.As`.

Stable kinds are `ErrorInvalidConfig`, `ErrorInvalidHeader`,
`ErrorDuplicateHeader`, `ErrorMalformedRow`, `ErrorInvalidEncoding`,
`ErrorInvalidLayout`, `ErrorArchive`, `ErrorEntryNotFound`,
`ErrorLimitExceeded`, and `ErrorSpreadsheet`.
