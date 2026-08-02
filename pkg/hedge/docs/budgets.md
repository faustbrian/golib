# Budgets and amplification

`MaxHedges` bounds one logical operation. A shared `Budget` bounds additional
work across concurrent logical operations. Every budget declares a finite
positive `Capacity` bounded by `MaxBudgetCapacity`. `OutstandingBudget(B)`
admits at most `B` simultaneous or completed-but-unconsumed hedges across every
policy sharing the instance and releases a permit when the coordinator consumes
or reclaims the result.

Budget denial is an observable admission decision, not a downstream error. The
existing attempts continue; their deterministic selected result is returned.
For independent resources, create one finite shared budget instance per known
resource. The built-in budget intentionally does not create an unbounded map
from attacker-controlled resource strings.

Retry and hedge layers must draw from one hard amplification budget. Separate
limits of `R` retries and `H` hedges can otherwise create up to
`(R + 1) * (H + 1)` executions.
