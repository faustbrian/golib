# Security and resource limits

Barcode payloads and decoded URLs are untrusted data. This library never
executes, fetches, redirects to, or automatically follows decoded content.

`imagedecode.Limits` bounds encoded bytes, width, height, total pixels,
candidate attempts, payload bytes, rotations, memory, corrections, and time
before or between expensive work. Cancellation and the derived time deadline
are checked before conversion and between decoder attempts. Set stricter
application-specific limits for public uploads.

`MaxCorrections` limits the exact corrected-error count reported by QR, Data
Matrix, Aztec, and PDF417 readers. Linear formats report zero because their
readers validate checksums rather than applying error correction.

Use `DecodeEncoded` for untrusted PNG, JPEG, or GIF streams. It limits input
bytes, inspects encoded dimensions, applies pixel and memory budgets before
full decompression, rejects truncation, and checks cancellation between stages.
Callers using `Decode` directly remain responsible for how their `image.Image`
was allocated.

Additive two-dimensional readers are isolated at the adapter boundary. A panic
from malformed symbol data in a decoder dependency is converted to a failed
candidate and cannot escape `Decode`; another bounded candidate may still be
attempted.

Matrix and renderer constructors reject invalid dimensions, multiplication
overflow, non-positive scales, and allocation budgets. Errors avoid including
full payloads by default. Security reports should follow `SECURITY.md` once the
publication contact is configured.
