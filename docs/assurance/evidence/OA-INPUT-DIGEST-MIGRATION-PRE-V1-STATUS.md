# Pre-v1 Status Input-Digest Migration

The repository has no local or remote release tags. Six pre-v1 package
changelogs and their related status, security, roadmap, compatibility, and
release-candidate guidance were corrected so implemented work is no longer
presented as a published `v1.0.0` release.

## Reviewed digest transitions

| Module | Previous digest | Current digest |
| --- | --- | --- |
| `pkg/audit/postgres` | `f2599f3de3a9d8956a559080116127c3bea89962d0ca6e1cfe1422e9a64d0986` | `3c60aae4255be382c7c503814430c8b94cb89566116c8ce3d01c3900775cd87e` |
| `pkg/authorization` | `cba4369eea30402efac721490eaaf5e5686e1393c05f469a2831a6dce697e6ba` | `ac82fd8380e246c8071ac3b275467a86c3e8c7b2ac71aad1ef58ae3ac61d95ac` |
| `pkg/jsonrpc` | `cd8dad00f351b4a344c5bdc0dc45373e7924576984fc6100519327bbd282b5d3` | `f4bb43dfd934621ad320681a5afa14e3e3c4cc5d420ef9ba370249cf3b3c40b6` |
| `pkg/postgres` | `f2068c535834eef4630d287d290ba46c538a846acc0ac4d0f21fa209be2e7c66` | `3ea76193141f6969dc7d2aaa7aa21d73c69366e56b88d7d31c337195814305c8` |
| `pkg/queue-control-plane` | `4c87aa988057ae79d30362fad7a797c11392e5e8a6ee2fff646d63bfd0960007` | `529b0332cf1bc9e78c952680080e0f494cece35c4c1abaae6aa35c93afea377d` |

Only Markdown, generated documentation bundles, and documentation contract
assertions changed. Production code, public APIs, protocol behavior,
dependencies, service images, and the runtime behavior exercised by retained
operational-assurance scenarios did not change. The package documentation
gates passed with the replacement inputs.

These are exact one-way identity migrations for retained evidence. They do not
relabel execution times, broaden an original claim, authorize future inputs,
or claim that an operational campaign was rerun.
