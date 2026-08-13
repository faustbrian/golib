# Compatibility

The module requires Go 1.26.6 or newer and currently exposes a pre-v1 API.
`Factory.Open` is unavailable on `js` and `plan9`, where `os.Root` cannot
provide the rename-stable containment required by the storage contract.

Lexicographic ordering, duplicate preservation, exact bounds, lifecycle error
semantics, owner-only permissions, and authenticated corruption rejection are
compatibility commitments. The encrypted chunk bytes are deliberately private
and may change because chunks cannot be reopened after process ownership ends.

Changing record size, key derivation, or callback retention behavior requires
an application migration. Existing stores must be closed before upgrading;
there is no persistent on-disk migration.
