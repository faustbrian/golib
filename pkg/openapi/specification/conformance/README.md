# OpenAPI conformance evidence

`normative.tsv` is the generated occurrence inventory for all BCP 14 keywords
in every supported patch specification. A sentence with multiple keywords has
multiple rows so one implemented clause cannot conceal another unreviewed
clause. Stable IDs are scoped to the exact patch document.

`evidence.tsv` is the reviewed conformance ledger. Every occurrence begins as
`unimplemented` and can become `implemented` only when its row links production
code, executable tests, and documentation. Partial behavior remains `partial`;
schema-only or example-only evidence is not sufficient for prose conformance.

The generated occurrence inventory is a completeness guard, not semantic
interpretation. Human review may link multiple occurrence rows to one semantic
rule, but rows are never removed merely because patch specifications repeat or
clarify a requirement.

`object-fields.tsv` is generated from normative object tables rather than the
informative schemas. It preserves version, object, variant table, fixed or
patterned field name, type text, required marker, and source line. The
versioned model generator refuses unmapped fixed-field types and emits a typed
accessor for every unique fixed field in each latest supported patch.

Regenerate `normative.tsv` with:

```sh
go generate .
```

The generator creates `evidence.tsv` only when it does not exist, preserving
reviewed claims on later runs.

All 2,381 current occurrence rows are reviewed as `implemented` and link their
implementation, executable test, and documentation evidence. This status means
the ledger has no known prose gap; it does not replace adversarial, mutation,
interoperability, or resource-safety evidence and is not by itself a full
compliance claim.
Evidence tests require exact inventory ordering, known statuses, complete
reviewed columns, and existing implementation, test, and documentation paths.
