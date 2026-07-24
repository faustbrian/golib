# Policy examples

## Unicode password

Use `password.Policy` when length means Unicode code points. Every alphabet and
class rejects duplicate code points and normalization-colliding code points.
Excluded characters are removed before feasibility and entropy are calculated.
Required classes may overlap.

## Byte password

Use `password.BytePolicy` for arbitrary bytes, including zero and non-UTF-8
values. `GenerateBytesInto` knows the exact output length and leaves a short
destination unchanged. Do not pass binary values to text-only systems without
an explicit encoding layer outside this module.

## Passphrase

Choose separators that cannot occur in transformed list words. EFF lists
contain some punctuation, so a space is the safest parsing separator. Casing is
part of the policy and must not collapse entries. Prefixes and suffixes use
independent `password.Policy` distributions and explicit separators that cannot
occur in their alphabets.

Always call `Analyze` during configuration or startup. Set
`MinimumEntropyBits` from the application's threat model, not from a generic
password-strength label.
