# Verification Snapshot Index Input-Digest Migration

The disposable verification snapshot now preserves the tracked classification
of staged additions even when a repository or machine-wide ignore rule matches
their path. Before this correction, the file contents were copied but digest
discovery in the snapshot could omit the path and disagree with the source
working tree.

## Reviewed digest transitions

| Module | Previous digest | Current digest |
| --- | --- | --- |
| `pkg/adaptive-throttle` | `ef92b812354b9cebb51b9d9e7f74a605a42d190e4806e951c5f3bbebd2fa89f4` | `90b700a7d752b87300fc6fbba0b97142be0275bd53fc6b92f31a9b435be3921e` |
| `pkg/api-query` | `c173a1579ee8ccaca11091db249e009d6b80e1abac4e3438b67a86503c5b3768` | `d3f6edda5b269083d04afb97137e9a4b86e0532e4c3eb4ea9c96db93c2c84b67` |
| `pkg/audit` | `913ba366fab616451eeecbec1f5923ffa5e7b98e80dae5123900ca89a3805bda` | `f6c6b6ba4f0e1de5948efeaa6f7f7f918ec5e27f1d2832da8e25167424fef068` |
| `pkg/audit/postgres` | `3c60aae4255be382c7c503814430c8b94cb89566116c8ce3d01c3900775cd87e` | `4312eb1062cba2414e2ec4b11faf477977bff8f1a26000398855ad3f63558c5b` |
| `pkg/authentication` | `fd0ed199914739aa42e3efbdfc8346cfb0565cee724de57a14fcccfe6c73d086` | `92f240486d4ac8cf44da3d3d6e78367a6ba1203227841e016990984deaf9e8cb` |
| `pkg/authentication/authotel` | `dda5503550de2f2a0fcbf0797f9cf53b0db20772cc89436214c77c07ad69b707` | `6c707c824b1b45c4ee8539e95ff8ba7dae0d49244d00c6381a0691c03f793f8c` |
| `pkg/authentication/jwt` | `67c480a408d4b118534a901d24d9735a8279ce7f433d9f3977a8fafa34f379f7` | `9697872ff954283b4edd5cfc881f114bbbaa37b9f030f927c7353697f426d8dd` |
| `pkg/authentication/oidc` | `32999f4dec3baeb808b8f7e8ea04c141c3e01c68f906a2b51fb57c6732c2e3f6` | `d2578c73dc393daa4e3d78367e5730952f5b93d36482246e813cb45f41a5cbb9` |
| `pkg/authorization` | `ac82fd8380e246c8071ac3b275467a86c3e8c7b2ac71aad1ef58ae3ac61d95ac` | `c02eb0b06ba95ea5300462b723d5dbfab8d0f67569c8bbbbdc4c61378cc5a028` |
| `pkg/bulkhead` | `dd9a38c79eb412652607fa439b0e1b0f8a0e93cc7d559dd124af09c04f4f312c` | `a627f260886013f6fbf1b878a32d01d115d885b405ae43f83058e97c2937da7e` |
| `pkg/cache` | `eba9d5eced351971f3306ba0f173a84f1ce16b5b4471d54ec81f6f7178c7dd66` | `5dc43809fba104861e8697df1922ada80c1aa650c19e614c6db770da4329b7bb` |
| `pkg/capability` | `30fbd87edaa01a1c35b7397dd343c7b8fc2a92a17adc03bdaee8587a578289d9` | `e67c099e710406eaf0eeeee86212810bcedcfa3b47e871206f83643202e86e30` |
| `pkg/circuit-breaker` | `b0efe663410bbe59928d9d575575e4025b352cdf18d64e9d4e5ecbbbfbcfbdd5` | `d45fb76c9c28db3bea6bcf3a1907ed90d7458826ef6c423a981793fb0bae0e50` |
| `pkg/cloudevents/adapters/golib` | `a86cc1192f27b7df7b3916e3f25b69218b34b6e5a5740c4659c2de0771cee3df` | `86c0998887c64c152eca0c62b8374f679629103ac4b869bbf46a0b66db3752ae` |
| `pkg/concurrency-limit` | `04aac61dbd20a2a9b69e8dccfb1ffcbc70511f0a405ade8f7ae1519d63794fe3` | `e41277832c42fe06fecf9b7aa9a2ff7ad8ffe608a3cddf29ef5713c2084f301b` |
| `pkg/config` | `29996aa208d91464bdbfef3446f802ea20a1c907222c936b5bb69cdd9b18a370` | `9a0b14df3598b504e08f20d8b237487477e9a88cd15a5e607712fd074b84be2d` |
| `pkg/config/adapters/awssecretsmanager` | `3dfcb298f6c2fe4df9277f939f6e22f49f42d3a549c031ec158e5c22ba8f8f3f` | `c3eae83ad77e6e09cf472807791af5136458c7908517898a51afa0b05413d703` |
| `pkg/correlation` | `290fcf8583a814cb6dbee1d08fd6b0c0dc1a4e7ebe73c1199c578b7638442308` | `0f3902eb02d24a3c1a20bc27d207c8ed4bbffdb00eb0d613902ae33b9292eb35` |
| `pkg/filesystem` | `3b8c99344356921a61a49a57f3ce7dbf4cc99c17d201c177d86593ae3c4dabdb` | `491136aa3ecbbb6f8d3595f49524fe4e6e0862611a9ac97c7fa7e57499bbb02c` |
| `pkg/hedge` | `2b60572f6a1a48213f436a1dc0e66afda9bc1df9a6a5e9c1c77d464177a51384` | `60c51549bc99bb44bb4245b8416cec768a6e610cbfc501250b7638d2d5dd7ce8` |
| `pkg/http-client` | `d0e54524cdfa08aa023935060b6ac9f656a447ebde09d7787e309a9bb1232317` | `09f27858307481625c72146e339a31fe14111f9315d66476039672846870034b` |
| `pkg/http-middleware` | `6308192686c05be4ee749e560a2790a38ee3f11d79bde0cb675ef0608f9b3387` | `3e5aab072a04002dc24cc428a692cfd1bc7c4d715c60a4ca86f919bbb8ed3f10` |
| `pkg/http-signature` | `9df96c2a5004c341ecff28fd65ec2490df90cc6e42eb9eef3fc9a88abb9efa4a` | `b0469e0701518e35db9c4a301d7321e7656fea64400fc0a3c9ea945d6dc5a457` |
| `pkg/idempotency` | `907eb47647e0a82f1a251ca5c28a311361844f27ac38bf13ee721af04f882009` | `f42cc39db74b948598135f92ffd2f95d4288da2d7eaf5b4a63df2e3264f4e8f0` |
| `pkg/international` | `7d5b33d57b35d79ef082f011debe9ba39d3def92da970b4174b91a0098bf8313` | `1e4775353769d449c69c2e4de77b4d78bf5d47dd9f40a4ab981f467a515be77e` |
| `pkg/jsonrpc` | `f4bb43dfd934621ad320681a5afa14e3e3c4cc5d420ef9ba370249cf3b3c40b6` | `4e6823bada12a6222eb029f172513bdd46d30805ba8322302fc3b7260fcddcce` |
| `pkg/kafka/kafkaservice` | `3174759870decd5f3adf9ba053cb314d7d8a5e6dc7992b03867847ce751a8cb5` | `2c570e2ffa89a05b079efc5a3b8e02a3385064f987dd426a0acb67d7e833b799` |
| `pkg/knapsack/objective/gomoney` | `091089f02f467e4d9df64c7aee50cb9de9ad33a3dd35d8b0f0e3001082307b7a` | `3b8ffdb9567e4471e060ea4a84fc430a50fd03e7f0bd15fcb41650aa114e60aa` |
| `pkg/lease` | `eba9351d5e1db6e90508721490a4dcff097711c2a34e5e88ff38ddf599594834` | `f07e281aa76bf8122bb95423bbcb217f3a8f09b5fc983986ec0725b9b232bddd` |
| `pkg/localized` | `4abf428f0e628e5ad4d1211c7a53a61371bce7407fda385517d22995ba4117fa` | `e8368e3e516b5eea36286ebdea14ca61e3ff24b649a2f2e8f4f4dd86c6310978` |
| `pkg/migrations` | `ee41d7c642979cdb3894773a9950f9cf3ec28851c9ce78a00534741d229db8a5` | `f7944477dd26e0eed32d1ac4c9c956790349c9cf32180d657bf1a62c0bf7d45e` |
| `pkg/money` | `bd7e9edd375a79abab6aec86593f1f7119ded6d3674f1a99a549ecb8ddf11947` | `e7ae60ec997acc4ccb20a5101db02de0f39e362851f9d6e3d160793ef13ef85c` |
| `pkg/opening-hours` | `1de3e6ac48a731fad77d7e1f1d29b852a229f6e5fa93f12abeebd113f95b4f5b` | `39be2eb684ad5294ff1d43e4af67effed6251e36317050b02179e972dab75ba9` |
| `pkg/postgres` | `3ea76193141f6969dc7d2aaa7aa21d73c69366e56b88d7d31c337195814305c8` | `3bf283b9d05c35a81e5b5431a484b70dfc15215592bfdc415d7cf594558abab8` |
| `pkg/queue-control-plane` | `529b0332cf1bc9e78c952680080e0f494cece35c4c1abaae6aa35c93afea377d` | `8e9ff310949b0bccf07c992b2b3ce2a9c50a7c9a72352f6a8278d858b3bcaefc` |
| `pkg/queue/queueservice` | `92726aa0988dbba58a99578da13ad03e523b1765440c95219d349ba7f36c4532` | `0a6f5dd682536971ca66ad4a5b801bdcf2ee5a387b3b586cd4c9bb6bff60b403` |
| `pkg/rate-limit` | `7f7c5ec7145b0729b9557f6bbcbeec8e5cf9777533279fd0e647d02bbb9075d5` | `52c54d5d65eb5b4afe0186a083a871a9b692ac15f02a2ae6da56d79e5009a47b` |
| `pkg/retry` | `5a0ba6c3f3197ccff6b917626cae4831cd15607f3e903b66171afabb29f9e42b` | `d5f09cc597bbb3a11355e5ac99a62c7c02ad446e4f6b58c7b1d43fc54057d67f` |
| `pkg/router` | `288dfef6a6e9d32a75882bbda58f19881c0742a59cfd0c2135a1a391faba9a8f` | `3b46284bf66967b7fdf4aa96513a5193716324a2dc96fee5bb8efbd94117186c` |
| `pkg/rule-engine/adapters/temporal` | `2d099a1ab02ac09928f90382a01932b7ff372bafb5739f9ec7f5cb575b2afef3` | `f72fea9ceed0400f77bb6307d7c6fb3e427f22a4542d1cb77ba473cac18c6c16` |
| `pkg/scheduler` | `10e4ce4956e6468cfb0404f5587a52954ab5e131db11c1ced1dbcc3b1ea32ff6` | `68c8a6851301f71021daed2fc837efbec07fee96762dc3e0bc233197e59cf16f` |
| `pkg/secret-envelope` | `35ad1e45415286c9c2bb58f739a91a35a6794d99aaeb9a658b7ee3a3222518e3` | `99841b88401ac998136dd7373067d9d2683acd4054c4280a3566eb00c2ff97ed` |
| `pkg/service` | `5c426a16fc81a79cca4f62eeb43d59287623eaa76329f9df8a69aba06062f653` | `423ce92ab924d596461ad3adf5875be11b6823512699e25226d8d73aaf06b927` |
| `pkg/telemetry` | `0fb841595d6fc870057d7a59dfbc1ba39447ab4a8ddadedb59697a9ebea88087` | `304038e29d7d61f7c57cb2f91632e527dd2a73429d3cb462810703a29f081f82` |
| `pkg/temporal` | `312564be8e839abc521801d6f1465d6b8911387f8e53029db2493f3bf8c58838` | `4f2a700d0537b4859519374a217f5011d17bf72e124eeefb4e965e4e9d332e11` |
| `pkg/tenancy` | `9b9f0b17a046a57afdeaaaae73f371cf9221705c04bb567075785bec24da063d` | `924f8925d6151cd80f06599a2b3b43db5a9c8b49e0b0bd6919b89be1c09ad1b0` |
| `pkg/validation` | `9cd7ca5393d88b5d54e2c038056f330c5e10b194762e0cd823f03df9aa6ca11a` | `acb07ded26fcbf2ae8ed6654681f869d4d3f8dabb7ff22c5ef29033160070cd1` |
| `pkg/webhook` | `9d408f163195a3dafc2b86ea5af6bb07fd98372973076e5f3a9f0135ab37c1d8` | `3ef59b6f1301d29dc32f126ca32680cf7523b91c620d4bfdc7151895837ac3ca` |

Only verification snapshot construction changed. Package production code,
public APIs, dependencies, services, test behavior, and the behavior exercised
by the retained operational-assurance campaigns did not change. The corrected
snapshot and source repository now calculate identical input identities.

These are exact one-way identity migrations for retained evidence. They do not
relabel execution times, broaden an original claim, authorize future inputs,
or claim that an operational campaign was rerun.
