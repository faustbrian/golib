# Compatibility matrix

| Producer/encoding | Verify | Hash target | Upgrade behavior |
| --- | --- | --- | --- |
| Go Argon2id PHC v19 | Yes | Yes | Monotonic parameter upgrade |
| PHP/Laravel Argon2id v19 | Yes | Shared PHC encoding | Monotonic upgrade |
| Go bcrypt `$2a$` | Yes | Yes when bcrypt policy | Upgrade to Argon2id |
| Standard bcrypt `$2b$` | Yes | Parser/verify | Upgrade to Argon2id |
| PHP/Laravel bcrypt `$2y$` | Yes | PHP produces it | Upgrade to Argon2id |
| Argon2i/Argon2d | No | No | Unsupported algorithm |
| Argon2 version other than 19 | No | No | Unsupported version |
| Scrypt/PBKDF2/custom formats | No | No | Adapter not present |

Minimum Go version is 1.26.5. The pinned cryptographic dependency is
`golang.org/x/crypto` v0.54.0. The PHP corpus was generated with PHP 8.5.8 and
contains only the literal synthetic password documented in the migration guide.
Producer commands and source provenance are recorded in
[vector and fixture provenance](vector-provenance.md).

No format extension is inferred. New algorithms require an explicit adapter,
grammar, bounds, vectors, fuzzing, migration policy, and compatibility entry.
