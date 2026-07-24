# Semantic compatibility

Package `diff` compares compiled sets rather than source formatting. Results
are sorted and classify additions, removals, and modifications as breaking,
non-breaking, or unknown. Reports always include caveats.

Classification is conservative. Removing interfaces, operations, bindings,
services, endpoints, message fields, or schema symbols is breaking. Additions
are generally non-breaking at the structural level. Extension policy,
application semantics, substitution groups, wildcards, and transport behavior
can change compatibility without a visible core component change, so a diff is
review input rather than a wire-compatibility proof.
