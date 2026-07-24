# Authoritative authentication vectors

These vectors are immutable interoperability fixtures, not live credentials.

| Vector | Input | Expected result |
| --- | --- | --- |
| RFC 7617 | `QWxhZGRpbjpvcGVuIHNlc2FtZQ==` | user `Aladdin`, password `open sesame` |
| RFC 7617 UTF-8 | `dGVzdDoxMjPCow==` | user `test`, password `123` followed by U+00A3 |
| RFC 6750 | `Authorization: Bearer mF_9.B5f-4.1JqM` | exact bearer token extracted |
| RFC 7520 Figure 5 | HMAC JWK with `kid` `018c0ae5-4d9b-471b-bfd6-eef314bc7037` | configured HS256 JWT authenticates |
| OIDC Core Section 2 | `iss`, `sub`, `aud`, `exp`, `iat`, `auth_time`, `nonce`, `acr`, `amr`, `azp` | signed ID token authenticates under matching policy |

Executable copies live in each module's `interoperability_test.go`. Negative
companions mutate algorithms, key types, identifiers, audiences, issuers,
dates, authorized parties, nonce values, controls, duplicates, and resource
limits and must fail closed.

Static API-key lifecycle vectors are covered in `apikey/static_test.go`: a
current and previous key overlap, atomic replacement admits the new set, the
removed key is rejected, and unknown or duplicate material never authenticates.
