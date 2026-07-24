# End-to-end examples

## ZIP-backed semicolon import

```go
archive, err := tabular.OpenZIP(file, size, tabular.ZIPConfig{
    MaxEntries: 20, MaxEntryBytes: 64 << 20, MaxTotalBytes: 64 << 20,
})
if err != nil { return err }

entry, err := archive.Open("orders.csv")
if err != nil { return err }
defer entry.Close()

reader, err := tabular.NewDelimitedReader(entry, tabular.DelimitedConfig{
    Delimiter: ';', AllowVariableFields: false, FieldsPerRecord: 4,
})
```

## Spreadsheet import

```go
reader, err := tabular.OpenSpreadsheet(file, size, tabular.SpreadsheetConfig{
    Format: tabular.FormatXLSX,
    Sheet: "Orders",
    Header: &tabular.HeaderConfig{RejectEmpty: true, RejectDuplicates: true},
    ZIP: tabular.ZIPConfig{MaxEntryBytes: 32 << 20, MaxTotalBytes: 64 << 20},
})
if err != nil { return err }
defer reader.Close()
```

Complete compilable delimited and fixed-width examples live in
`example_test.go` and are executed by `go test`.
