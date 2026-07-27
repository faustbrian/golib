# Compatibility corpus

These fixtures pin first-party JSON payload bytes and one representative
upcast path. They are independently authored for this module, covered by the
repository MIT license, and contain no production or upstream EventSauce data.

`json/customer-registered-v2.json` proves the documented integer, timestamp,
and deterministic map-key encoding. The `upcast` fixtures preserve one stored
v1 payload and the two ordered logical payloads produced by its reviewed
rename, split, and schema-advance chain.

Update a fixture only with the corresponding schema or compatibility decision,
then refresh `checksums.txt` with:

```sh
shasum -a 256 \
  testdata/json/customer-registered-v2.json \
  testdata/upcast/legacy-user-created-v1.json \
  testdata/upcast/user-registered-v1.json \
  testdata/upcast/user-email-changed-v2.json
```

Focused tests verify both the behavior and every recorded checksum.
