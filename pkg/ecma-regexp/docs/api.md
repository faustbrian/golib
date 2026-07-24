# API and indices

The processing pipeline is explicit:

1. `Tokenize` returns immutable tokens with pattern byte spans.
2. `Parse` returns an immutable typed `Pattern` AST.
3. `Compile` returns an immutable executable `Program`.
4. `Match`, `Find`, `FindAll`, `Replace`, and `Split` execute with caller
   options and context cancellation.

`Match` attempts only `MatchOptions.StartUTF16`. `Find` searches from that
position unless the `y` flag makes the program sticky. `FindAll` returns
ordered, non-overlapping matches and advances empty matches using ECMAScript
`AdvanceStringIndex` semantics.

Every capture has a half-open `IndexSpan`. `Index.UTF16` is the normative
ECMAScript code-unit position. `Index.Rune` and `Index.Byte` map to the Go
input. `Index.Exact` is false at a boundary inside a surrogate pair.

The string-taking APIs decode invalid UTF-8 one byte at a time as U+FFFD and
preserve byte offsets. Use `UTF16String` APIs when exact ECMAScript strings,
including lone surrogates, are required. `GoString` rejects an unpaired
surrogate; `LossyString` performs explicit replacement.

`Program` values are immutable and may be shared concurrently. `Session`
provides caller-owned `lastIndex` state for `g` and `y`; it is not safe for
concurrent mutation. On an ordinary failed global or sticky execution,
`lastIndex` resets to zero. A limit or cancellation error leaves it unchanged.
