# FAQ

## Is this an authorization engine?

No. It has no permit, deny, role, permission, principal, or combining defaults.

## Is missing equal to null?

No. Missing is returned only for absent paths. Null is an explicit supplied
value.

## Are numeric strings coerced?

No. Normalize them before constructing facts or reject the input.

## Can a rule call my model or database?

No. Supply facts directly or use the explicit resolver boundary before
evaluation. Arbitrary method calls and reflection discovery are unsupported.

## Can I register operators globally?

No. Operators belong to one compiler, making ownership and concurrency clear.

## Why can custom predicates not be serialized?

Function behavior has no canonical portable representation or inspectable
grammar. Use built-in AST nodes or a typed registered operator.
