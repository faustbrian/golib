# Architecture

The root package owns typed values, validation, factories, deterministic
strategies, carrier codecs, propagation, context, external metadata, and
disclosure. Transport packages adapt those contracts but do not add ambient
state. The dependency direction is adapters toward the root; the root depends
only on the default identifier generator.

Generation, codec, and trust policy are immutable after construction. Mutable
entropy and sequence state belongs to explicitly owned generator instances.
Maps and headers remain caller-owned carriers, and context derivation is the
only request-scoped storage.
