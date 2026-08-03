# Versioned keyring operations

The keyring adapter accepts one to 32 named AES-256 wrapping keys supplied by
the application. It copies the input map and keys, generates a fresh 32-byte
data key and 12-byte wrapping nonce for every encryption, and authenticates the
exact key reference plus canonical envelope context.

The adapter does not fetch secrets. Applications must decode key material from
their approved secret-delivery boundary before construction. Values must be
cryptographically random 32-byte keys; passwords, passphrases, hashes, and
truncated strings are not keys.

## Rotation

1. Add a new random key under a new stable reference.
2. Deploy the expanded keyring while keeping the previous active reference.
3. Change the active reference for new writes and deploy again.
4. Rewrap or expire historical envelopes according to application policy.
5. Remove an old key only after proving no readable envelope references it.

The adapter does not silently fall back to another key. Missing references,
changed context, changed wrapping keys, malformed ciphertext, and tampering
fail closed.

## Custody

Keyring material remains in process memory for the provider lifetime and must
be delivered through an encrypted, access-controlled secret manager. Do not
commit it, place it in container layers, expose it through configuration
inventories, or persist it beside the ciphertext it protects.
