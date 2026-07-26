# FAQ

## Does parsing fetch references?

No. Core parsing and validation perform no network or filesystem access.

## Why are schemas stored as JSON instead of Go structs?

Draft 7 allows boolean schemas, arbitrary annotations, large exact numbers,
recursive references, and keywords that reflection models commonly narrow.

## Are documents safe for concurrent reads?

Constructors own inputs and getters return owned snapshots. Mutable caches and
registries have explicit synchronized lifetimes.

## Does a valid document prove the server implementation is correct?

No. It proves document conformance, not runtime handler behavior.

## Why are future OpenRPC minor versions rejected?

Their semantics are not guessed. A new feature line must first be inventoried,
pinned, implemented, and tested.

## Why can server URLs be relative or templated?

The normative Server Object text permits both. The pinned schema's conflicting
absolute-URI format assertion is narrowly removed during meta-schema compile;
semantic expression checks remain active.
