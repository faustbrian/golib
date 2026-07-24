# TLS

PostgreSQL TLS has two separate goals: encrypting traffic and authenticating
the server. Production deployments normally need both.

`TLSFromDSN` preserves pgx DSN behavior. Prefer `sslmode=verify-full` with an
explicit root certificate and a hostname matching the certificate. Be cautious
with pgx or libpq modes that permit plaintext fallback.

`TLSRequire` applies a copied `*tls.Config` to the primary and every fallback
host, eliminating plaintext fallback. Certificate pools, protocol slices, and
certificate bytes are copied. Callback functions, private keys, session caches,
random sources, clocks, and writers remain application-owned collaborators and
must be safe for concurrent use. The name means TLS transport is required; the
supplied configuration still controls authentication. Keep
`InsecureSkipVerify=false`, populate `RootCAs`, use TLS 1.2 or newer, and set a
valid `ServerName` when hostname inference is not appropriate.

`TLSDisable` removes TLS from the primary and every fallback and is intended
for local Testcontainers or a separately secured Unix/socket or sidecar path.
Document the compensating control before using it outside tests.

Integration evidence starts a strict `TLSRequire` client against the standard
non-TLS Testcontainers server and proves startup fails rather than using the
DSN's plaintext fallback. A separate wrong-password startup test proves native
authentication SQLSTATE remains inspectable without the password appearing in
the public error string.

Certificate files are loaded by the application so secret delivery, rotation,
permissions, and reload policy remain explicit. Never log a DSN, private key,
client certificate secret, or full `tls.Config`.
