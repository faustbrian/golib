# Validation output

`ValidateOutput` and `ValidateValueOutput` accept one of `OutputFlag`,
`OutputBasic`, `OutputDetailed`, or `OutputVerbose` and return detached
`OutputUnit` values.

Output units expose validity, JSON Pointer keyword and instance locations,
canonical absolute keyword locations when available, human-readable errors,
nested errors, nested annotations, and exact annotation values. JSON Pointer
tokens escape `~` and `/`; absolute locations use the effective schema
resource identifier. Ordering is deterministic: schema arrays retain source
order and object-derived contributions use lexical key order.

Flag JSON contains only `valid`. Basic output uses flat error or annotation
units and retains by-reference applicators in `keywordLocation` while absolute
locations identify dereferenced schema resources. Detailed output condenses
those units into applicator and reference hierarchies. Verbose uses the same
uncondensed hierarchy and reports every evaluated keyword, including
successful and failed applicator branches and dereferenced schema targets.
Annotations in that tree describe the evaluation result at each location;
they are not the same as retained annotations from successful paths.

`CollectAnnotations` returns retained annotations as a flat, deterministic
list when an application needs annotation collection rather than a standard
output tree. Failed schema branches do not contribute to that list. Reference,
dynamic-scope, applicator, content, format, metadata, unknown-keyword, and
unevaluated-location propagation is covered by the official annotation corpus.

Output generation repeats bounded diagnostic evaluation after the flag result
and may therefore invoke custom callbacks again. Callbacks must be pure for a
given input. `MaxOutputUnits`, `MaxAnnotationBytes`, callback budgets, total
evaluation operations, and cancellation remain active. Limit exhaustion is an
error, never an invalid-instance result.
