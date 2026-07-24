# API map

The root package exposes common errors, `Clock`, `Generator[T]`,
`Inspection`, and `ID[Tag]`. A tag is a zero-value `Validator`; the generic ID
stores canonical text and prevents assignment between different domain tags
without reflection or runtime registration.

`uuid` parses RFC UUID versions 1 through 8, generates v4 and monotonic v7, and
supports pgx UUID values. `ulid` parses and generates monotonic ULIDs. `typeid`
validates TypeID prefixes, converts canonical UUID text or validated `uuid.ID`
values, and generates UUIDv7 suffixes. `ksuid` implements the Segment codec and
a monotonic payload policy. `nanoid` validates explicit alphabets and entropy.
`idtest` contains deterministic sources and assertions.

Run `go doc -all` on a package for signatures and method contracts. The
`api-compat` gate fingerprints every exported package and requires deliberate
review when the surface changes.
