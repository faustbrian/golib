# Isolated Module File Input-Digest Migration

The repository isolation wrapper now exposes its task-owned temporary module
file through an opt-in environment contract. Nested Go tooling can therefore
resolve the same current owned-module graph as its parent command without
leaking the parent's Go command flags into unrelated child processes.

## Reviewed digest transitions

| Module | Previous digest | Current digest |
| --- | --- | --- |
| `pkg/adaptive-throttle` | `90b700a7d752b87300fc6fbba0b97142be0275bd53fc6b92f31a9b435be3921e` | `1e35e53c20a0346e6e8b27d22b526f90591a838d0a69c7851f16916b4c2b5346` |
| `pkg/api-query` | `d3f6edda5b269083d04afb97137e9a4b86e0532e4c3eb4ea9c96db93c2c84b67` | `cc3212dfc3c205eeb5557f001f7c4d5f59459553a5fcfe4f83d5f2020c9b1633` |
| `pkg/audit` | `f6c6b6ba4f0e1de5948efeaa6f7f7f918ec5e27f1d2832da8e25167424fef068` | `8bca80404cf6ed39b6aa0db37e8977c84e3004af6be844bcd7236b18c58e23be` |
| `pkg/audit/postgres` | `4312eb1062cba2414e2ec4b11faf477977bff8f1a26000398855ad3f63558c5b` | `5e597f0f3f96c21c80e894d69cded9c4e22ea4112e7fb90c788927b88d9baeec` |
| `pkg/authentication` | `92f240486d4ac8cf44da3d3d6e78367a6ba1203227841e016990984deaf9e8cb` | `63c2593205700ee3281e4b4ae7488a70815867953f002bbd9f0debc7dcf165bd` |
| `pkg/authentication/authotel` | `6c707c824b1b45c4ee8539e95ff8ba7dae0d49244d00c6381a0691c03f793f8c` | `3f450a3cf4dad660dd6f21d0c5c25d7b8062527095f9dfdd74f26e4035c6acec` |
| `pkg/authentication/jwt` | `9697872ff954283b4edd5cfc881f114bbbaa37b9f030f927c7353697f426d8dd` | `966fa416ceff0d913925a65ad51327cd127eb4293069df332550df31a693743c` |
| `pkg/authentication/oidc` | `d2578c73dc393daa4e3d78367e5730952f5b93d36482246e813cb45f41a5cbb9` | `8ac43e8e1ca913bf1fa814e521601a73b88c9a4a41281ff1d642de6d142b05a0` |
| `pkg/authorization` | `c02eb0b06ba95ea5300462b723d5dbfab8d0f67569c8bbbbdc4c61378cc5a028` | `dc6161aa315ee514b6b00feeb52acd5598103759163f5954464b323726171866` |
| `pkg/bulkhead` | `a627f260886013f6fbf1b878a32d01d115d885b405ae43f83058e97c2937da7e` | `4414ec5ec03926888c54d2ddf99db28cdffbe7049c80dd20aafbe7d51cc0e0c4` |
| `pkg/cache` | `5dc43809fba104861e8697df1922ada80c1aa650c19e614c6db770da4329b7bb` | `02f3d79289602562c696e4b90a586791e6d5ac88fb006ced6a4bbcd45e0afca8` |
| `pkg/capability` | `e67c099e710406eaf0eeeee86212810bcedcfa3b47e871206f83643202e86e30` | `368d75f9ce8339c6b87e5531c2eb1330442696f4cd9c3e9d3c844199196fb33d` |
| `pkg/circuit-breaker` | `d45fb76c9c28db3bea6bcf3a1907ed90d7458826ef6c423a981793fb0bae0e50` | `3a3bed9f58779658daef87b15f1ebce7c49da2e47b4f4e6519219c65c3d42374` |
| `pkg/cloudevents/adapters/golib` | `86c0998887c64c152eca0c62b8374f679629103ac4b869bbf46a0b66db3752ae` | `5624042ba9a4eb3b989b7a003949f9acc4894b8b223fdc17b26189abe1501059` |
| `pkg/concurrency-limit` | `e41277832c42fe06fecf9b7aa9a2ff7ad8ffe608a3cddf29ef5713c2084f301b` | `08cd883248a2f5328d37a2dc460a297bc426272c59403328bb34df3cc22ed804` |
| `pkg/config` | `9a0b14df3598b504e08f20d8b237487477e9a88cd15a5e607712fd074b84be2d` | `e728114cfde34ea51ebbeef979224dda0a33ea2cc126c48784365db5a8295534` |
| `pkg/config/adapters/awssecretsmanager` | `c3eae83ad77e6e09cf472807791af5136458c7908517898a51afa0b05413d703` | `2f1a9aabc334c8b9303a2649fd7bce5cfb5a05a574bc9e8e23097a3d97e164ae` |
| `pkg/correlation` | `0f3902eb02d24a3c1a20bc27d207c8ed4bbffdb00eb0d613902ae33b9292eb35` | `b8506df6ce9b6a2eba7b0c41a05ffcd3e19dcc175debfbf54c86d415ccd33fc4` |
| `pkg/filesystem` | `491136aa3ecbbb6f8d3595f49524fe4e6e0862611a9ac97c7fa7e57499bbb02c` | `e7555cd3fdf606b2cd386983ba0cd7ef53a8505f7c68876931256b1324553f7f` |
| `pkg/hedge` | `60c51549bc99bb44bb4245b8416cec768a6e610cbfc501250b7638d2d5dd7ce8` | `7e2f074868ff50d1e3789d907a021ac080b06337b1004da5d9654afc37dcb11b` |
| `pkg/http-client` | `09f27858307481625c72146e339a31fe14111f9315d66476039672846870034b` | `a3abb6fd97da92f9ecc59cb6428e68d3d5940b9d9cd86c8f83bf182f8eafa793` |
| `pkg/http-middleware` | `3e5aab072a04002dc24cc428a692cfd1bc7c4d715c60a4ca86f919bbb8ed3f10` | `22d7763f7b9812dc6d3d1f055864f0d8329d275c0eb4ff71fe90e73b73df8ad1` |
| `pkg/http-signature` | `b0469e0701518e35db9c4a301d7321e7656fea64400fc0a3c9ea945d6dc5a457` | `139b587126cf83a49f58cef4069db234f87b016d09e472d308be0f6a281f07dd` |
| `pkg/idempotency` | `f42cc39db74b948598135f92ffd2f95d4288da2d7eaf5b4a63df2e3264f4e8f0` | `00dba8d0f7cccdf6da7795b168644450f41780c67bdcfa6b065fe791177ec601` |
| `pkg/international` | `1e4775353769d449c69c2e4de77b4d78bf5d47dd9f40a4ab981f467a515be77e` | `1a02aa5f30d6a8f9e547afe0ed4ede022afbd46ddb81bc66f7b3961e23100bea` |
| `pkg/jsonrpc` | `4e6823bada12a6222eb029f172513bdd46d30805ba8322302fc3b7260fcddcce` | `35f718782669639b5b4b5bd1252bea77ac4e5baffafb6a412d0b7dc860b095f6` |
| `pkg/kafka/kafkaservice` | `2c570e2ffa89a05b079efc5a3b8e02a3385064f987dd426a0acb67d7e833b799` | `8773f87a77e6ae672d02d439de2af35a2e6a47877a6fb363afe2514001679bdb` |
| `pkg/knapsack/objective/gomoney` | `3b8ffdb9567e4471e060ea4a84fc430a50fd03e7f0bd15fcb41650aa114e60aa` | `36c76787e3d2b9d85e31e47d3db68b3f9408c6b221b25e44b649a95da665d7c6` |
| `pkg/lease` | `f07e281aa76bf8122bb95423bbcb217f3a8f09b5fc983986ec0725b9b232bddd` | `0b8a5dd287f334fae526576aca838673dedc033fe7348840de51bcb312cf28f7` |
| `pkg/localized` | `e8368e3e516b5eea36286ebdea14ca61e3ff24b649a2f2e8f4f4dd86c6310978` | `f9e4a3ef9912b2f611aa4285c79ce9194d61dc4a3da22e229a4c92806f2bec83` |
| `pkg/migrations` | `f7944477dd26e0eed32d1ac4c9c956790349c9cf32180d657bf1a62c0bf7d45e` | `cf7f042fc41dab88753be913faea7d2b69ad468fc5dbf616b8969a7d476faaee` |
| `pkg/money` | `e7ae60ec997acc4ccb20a5101db02de0f39e362851f9d6e3d160793ef13ef85c` | `08b6e5b471dde2ec126d00e2920df6c6bf64063eb1ac46584bed2e266e391d1a` |
| `pkg/opening-hours` | `39be2eb684ad5294ff1d43e4af67effed6251e36317050b02179e972dab75ba9` | `454354675db0db2789aebdbef5b776dc97887986846b1099bb315483fc63622e` |
| `pkg/postgres` | `3bf283b9d05c35a81e5b5431a484b70dfc15215592bfdc415d7cf594558abab8` | `7d058e56b4462dada8d4cc79d0c2fa99533a7e70dfe0c8bcb30199b9825ff51e` |
| `pkg/queue-control-plane` | `8e9ff310949b0bccf07c992b2b3ce2a9c50a7c9a72352f6a8278d858b3bcaefc` | `b152da368ae7ef1aa5aeb3f3025505a1aeaca5b08ec75f64c8e8cb0f0b4cf070` |
| `pkg/queue/queueservice` | `0a6f5dd682536971ca66ad4a5b801bdcf2ee5a387b3b586cd4c9bb6bff60b403` | `ed9ddb4649166b814fb4a16c6811f5a79d66dcac020018d0ed7b55d3794ca9dc` |
| `pkg/rate-limit` | `52c54d5d65eb5b4afe0186a083a871a9b692ac15f02a2ae6da56d79e5009a47b` | `f3f84e14ebc1e66486cdce7c4b70843d22325befd4272498cf91dd2bc58fd5e8` |
| `pkg/retry` | `d5f09cc597bbb3a11355e5ac99a62c7c02ad446e4f6b58c7b1d43fc54057d67f` | `4cf35ca66c7c40b64e6f27e5ab4d1240374fb7c0d5d421e998ef6cb38b0dd7ee` |
| `pkg/router` | `3b46284bf66967b7fdf4aa96513a5193716324a2dc96fee5bb8efbd94117186c` | `cde569c7b7ebe5aa7be7d900ae13cf57dc50d8bc998771f787df41fc85901781` |
| `pkg/rule-engine/adapters/temporal` | `f72fea9ceed0400f77bb6307d7c6fb3e427f22a4542d1cb77ba473cac18c6c16` | `1db8a2b45623d4c7b893b6181ea362dce1087fd049efa233073c68c17f27184c` |
| `pkg/scheduler` | `68c8a6851301f71021daed2fc837efbec07fee96762dc3e0bc233197e59cf16f` | `4f5c7c3c53708c11d94d717f43bfc307e016e3807ca528b97bda2aeecbd5c81d` |
| `pkg/secret-envelope` | `99841b88401ac998136dd7373067d9d2683acd4054c4280a3566eb00c2ff97ed` | `68b554045964fa747360f952504cf1e4267239725eeb25bc5bbd306402e9adbc` |
| `pkg/service` | `423ce92ab924d596461ad3adf5875be11b6823512699e25226d8d73aaf06b927` | `aabd01d51e5755d1ce4fa29c419361d29d7b624ce08e4e8f876b5e47838ff0ac` |
| `pkg/telemetry` | `304038e29d7d61f7c57cb2f91632e527dd2a73429d3cb462810703a29f081f82` | `e86ef843f3aec3a9762b23e79e9b6b42d5272be0415378ca8e9a4afe929ebe8d` |
| `pkg/temporal` | `4f2a700d0537b4859519374a217f5011d17bf72e124eeefb4e965e4e9d332e11` | `d5d07be9fbd809caf4829649ad78d450d2c2801ba7800ca36629e9fc1fca4594` |
| `pkg/tenancy` | `924f8925d6151cd80f06599a2b3b43db5a9c8b49e0b0bd6919b89be1c09ad1b0` | `64cc0d3f61f8e88880e821083caccb030ba174761c03c4d4913be3b6b8c9cd86` |
| `pkg/validation` | `acb07ded26fcbf2ae8ed6654681f869d4d3f8dabb7ff22c5ef29033160070cd1` | `7eb84a03c2ddfc0d364015bf8d765667da0367d0d18521156a0a80442112f48b` |
| `pkg/webhook` | `3ef59b6f1301d29dc32f126ca32680cf7523b91c620d4bfdc7151895837ac3ca` | `0ec0d35ebbaab4a34ece243a6260bc3278d0be156e1360e7a23edaf35f93abef` |

Only the package-verification execution contract changed. Package production
code, public APIs, dependencies, service versions, and the runtime behavior
exercised by the retained operational-assurance campaigns did not change. The
new environment value points to the same task-owned isolated module graph that
the parent Go invocation already used.

These are exact one-way identity migrations for retained evidence. They do not
relabel execution times, broaden an original claim, authorize future inputs,
or claim that an operational campaign was rerun.
