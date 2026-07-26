# Vector and fixture provenance

All passwords in this corpus are synthetic literals. No fixture was copied from
an application database, incident, user account, or production log.

## Argon2id reference encoding

`TestMaintainedImplementationVectors/argon2id_PHC_reference_CLI` uses the PHC
string produced by the official Argon2 reference implementation at
<https://github.com/P-H-C/phc-winner-argon2>:

```sh
printf password | argon2 somesalt -id -v 13 -t 1 -m 6 -p 1 -l 24 -e
```

The fixture fixes Argon2 version 19 (`-v 13` is hexadecimal notation), 64 KiB
memory, one iteration, one lane, an eight-byte salt, and a 24-byte output. The
Go test verifies the reference output through the package's strict PHC parser
and maintained `x/crypto/argon2` implementation.

## Maintained bcrypt vector

The `$2a$` fixture and password `allmine` are the generation vector in the
pinned `golang.org/x/crypto/bcrypt` v0.54.0 upstream test suite. It proves the
adapter preserves the maintained implementation's standard encoding. Separate
PHP fixtures below provide independent interoperability evidence.

## Independent PHP fixtures

The committed `$2y$` bcrypt and Argon2id PHC fixtures were generated with PHP
8.5.8 using only `synthetic password`:

```php
password_hash('synthetic password', PASSWORD_BCRYPT, ['cost' => 10]);
password_hash('synthetic password', PASSWORD_ARGON2ID, [
    'memory_cost' => 65536,
    'time_cost' => 2,
    'threads' => 1,
]);
```

`make interoperability` independently verifies both committed PHP outputs in
Go and again with PHP's `password_verify`. It also creates fresh random PHP
Argon2id and bcrypt outputs and verifies them in Go, then creates fresh Go
outputs and verifies them in PHP. This live bidirectional gate tests the shared
encodings without trusting a single implementation or only stable fixtures.

## Maintenance rule

Any fixture change must retain the producer, version, exact synthetic input,
parameters, generation command, and bidirectional verification. Randomly
generated replacements must not erase the stable committed corpus.
