# FAQ

## Does parsing load imports or includes?

No. Parsing records references. Only compilation may resolve them, through the
resolver supplied by the caller.

## Can I enable arbitrary HTTP schema loading?

The package has no unrestricted HTTP resolver. Implement a resolver with an
explicit allowlist and strict byte, redirect, and credential policies.

## Is XML Schema 1.1 supported?

No stable 1.1 support is claimed. Partial syntax without assertions,
alternatives, open content, and datatype changes would be misleading.

## Is the package fully XML Schema 1.0 conformant?

The stable XML Schema 1.0 scope is fully implemented in the live requirement
matrix. The pinned XSTS gate passes every accepted expectation, and the
production-coverage gate is 100%. The 90 upstream XSTS expectations marked
`queried` are reported separately because the suite does not assign them a
pass/fail result.

## Are compiled sets safe to share?

Yes. Sets and validators are immutable and their public accessors return deep
copies where the model contains mutable storage.
