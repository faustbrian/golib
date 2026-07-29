# execution-spec-tests fixture manifest

- Source: `ethereum/execution-spec-tests`
- Release: `v5.4.0`
- Release commit: `88e9fb8f10ed89805aa3110d0a2cd5dcadc19689`
- Asset: `fixtures_stable.tar.gz`
- Asset SHA-256:
  `92cf1b47ad12fb27163261fc3c1cea5df72439cab507983d06b56c94f8741909`
- License: CC0-1.0 (`LICENSE`, copied byte-for-byte from the release commit)
- Update:
  `./scripts/update-execution-spec-fixtures.sh`
- Applicability: stable Frontier, Berlin, London, Cancun, and Prague
  execution profiles represented by the selected blockchain fixtures.

Imported fixture files remain byte-identical to the release asset:

| File | SHA-256 | Local coverage |
| --- | --- | --- |
| `blockchain_tests/frontier/examples/test_block_intermediate_state.json` | `411035cccfee534648135073873e94aba2e58c2d2eb09aa41a377305b3da1c2f` | Legacy transaction roots and pre/post allocation state roots |
| `blockchain_tests/berlin/eip2930_access_list/test_eip2930_tx_validity.json` | `860c386fcb930553b7a6487e805bcfbd65d430ca5548e3d8bcddf05c62220670` | Type-1 transaction roots and pre/post allocation state roots |
| `blockchain_tests/london/eip1559_fee_market_change/test_eip1559_tx_validity.json` | `d12a301b9cd3aa4a1978a336e24874dbc70b4ae4d3ff1fc3e8f4944457571b91` | Type-2 transaction roots and pre/post allocation state roots |
| `blockchain_tests/cancun/eip4844_blobs/test_blobhash_multiple_txs_in_block.json` | `0910707d823e54042a6f8be1dd87822856390172d1f2ff7924caf6dabfc9cf9e` | Mixed type-2/type-3 transaction roots and pre/post allocation state roots |
| `blockchain_tests/prague/eip7702_set_code_tx/test_eip_7702.json` | `7e1b668d91043606afedba88775e825f0fded12f0c4ba98d8c452a023221750e` | Type-4 transaction roots and pre/post allocation state roots |

The block fixtures also contain receipt-root commitments. They do not contain
the receipt values needed to reconstruct those tries, so this corpus does not
claim receipt-root coverage. Receipt-root evidence belongs in a separate
byte-level corpus with exact receipt bytes and provenance.
