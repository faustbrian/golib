# go-ethereum receipt fixture manifest

- Source: `ethereum/go-ethereum`
- Version: `v1.17.3`
- Revision: `117e067f0f0bae1a17082321f224dedb6765b10f`
- License: LGPL-3.0 (`COPYING`, copied byte-for-byte from the pinned revision)
- Update: `./scripts/update-geth-receipt-fixtures.sh`
- Applicability: Geth transition-tool receipt outputs for legacy, type-2,
  type-3, and type-4 execution profiles.

Imported files remain byte-identical to the pinned revision:

| File | SHA-256 | Local coverage |
| --- | --- | --- |
| `cmd/evm/testdata/1/exp.json` | `50296a4d39478d0e2a08db144a53a0d1c33a54fbdd5604f5f3d406e82f991758` | Legacy status receipt root |
| `cmd/evm/testdata/13/exp2.json` | `b3835420252fd3eb61676728ad83b4c35469d007c14e5e0abb2cfd117c192b58` | Ordered two-receipt type-2 root |
| `cmd/evm/testdata/28/exp.json` | `d4547eeaa055b7e3c90a1cddbfb616fc193be356034a2a239731fc74cc5c678e` | Type-3 receipt root |
| `cmd/evm/testdata/33/exp.json` | `8aa6a6530afca88899c105908e032387c51a2fa6019f4002726320a7c97270f4` | Type-4 receipt root |

These are maintained-client fixtures, not official execution-spec fixtures.
Type-1 receipt roots remain covered by the pinned Geth and EthereumJS dynamic
interoperability tests. The official execution-spec-tests blockchain fixtures
do not expose receipt values and therefore cannot reconstruct receipt tries.
