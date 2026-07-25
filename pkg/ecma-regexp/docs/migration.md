# Migration from Go regexp and PCRE

Do not assume that a pattern accepted by Go `regexp`, PCRE, or ECMAScript has
the same language or result in another engine.

From Go `regexp`:

- replace `regexp.Compile` with `Compile` and provide an explicit flag string;
- choose `Match` for exact-start matching or `Find` for search;
- supply finite compile and match options plus a context;
- consume UTF-16 indices when reproducing JavaScript-visible behavior;
- audit backreferences, lookaround, named captures, Unicode properties, and
  replacement tokens instead of rewriting them to RE2 approximations.

From PCRE:

- remove PCRE-only options, verbs, conditionals, recursion, and syntax;
- verify escape and character-class behavior in the selected ECMAScript mode;
- audit anchors, newline handling, duplicate names, and case folding;
- use the differential and conformance tests as executable migration vectors.

For JSON Schema, migrate directly to `CompileJSONSchemaPattern`; do not add
implicit anchors or pass user-selected flags.
