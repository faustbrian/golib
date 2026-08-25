# Standalone Proxy Input-Digest Migration

Observed at `2026-08-25T09:48:19Z` on `darwin/arm64`.

## Change Boundary

The local release proxy now archives tracked module source only and excludes
repository-owned `.golib` verification tooling. This matches public Go module
archive semantics and prevents an untracked working-tree file from changing a
release candidate.

`scripts/build-local-proxy.sh` is included in operational-assurance input
fingerprints because it resolves owned dependencies for isolated verification.
Its corrected archive selection changes that verifier identity for every
retained reference input even though no production source, test, fixture,
runtime configuration, service image, dependency edge, or observed scenario
behavior changed.

## Reviewed Digest Transitions

| Module | Previous digest | Current digest |
| --- | --- | --- |
| `pkg/adaptive-throttle` | `5bc5dd1d5a489428fee9caa538589e856a47a28e3c9b8e26486a9e206bc7e3cc` | `1e28d483078659686ad4e85a9017795d9f9ffefd38e0677e3dc1f612b9559299` |
| `pkg/audit` | `3a6581a13dcfefecbf558987c83002199053ad52c98c30036f9d35848d36b5d0` | `282d3e449479ad70e71f7853f7a184af591a5864f7c58ac1078471be4741e6fa` |
| `pkg/authentication` | `45fb6f16190b0e6c385832177a9c7b6b9e181899617637e79281a8b1f28b5834` | `adce1bcd37c9732e3b1bc7e2d8a446189c8fc891d922cb3ba0a17f3c6b8b84a6` |
| `pkg/authorization` | `7509c4782dda8291e10c9ec91b8547d58804029d7a25575371368241b0239c4e` | `8e944608173313ff42cfc5bcf4d667a50cd57398f72b6cec4dded50e6745f30f` |
| `pkg/bulkhead` | `dd88bd6f51521658368ed8a0fc846701edd9fb01cf415c9fa30adb0240a44191` | `4e52f43e92c0f6cf73a33d7bc83df70c8e01bc76954b40b27dac45d1c8ddadc4` |
| `pkg/capability` | `744531d8d2787f4ad5beb78a23f4e08a64fad73d46bef9d37aa28ce287513fde` | `cfe189226296e387c90c7e61914ffd9d9a8eec0ad853202514e70d5402ca75f5` |
| `pkg/circuit-breaker` | `0b40fcf8d7ba772b04ef96c611c9d075ab3fa2014cf910ce31f1294f60177b67` | `2c795589c165ae689207643a5fda2aebcf855bf47e2f1024b04a58d97a662fda` |
| `pkg/concurrency-limit` | `29e972f5e89aa5ac937ecf1b07b2cdf4044ec6d7b876d0ffa6cf9354c3b6898d` | `0ae3cb1b614bcae878d47eb5a941b4158ba49b38c3ac0147ed43f1144df753e3` |
| `pkg/config` | `790d88777a916815bde2d167e4a938b61d5d9c9fc1546494f6b2dfea7db825f2` | `da32192a7946a249b09d269193526b61e15a7ba90f0b644d755b37104f5fe5f1` |
| `pkg/correlation` | `1825d1bd168221dbdb9b5f13d1a5d3168fc6556e54b526d1363d516e170deb1c` | `e2bb3356543bca69fd5a1e628d85d3d9b8b8c0fbb8030e1469a7848a6bb0c9df` |
| `pkg/filesystem` | `52e078d05e835451f61f41c4e550b96b99279a7ecee7000edd66c768457bb02b` | `0fe9314941b0f10288cfea91837110c682fb45e4f92f9657e110a36fd3b3a41b` |
| `pkg/hedge` | `350bb37952faf1079d0c5d953aab372a503c05e9a30e17935d816dd4f68ed5e9` | `99798339328e1916afad9bd1e39e547ae1e6c85309640eae604eef6021277489` |
| `pkg/http-client` | `0617e46e8416a5f48ab42321dc03fe27fdaabc8e5f561fcc823678bb158172cf` | `c94f81b5148a894923d29bcc0301d368c9de12525176b2c9fb88b2b1eeefa8e8` |
| `pkg/http-middleware` | `99a3c18aee467d830f3743ab5b7ca9c51a30267fd332c02311656346d7306fb7` | `1385283172f9dde2d748cd74bf852bb7566dfafef5b09effda22e209bb4f0d9c` |
| `pkg/http-signature` | `c03c7c87996b0353ae47cdc7cb722857eb648440a67de5b4f1c51d3999375794` | `94acab2a8cff26e03d9d4041cdf7f78d885b8118743dbeb2feed4ecf0f368aae` |
| `pkg/jsonrpc` | `3e1e9d5d868353d91b386b1fb4478114229c5a145af5cef8493de81299ee7b1d` | `5b3b52dd82f92007d698263cda734dad5c525064128e12e6c4c3bcaedd0ccfde` |
| `pkg/rate-limit` | `db194bf9086c2a31b6a52cf1a4082f6b079f7876bd0d1bc22002b36a4dd1a02f` | `c0bb3db89c4dfbed26b1ec93718a455d9e68c93707d3373b7e66632a66812cf5` |
| `pkg/retry` | `a88f6663a6e63b8a5f2a158e74a98b335b6d4371086e767eab92a2882649607a` | `e96bf2ece580142af645a264b3d4159e7f9a22178fb70fb972a71a3f369335e5` |
| `pkg/router` | `39978240d432f1ef3e391c0cfcfdfcab1c684e1c38469fca4f85133552c978ed` | `17f02650453c3690d036a9db0482825f285e1467d8fc23457e1a3f9f7fb5d012` |
| `pkg/secret-envelope` | `9b54688939328b4820ce0c8df75815853ce1b83474ba6d838744a0150f0aab90` | `2d32d62cabb98601c94485783cea838387b2ed073d9d612b892c7e0c915f0039` |
| `pkg/service` | `8fe06675f07c6117c50e92c927262a04d92beb9ea03a5d52c9a9b382cc95a312` | `3f5718bbd58bad0f7e2046e391ff528d99421feca523ec1c06c608344b6c5378` |
| `pkg/telemetry` | `1f2de50f7d920318b56ea8bfa3630a27777e3376dcf4c102b3550700b46bc8cc` | `22ca76ae56557af4f9b0cb824a74dc72e34bf43d24538affd6396168d9021c3c` |
| `pkg/tenancy` | `b7bc35f35eb7f551326359ef57b207ffb34a35e9fdd0212473bfbbf8743ac25b` | `b11757935d0caeca9902c5c25f8e41fcea441efad6e03b6ad7bb7ec298503ed2` |
| `pkg/validation` | `a541baee8df5134abbd67157c45afd9ad9c081a5bac6c7dbd0c665a3c2b06100` | `9684685863670f31ea7d9bac9cb4ed1a9fc3022cb475737b20d4cab0f4b5f80a` |
| `pkg/webhook` | `4170de093e88456afaff6007c4559cce4c194fe2b7bad2d34050c1c460606a7b` | `ab83c3873a2d6c68cea65942fb94103e44269faa4f5b9a2eb39f2464d84238ae` |

## Verification And Claim Boundary

The transition set is limited to the 25 inputs referenced by passed
operational-assurance evidence. Each current digest was recomputed with the
exact Go, platform, CGO, kernel, and Node identity recorded by its original
evidence. The retained evidence paths and hashes remain unchanged.

This migration authorizes reuse only across the reviewed release-proxy archive
selection change. It does not authorize migration across production source,
tests, fixtures, runtime configuration, dependencies, service images, or a
different environment identity. Final standalone release and public-proxy
verification remain mandatory.
