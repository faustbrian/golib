# Formal exception and overlay precedence

Evaluation for civil date `D` is deterministic:

1. Evaluate the weekly rule owned by `D`.
2. Evaluate the previous date's owned rule and exceptions; retain only spill.
3. Union current weekly availability with incoming spill.
4. Sort `D` exceptions by ascending priority, then canonical source, revision,
   and operation if canonical conflict resolution was explicitly selected.
5. Apply each operation in order.
6. Clip to `D` and its inclusive effective range.

| Operation | Prior intervals | Rule intervals | Result |
| --- | --- | --- | --- |
| replace | discarded | required | rule |
| add | retained | required | union |
| subtract | retained | required | prior minus rule |
| close | discarded | forbidden | empty |

Duplicate source/revision pairs on the same date return
`CodeDuplicateRevision` regardless of their priorities or insertion order.
Equal priority returns `CodeAmbiguousException` under the default
`RejectAmbiguous` policy.

Overlay is distinct from union. An explicit right-hand day or exception replaces
the complete date. An inherited right-hand day preserves the left schedule;
incoming right-hand spill overrides only its spill interval.
