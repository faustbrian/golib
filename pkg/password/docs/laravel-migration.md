# Laravel password migration

The migration is login-time, verification-first, and identity-neutral. Keep
Laravel as the source of user identity and storage ownership.

## PHP and Go correspondence

| PHP/Laravel | Encoded field | Go |
| --- | --- | --- |
| `PASSWORD_ARGON2ID` | `$argon2id$` | `password.Argon2id` |
| `memory_cost` | `m` in KiB | `Argon2idParameters.MemoryKiB` |
| `time_cost` | `t` | `Argon2idParameters.Time` |
| `threads` | `p` | `Argon2idParameters.Parallelism` |
| PHP bcrypt cost | `$2y$cc$` | parsed bcrypt cost |

PHP and Go use unpadded base64 in PHC strings. Bcrypt `$2y$` verifies through
the maintained Go bcrypt implementation. The synthetic corpus was generated
locally with PHP 8.5.8:

```php
password_hash('synthetic password', PASSWORD_BCRYPT, ['cost' => 10]);
password_hash('synthetic password', PASSWORD_ARGON2ID, [
    'memory_cost' => 65536,
    'time_cost' => 2,
    'threads' => 1,
]);
```

Fixtures live in `passwordtest`; they contain no production credentials.

## Login-time flow

1. Read the user and current encoded hash without modifying either.
2. Call `VerifyAndUpgrade` using the candidate password.
3. Reject mismatch; classify malformed/resource failures as stored-data or
   operational failures rather than bad credentials when the user exists.
4. If no rehash is needed, authenticate normally.
5. If a replacement is returned, conditionally update only while the old hash
   is still present.
6. Authenticate after a successful match even if the optional CAS loses to a
   concurrent login. Re-read later if the application needs confirmation.

`passwordauth.Authenticator` packages the subject and expected/replacement CAS
pair while leaving lookup and persistence with the application.

## Failure and crash safety

- Before verification: no write exists.
- After mismatch: no replacement is created.
- During replacement hashing: the old hash remains stored and usable.
- Before durable CAS: the old hash remains stored and usable.
- CAS loses: another writer changed the value; never overwrite it blindly.
- CAS commits: the new Argon2id hash becomes current atomically.
- Process crashes after commit: the durable new hash remains valid.

Never clear or overwrite the existing hash before successful verification and
durable conditional update. Do not change the user identifier, session policy,
or authentication principal because the hash algorithm changed.
