# Syntax, flags, Unicode, and captures

`Compile` accepts ECMAScript 2025 pattern text and a separate flag string.
Only `d`, `g`, `i`, `m`, `s`, `u`, `v`, and `y` are accepted. Duplicate flags,
unknown flags, and the conflicting `u` plus `v` combination are errors.

- `d` requests indices in ECMAScript; this package always makes complete
  index spans available on results.
- `g` controls replacement repetition and stateful `Session` execution.
- `i`, `m`, and `s` select ignore-case, multiline, and dot-all behavior.
- `u` enables Unicode code-point matching.
- `v` enables Unicode Sets syntax and Unicode code-point matching.
- `y` makes search sticky at the explicit start or `lastIndex`.

The grammar includes character and property escapes, classes, assertions,
captures, backreferences, lookaround, greedy and lazy quantifiers, scoped
`i`/`m`/`s` modifier groups, and Unicode Sets class algebra and string
properties. Unsupported or future syntax produces a typed `SyntaxError`.

Capture zero is the complete match. `Capture.Participated` distinguishes an
unmatched capture from a participating capture whose value is empty. Duplicate
named captures are accepted only where ECMAScript permits alternatives that
cannot both participate; named lookup returns the participating capture.

`ParseOptions.AnnexB` controls web-compatibility grammar outside `u` and `v`
mode. It is enabled by `DefaultParseOptions`. Disable it when legacy octal,
identity escapes, and other Annex B productions must be rejected.

Unicode property, case-folding, identifier, and properties-of-strings tables
are generated from the sources pinned in `specification/manifest.json`.
