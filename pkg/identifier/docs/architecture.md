# Architecture

Concrete identifiers are immutable values. Fixed-width families return arrays
by value, never mutable slices. Variable text values keep state private.
Serialization validates into a temporary value before replacing a receiver.

Generators own their clock, entropy reader, mutex, and monotonic state. There
is no package-global generator and no runtime registry. A generator is the
boundary of ordering: sharing one instance shares order; separate processes or
instances do not coordinate.

The root generic typed ID uses a compile-time tag whose zero value validates
text. This avoids reflection and registration while allowing JSON and SQL
decoding to recover validation behavior from the type argument.

External maintained packages are codec/differential references, not ambient
generation state. Standard-library `crypto/rand` is the production default.
