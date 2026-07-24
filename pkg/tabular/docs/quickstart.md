# Quickstart

## Choose a reader

Use `NewCSVReader` for comma-separated data, `NewDelimitedReader` for an
explicit non-comma delimiter, `NewFixedWidthReader` for byte-positioned
records, and `OpenSpreadsheet` for an explicitly selected XLS or XLSX file.
Use `OpenZIP` when an import arrives inside an archive.

Every streaming reader returns `io.EOF` after its last row. Treat any other
error as a failed import unless the application has an explicit recovery
policy.

## Configure limits

Defaults are protective, not unlimited. Set limits from the surrounding
system's upload policy when those limits are smaller. XLS uses
`MaxWorkbookBytes`; XLSX uses the limits in `SpreadsheetConfig.ZIP`.

## Handle typed errors

```go
if errors.Is(err, tabular.ErrorMalformedRow) {
    var detail *tabular.Error
    if errors.As(err, &detail) {
        log.Printf("bad row %d field %d", detail.Row, detail.Field)
    }
}
```

## Close spreadsheets and ZIP entries

Call `Close` on spreadsheet readers and ZIP entry readers. Closing a
spreadsheet does not close the caller-owned `io.ReaderAt`.
