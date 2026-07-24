# Migration

## From Ad Hoc JSON-RPC Handlers

1. Inventory method names, parameter shapes, notification behavior, and error
   codes.
2. Move protocol decoding and validation into this package before changing
   business behavior.
3. Register one method at a time and compare success and error responses with
   captured fixtures.
4. Add batch, notification, malformed-input, and cancellation regressions.
5. Switch clients only after ID correlation and transport timeout behavior
   match production expectations.

## Between Package Versions

Read [CHANGELOG.md](../CHANGELOG.md) and [compatibility.md](compatibility.md)
before upgrading. Pre-v1 releases may contain documented breaking changes.
Never infer wire compatibility from compilation alone; replay representative
requests and errors.
