# FAQ

## Why no auto-detection?

Encoding and format guesses can silently reinterpret data. Source contracts
should select them explicitly.

## Why is XLS materialized?

OLE2 and BIFF8 contain cross-referenced sector chains and workbook structures.
The reader bounds materialization with `MaxWorkbookBytes` instead of claiming
streaming it cannot provide.

## Why use Excelize only for XLSX?

Excelize provides maintained OOXML behavior. XLS uses a small internal parser
to avoid adding an unmaintained module and its transitive supply-chain surface.

## Does the package export files?

Not in the first release. Export helpers remain a separately evaluated roadmap
item; importing and exporting have different correctness contracts.

## Are numbers typed?

No. Rows contain source-oriented strings. Schema conversion belongs to the
application so locale and precision choices remain explicit.
