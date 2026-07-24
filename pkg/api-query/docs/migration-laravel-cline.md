# Laravel and Cline RPC migration

Inventory each existing resource/query-builder convention before replacing it:
selected/default/hidden fields, eager-load paths, validation rules, operators,
authorization checks, scopes, tenant predicates, default ordering, pagination,
limits, and response metadata. Treat observed behavior as a compatibility
decision rather than reflecting models automatically.

Map the old endpoint in this order:

1. Declare public capabilities in one versioned `SchemaConfig`.
2. Mark join keys and hydration dependencies `Required`; keep them out of the
   response fieldset unless selected and authorized.
3. Translate validation rules to typed filter definitions and structural
   bounds. Do not carry raw scope names, SQL, regex, or callback functions.
4. Move policies to `AuthorizeFunc` and tenant/global scopes to mandatory
   constraints or non-removable application SQL.
5. Declare the existing stable order. Add a unique tie-breaker before adopting
   cursors; do not pretend offset pages are cursor-stable.
6. Parse RPC parameters with `apiqueryrpc`, compile, and adapt only the plan.
7. Run old and new implementations against a characterization corpus and
   compare projected records, ordering, boundaries, and structured failures.
8. Version intentional differences and migrate clients before deleting the old
   path.

Laravel `whenLoaded`, hidden attributes, global scopes, and Eloquent builder
macros are not API contracts by themselves. Make each necessary behavior
explicit. The Go adapter must not become a repository or reproduce lazy loading.

For SQLC, prefer a bounded set of named query variants for approved filter/sort
shapes. Use generated code for execution and `apiquerypgx` only for reviewed
fragments where variants are impractical.
