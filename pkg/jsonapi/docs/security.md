# Security

## Trust Boundary

Treat every JSON:API document, query string, media-type parameter, extension
member, profile callback, and pagination cursor as untrusted input. Apply
bounded decoding before allocating application-owned structures.

## Safe Adoption

- Set limits appropriate to the endpoint before decoding.
- Validate documents in the correct request or response context.
- Register only trusted extension and profile callbacks.
- Do not expose callback error text directly to clients.
- Enforce HTTP request-body and header limits outside this package as well.

The package does not provide authentication, authorization, rate limiting,
transport timeouts, or persistence controls. Those remain application
responsibilities.

## Reporting

Follow the repository [security policy](../SECURITY.md) for private
vulnerability reporting. See the [threat model](threat-model.md) and
[hardening evidence](hardening.md) for the maintained security assumptions.
