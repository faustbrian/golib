# Dataset update and reproducibility report

This v1 baseline is the semantic projection in
`data/dataset-snapshot.json` (schema 1, SHA-256
`a1e2d3de3b36bb5642b13f3340a81324240090cf700dc30bf7d43581bf938983`).
It contains no names or values outside compatibility-relevant generated
metadata fingerprints.

| Dataset | Total | Current | Historic/deleted | Other statuses |
|---|---:|---:|---:|---:|
| Country | 301 | 249 official | 12 deleted | 13 reserved, 26 user-assigned, 1 unknown |
| Subdivision | 5,653 | 5,027 official | 626 deleted | 0 |
| Currency | 307 | 178 official | 129 historic | 0 |

Regenerating the snapshot from the committed tables produces an empty semantic
diff for all five classifications: additions, removals, alias changes, status
changes, and metadata changes. `make generate-check` independently regenerates
the Go tables from checksum-pinned upstream inputs and compares both the tables
and semantic snapshot byte for byte.

For an update, preserve the old snapshot, regenerate the tables and snapshot,
then run:

```sh
make dataset-diff BEFORE=/tmp/international-before.json \
  AFTER=data/dataset-snapshot.json
make release-check
```

Attach the JSON diff to review and update this report and `CHANGELOG.md` with
every non-empty category. An empty alias category means the package retains
historic identifiers as distinct values; it does not silently redirect them.
