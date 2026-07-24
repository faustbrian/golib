# Basic authentication

Use `basic.NewStatic` for a small configured set of service credentials. The
authenticator hashes usernames and passwords to fixed-size digests and compares
every configured entry in constant work relative to entry count.

Pair it with `authhttp.BasicAuthorization`. Use TLS without exception, keep
passwords in a secret manager, and rotate by deploying overlapping entries
before removing the old credential. Basic is usually appropriate for controlled
service integrations, not end-user password verification; this package does
not hash stored user passwords or manage accounts.

See the compiled `basic.ExampleNewStatic` example.
