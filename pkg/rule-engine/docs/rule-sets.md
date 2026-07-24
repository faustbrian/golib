# Rule sets

Rules are ordered by priority descending, then ID ascending. Input slice order
has no effect. Tags and namespaces are inspectable metadata and do not alter
evaluation.

`FirstMatch` selects the first ordered match. `CollectAll` records every unique
match across chaining iterations. `ErrorOnMultiple` becomes indeterminate when
more than one unique rule matches.

Derived paths have one producer per rule set. This removes write races and
ambiguous conflict resolution. If multiple business sources may propose a
value, model those proposals as separate paths and add an explicit resolving
rule.
