# Cookbook

## Every OpenRPC object

[`complete-openrpc.json`](../parse/testdata/complete-openrpc.json) contains every
field inventoried from the pinned 1.4.1 meta-schema: root, Info, Contact,
License, External Documentation, Server, Server Variable, Method, Tag, Content
Descriptor, Error, Link, Example, Example Pairing, Reference, and all Components
maps. Its tests remove and null each field independently.

## Empty security-filtered discovery

Return a copied document whose `Methods` slice is allocated and empty. Validate
it normally and let discovery produce the canonical snapshot. Do not return a
nil required methods slice from a constructor.

## Offline external schemas

Load exact schema bytes into `reference.NewMemoryStore`, enable external
resolution, and allow exact URI schemes and hosts. Pass the resolver to
`validate.ResolvedDocument`. No package-global loader is installed.

## Portable bundles

Call `reference.Bundle` to retain exact root and resource bytes keyed by
absolute document URI. Persist those entries separately. `ResourceBundle.Store`
recreates an offline store without rewriting `$id` or inventing extensions.

## Runtime link parameters

Keep constants as arbitrary JSON and expressions as strings. Call
`expression.EvaluateLinkParams` with a typed immutable context. Missing values,
non-scalar interpolation, output size, and node limits are explicit errors.

## Compatibility gate

Use `diff.CompareResolved` for documents whose source references differ but
whose target semantics may be equal. Preserve report pointers in review output;
do not reduce conditional findings to compatible automatically.
