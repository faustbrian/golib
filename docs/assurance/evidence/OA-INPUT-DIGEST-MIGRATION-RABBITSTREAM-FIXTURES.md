# RabbitMQ Streams Fixture Input-Digest Migration

RabbitMQ Streams support adds task-owned service setup and cleanup branches,
service-specific digest inputs, and four cataloged modules. The two retained
operational-assurance reference campaigns do not request RabbitMQ Streams,
exercise the new CloudEvents adapter surface, or alter any pre-existing service
branch. Their observed behavior and original claim boundaries are unchanged.

## Reviewed digest transitions

| Module | Previous digest | Current digest |
| --- | --- | --- |
| `pkg/adaptive-throttle` | `27f157e36a8dd423e1d149501e3e9ed0bc5c8ae90e1d17eaf896011b3553c5b9` | `5bc5dd1d5a489428fee9caa538589e856a47a28e3c9b8e26486a9e206bc7e3cc` |
| `pkg/api-query` | `25f068838b48d46e534888a7d9c0505b25af946ca6341de54c8d8b1aa20dccea` | `74b76c2e3b3a1fb6d531657292a0520fffec1cd832ec5d94df21619090620bd0` |
| `pkg/audit` | `8fb0f0172e3c56aa7df15fc30295c3222407285bebb96179f963a8dfe0d715d6` | `3a6581a13dcfefecbf558987c83002199053ad52c98c30036f9d35848d36b5d0` |
| `pkg/audit/postgres` | `02a4968c0208090ac1cc4c05e77b16ff8d3604ead71cd028370c21c114a68e2d` | `b5c2e89ae3be34cdc2707dc18c3c1afe1ee3f953225251add346b7534b991ebb` |
| `pkg/authentication` | `8eb64b8a8967245b7c605ee26bf5ff2f059fc2e4797cdb3829711d081a4d0d2f` | `45fb6f16190b0e6c385832177a9c7b6b9e181899617637e79281a8b1f28b5834` |
| `pkg/authentication/authotel` | `d44e44d0a427ae5e2d41632defab3bccf5fc8b0f86b41632c491e3a5be814076` | `643c601ff10a7d4665e51bf19f57aa20c7f70c5d337e6a6af5cf522da99e6799` |
| `pkg/authentication/jwt` | `79cfad50c565919eb6bdbd7d77a7d256db96b0a875af08f87ed5b153567651dd` | `4b606119dba64848d369d0e4e358c326b211f3b5d8c45fc0ec37a0fae5dfc8c7` |
| `pkg/authentication/oidc` | `ddc1c49e1ffa4f0775b52c01e1abe6a2f9e08f3d1323ec543a283cccdda4a360` | `cf034a6d4060799d065667cade414b3aac30e196c2cc94d3a176f31f40f4c53d` |
| `pkg/authorization` | `09863b3c9ba90ba4170a15204c0b74a439222a9956d0d0fbfcce4b5a877a1908` | `7ad588e59d1e7da4873c73d8c34a455825fc802a22b9d78720557877bb883765` |
| `pkg/bulkhead` | `6b1d4bde8d3c0d394194af394c7c46bf6c110fc8e72f7fbb8b15a32d5744c061` | `dd88bd6f51521658368ed8a0fc846701edd9fb01cf415c9fa30adb0240a44191` |
| `pkg/cache` | `05c7dff6b9b7556e309f4e3e7bbea3acec153d0caca6f813b17d26ffbbe9e3b9` | `cc0cf8634170e9db3346c77a78317e0e790bb50544b8841d29ee093e406d255b` |
| `pkg/capability` | `e0e1563d3c14d8198258bc14c51d49137778ccbd3d87d42f12aa079e6b8ce4a5` | `744531d8d2787f4ad5beb78a23f4e08a64fad73d46bef9d37aa28ce287513fde` |
| `pkg/circuit-breaker` | `dd3eac9d4075d99f8a2605fe5a250da586248f376a9791187c73802999d0e238` | `0b40fcf8d7ba772b04ef96c611c9d075ab3fa2014cf910ce31f1294f60177b67` |
| `pkg/cloudevents/adapters/golib` | `c0fe0dbb5b95267e9af4d374101522fcbe80d8f8d75329cac80d8e7e498779e9` | `f42e88c939a801184c2441ba13cfbfa8e19873be826875487db995ad3f4255ca` |
| `pkg/concurrency-limit` | `3f6203ccbaf9466f5a1b91ef2c8808beebf035273fd1dad0c0c415807e6c38e0` | `29e972f5e89aa5ac937ecf1b07b2cdf4044ec6d7b876d0ffa6cf9354c3b6898d` |
| `pkg/config` | `7e033fc546b6da34e0032ab344e66beb0fc8ed03bf2429ba8118a2d9813324f3` | `790d88777a916815bde2d167e4a938b61d5d9c9fc1546494f6b2dfea7db825f2` |
| `pkg/config/adapters/awssecretsmanager` | `a2a04e3816532bec18928864a9afc8afe5c19fa7d7382dd5a01b26a3ddca557a` | `c8c5ed192606ee51e97881744e40e79ad035f9247a92616993ef4e0201314f9e` |
| `pkg/correlation` | `d5ec462876b51f97f0daee19ce3c1fd0e16bf9adca946607faf7c78327558b37` | `1825d1bd168221dbdb9b5f13d1a5d3168fc6556e54b526d1363d516e170deb1c` |
| `pkg/filesystem` | `dda6f289d0d163624d931b3ff0d1591a1890a45ca703d68245418709ba7d25a7` | `52e078d05e835451f61f41c4e550b96b99279a7ecee7000edd66c768457bb02b` |
| `pkg/hedge` | `c603802aaa519f01d3b7fcc0939c738bad5ce57ecddf489f40af21358a4905a2` | `350bb37952faf1079d0c5d953aab372a503c05e9a30e17935d816dd4f68ed5e9` |
| `pkg/http-client` | `4fd30885a53d367e76bc0581f43f0f99f9a05b626388d9796a36bbab666851d3` | `0617e46e8416a5f48ab42321dc03fe27fdaabc8e5f561fcc823678bb158172cf` |
| `pkg/http-middleware` | `28ba6aea48363328ba0a46ecf9f7ce65826aca9e87f696abf4a8e680135b3c5b` | `99a3c18aee467d830f3743ab5b7ca9c51a30267fd332c02311656346d7306fb7` |
| `pkg/http-signature` | `35a2237f004991e403adcbd011637715873144e7ba56d250d00ed9a505e827bc` | `c03c7c87996b0353ae47cdc7cb722857eb648440a67de5b4f1c51d3999375794` |
| `pkg/idempotency` | `1fb75e5ec526a60c2828d1c1b1a3e76d1765c5a3778294b996fbb645bd61141c` | `bcddca65e21a5c20164319a573e82a4e791c70f26014eb5dfb0dcf0c25edac6a` |
| `pkg/international` | `978c8d23c301a5e8ac2f2e69f7e5ddb62f4dc68ba4647b1e0e2cb666b0f44f9f` | `dc0b531e334d64007a2961da583320a1362709151517196486352e1ff61d2d04` |
| `pkg/jsonrpc` | `3dfa3f17702df77a5daa5e1818fe0387baa90cfe1d8a2085066b2b7d9c0554e1` | `9b9c50ad2a57fd619eabfa11bba6ced6964f243f6c130fdba3568cd2dc0bea4f` |
| `pkg/kafka/kafkaservice` | `3811b09b4ced2fb66263495d183a56db298b9c61abfff4f16ae27d1952a01672` | `4b75da04d4f348f23535588d9b068a1bc264f57414e9256b8b4a48e1429d5e6c` |
| `pkg/knapsack/objective/gomoney` | `821617ca0f01f200c49fa9f0d7589e310f0a506fbadc54265b5679f37193823a` | `42e85300717b76251490c8047a45000ba84bc0423beaeff7ba1426b33381c1f4` |
| `pkg/lease` | `6aaf18768fb87e1a316db7a31356f97770f9798a1a2d703e0e009e4aca493b9f` | `54ebec6aa5385112dee436eba35a19bac0cac3f72f1c759d1caf7f0ad2c785ea` |
| `pkg/localized` | `8055919b6ec127a6bf1a379fc98e09928f4e73916129815adbd68da43af823f5` | `56d2953c51ced8407b4835ca03b443797b9e3c1dda394130b2f3fd0dd807ebc8` |
| `pkg/migrations` | `c8eecf5a59ba827049790a6fd854af9fc6e7341cf7106054db58d74669b22d9f` | `614ec88e7696b959ec2e65f86f3d9866cc8260e2d57ed84f51c94f22ae2d5c68` |
| `pkg/money` | `3e4731d08845a4245af2ef9fd66581df437c7fadc0b70eae7be9866e54c15e11` | `7f053f0433182189e029aad5bdcb36bffaa71b3bfd0365177f9b133a96256737` |
| `pkg/opening-hours` | `482f3ad20f110c9d0a98e85242f2737889c5f5a75a8ef70576f91d70fa98e111` | `3651f5e664d11e3676c32efe02d3c8a10a08d0e886bb87e4e40b30a28b535732` |
| `pkg/postgres` | `1aad648551aa0772802ae1bad0688e078cb5e77e9aa1119205b94d86288e1df2` | `a22facb7e9170bc78d995f526ed4ac53cf117ecbce12aa2ad5aaea153deef19c` |
| `pkg/queue-control-plane` | `c50fd66824a7a8700e566797e72fcd7d46616b157672926705d041e789e953e8` | `d53a2100e31e208117316d25782e77c5b41c8c66d346f6a0a4513f766ffbc075` |
| `pkg/queue/queueservice` | `2baa42223eb6152cf06d85862bf7a9f9f3c43b1bd1e012a2907c14ffe39f3b0f` | `f079e521846e0b53167d29fcf90e0a73097707ce4f77eaa9435f18a1f1d4c74a` |
| `pkg/rate-limit` | `e1d9dd180858239c137ee4c7812e988003a11535afc8117e53aa1b45b40c0c7d` | `db194bf9086c2a31b6a52cf1a4082f6b079f7876bd0d1bc22002b36a4dd1a02f` |
| `pkg/retry` | `ac9b793b5eda5dc5590788a61d421dcf09fd2f30b4be550e9022a966788df900` | `a88f6663a6e63b8a5f2a158e74a98b335b6d4371086e767eab92a2882649607a` |
| `pkg/router` | `8f76154bbb8d2633c02b6c1f58463339a96deca87fd7dc1e779ebbd7b6a6a242` | `39978240d432f1ef3e391c0cfcfdfcab1c684e1c38469fca4f85133552c978ed` |
| `pkg/rule-engine/adapters/temporal` | `6d71f4efe81357a9f7b0958b2911b92b591e5f5027472f53397e69377b16fbc3` | `4c08e43e210514fbd712832d7a61223054f609a171f9472489deab3a0e460942` |
| `pkg/scheduler` | `71618a4e61f716b230d38afc9e1f7ce9c540ee8d5470819e3e9010d693437118` | `07c97bc5236c706b760aac68bccd0ca654706038c0998a8e6644aacd230f1aed` |
| `pkg/secret-envelope` | `9da24a10e749977837aa349fdb4bc537f9600daaac19ab3bea9ff87f67b26594` | `9b54688939328b4820ce0c8df75815853ce1b83474ba6d838744a0150f0aab90` |
| `pkg/service` | `77324c994d5f6118bc3c3947e4acabf8b90c25be27d13f20736fcf487062e624` | `8fe06675f07c6117c50e92c927262a04d92beb9ea03a5d52c9a9b382cc95a312` |
| `pkg/telemetry` | `ecbd2dec674a79dc20426cad333f781fabde06ad64c60cd657d629a69f04c6d4` | `1f2de50f7d920318b56ea8bfa3630a27777e3376dcf4c102b3550700b46bc8cc` |
| `pkg/temporal` | `7b3e74373eb667f8c60a88c58015ddb94cb45453a4a9b904972263b5872e65ba` | `1b92b71e6f9a18558652733bc64595819fc68382210cee82402182582690c543` |
| `pkg/tenancy` | `178aecce19f50fba4dda270fb6a2fa89c632c679694c3b3610985b2dccc5221e` | `b7bc35f35eb7f551326359ef57b207ffb34a35e9fdd0212473bfbbf8743ac25b` |
| `pkg/validation` | `48bb4752e7c0b1a1da0f440c8ed1b1089b75dc06ac5352863337168b09ccda19` | `a541baee8df5134abbd67157c45afd9ad9c081a5bac6c7dbd0c665a3c2b06100` |
| `pkg/webhook` | `9de763d19b2acb700ec7141fa667559ee11ce499fc773a0c0f9fca89a985831e` | `4170de093e88456afaff6007c4559cce4c194fe2b7bad2d34050c1c460606a7b` |

These exact one-way transitions preserve the retained campaign evidence across
the additive RabbitMQ Streams fixture and catalog integration. They do not claim
that either campaign was rerun, do not broaden its module or environment scope,
and do not authorize any future digest. The four new releasable modules require
their own package gates and clean-consumer proof.

Retained partial evidence is also bound to the exact module-input snapshot it
recorded. Adding a new reverse dependant or releasable module therefore exposes
an unproved current boundary without retroactively expanding historical
evidence or making the assurance register structurally invalid.
