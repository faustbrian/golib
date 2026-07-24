# Fuzz corpus

`scripts/check-fuzz.sh` runs six bounded native Go fuzz targets. Every target
starts with deterministic semantic seeds; minimized defects that add new
behavioral coverage are retained under `testdata/fuzz`.

| Package and target | Seed and hostile-input coverage | Per-input bound |
| --- | --- | --- |
| `FuzzPathAndReportSafety` | field and key syntax, RFC 6901 escaping, structural path collisions, secret-marker formatting | report path and diagnostic limits |
| `rules/FuzzUnicodeAndMalformedPrimitives` | Unicode, invalid UTF-8, URLs, email, networks, and identifiers | standard string limit |
| `structplan/FuzzTagGrammarNeverPanics` | valid, unknown, duplicate, empty, numeric, and malformed tag tokens | 4,096 input bytes; compiler tag limit |
| `structplan/FuzzCompiledPlansAcrossSupportedShapes` | embedded fields, aliases, pointers, interfaces, maps, slices, arrays, and instantiated generics | 256-byte strings and 32 items |
| `structplan/FuzzTypedPlanConstructionNeverPanics` | empty and hostile names, nil accessors, nil validators, and panicking accessors | 4,096 name bytes; 64-byte path limit |
| `validationjsonapi/FuzzProjectionPaths` | arbitrary field/code text, pointer escaping, and JSON encoding | report diagnostic and path limits |

The committed `testdata/fuzz/FuzzPathAndReportSafety` corpus contains three
minimized regression inputs for structural path identity, oversized path
handling, and the secret-leak oracle. Native seed calls remain source-visible
beside each target so clean checkouts execute the same starting corpus.

Run the release smoke with `make fuzz`. Increase dwell time without changing
the target matrix with:

```sh
FUZZ_TIME=30s make fuzz
```

Fuzzing supplements, but does not replace, deterministic boundary, race,
mutation, and resource tests.
