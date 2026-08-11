# FAQ

## Why are quantities encoded as strings?

The core rule engine has a closed value-kind model. A versioned tag preserves
that boundary while making measurement support an optional adapter.

## Does equality convert units?

Yes. All five relations use the same exact measurement comparison.

## Why can a compatible conversion fail?

Some ratios have no terminating base-10 representation, or arithmetic may
exceed configured measurement limits. Rounding would change the comparison, so
the adapter returns `ErrInvalidQuantity` and the underlying cause.

## Can I use `less_than` on the tagged strings?

No. Built-in string ordering compares bytes. Register this adapter and use
`quantity_less_than`.

## Does the adapter recognize `meters`, `lbs`, or localized names?

No. Only canonical measurement unit symbols are accepted. Resolve aliases
before constructing the quantity.

## Does it cache or refresh anything?

No. Operators are pure, process-local values with no I/O, goroutines, cache,
or global registration.
