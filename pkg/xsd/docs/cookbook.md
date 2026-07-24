# Cookbook

## Compile embedded schemas

Create `resolve.Memory` with absolute resource URIs, pass it to
`compile.New`, and compile the root with the same absolute URI used as the map
key. Relative includes then resolve without file or network access.

## Validate untrusted XML

Set application-sized `validate.Limits`, provide a context deadline, call
`Validate`, and treat a non-nil error as an incomplete assessment. When the
error is nil, inspect `Result.Valid` and `Result.Diagnostics`.

## Generate a small schema

Use `builder.New`, add named types and declarations, call `Build`, and serialize
the returned document with `xsd.Marshal`. `Build` rejects unresolved component
references before the schema is published.

## Diagnose unsupported behavior

Locate the feature in `specification/requirements/xsd-1.0.tsv`. A `partial` or
`missing` row is not a validator bug report by itself; add a focused schema and
instance fixture plus the relevant normative section when implementing it.
