#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
spec_repo=https://raw.githubusercontent.com/OAI/OpenAPI-Specification
site_repo=https://raw.githubusercontent.com/OAI/spec.openapis.org
petstore_repo=https://raw.githubusercontent.com/swagger-api/swagger-petstore
github_rest_repo=https://raw.githubusercontent.com/github/rest-api-description

sync_file() {
    repository=$1
    revision=$2
    source_file=$3
    destination=$4
    expected=$5

    destination_path="$root_dir/specification/$destination"
    temporary_path="$destination_path.tmp"

    mkdir -p "$(dirname -- "$destination_path")"
    curl --proto '=https' --tlsv1.2 --location --fail --silent --show-error \
        "$repository/$revision/$source_file" --output "$temporary_path"

    actual=$(shasum -a 256 "$temporary_path" | awk '{print $1}')
    if [ "$actual" != "$expected" ]; then
        rm -f "$temporary_path"
        echo "checksum mismatch for $destination" >&2
        exit 1
    fi

    mv "$temporary_path" "$destination_path"
}

sync_url() {
    source_url=$1
    destination=$2
    expected=$3

    destination_path="$root_dir/specification/$destination"
    temporary_path="$destination_path.tmp"

    mkdir -p "$(dirname -- "$destination_path")"
    curl --proto '=https' --tlsv1.2 --location --fail --silent --show-error \
        "$source_url" --output "$temporary_path"

    actual=$(shasum -a 256 "$temporary_path" | awk '{print $1}')
    if [ "$actual" != "$expected" ]; then
        rm -f "$temporary_path"
        echo "checksum mismatch for $destination" >&2
        exit 1
    fi

    mv "$temporary_path" "$destination_path"
}

spec20=8e166da300c64bb7144938fec518b8e3f4cae715
spec30=b8953109f2eb4d9eebcc7f702f70456b2e074567
spec31=82603363df271c104c8f527f0fe641ea67da93fd
spec32=99710bcb26cbe4be646565eebeb04348f02374b5
site=121b0101ea4728d96149143ca784920d3a36ab54
petstore=8f0dd286987880b4af7bce552aca3813166f3049
github_rest=417c4fb368fc6a7162ce5f3eeeddce1a9a217747

sync_file "$spec_repo" "$spec32" LICENSE licenses/OpenAPI-Specification-LICENSE 4948367c65e1ce06690e2cadc6e86fce1a6a6db55ef874ce4b78c0f472ce5f13
sync_file "$spec_repo" "$spec20" versions/2.0.md oas/2.0/2.0.md 57bf00dfabdb79634e2886e022f3c6195296682b237ed4a56c7fc631c85b3c91
sync_file "$spec_repo" "$spec30" versions/3.0.0.md oas/3.0/3.0.0.md b55a1df4501cd095cfc86878e86dfffd215d4ad36fc6764b84cf8ec6b3976620
sync_file "$spec_repo" "$spec30" versions/3.0.1.md oas/3.0/3.0.1.md 9e120058b65e79288f7920271f18d745943e10866f00f0fbfa54190660da343f
sync_file "$spec_repo" "$spec30" versions/3.0.2.md oas/3.0/3.0.2.md 504a61acec0c22beaab2942d15a055afadb4589e56cf9512e8e8b0bf8fcf9b77
sync_file "$spec_repo" "$spec30" versions/3.0.3.md oas/3.0/3.0.3.md 8ccccaab5172a41ac1270fd0f0b40940e783134e0685ef3d41c63b62571feb29
sync_file "$spec_repo" "$spec30" versions/3.0.4.md oas/3.0/3.0.4.md 77af0760571a228e61ca2126d6f4de9efcf053d5dcfb7f0ad2f84d7ecf622ff6
sync_file "$spec_repo" "$spec31" versions/3.1.0.md oas/3.1/3.1.0.md ee99bcc50c7610f4876ce77b2f746036d4095e0909968bb6839259f955bac022
sync_file "$spec_repo" "$spec31" versions/3.1.1.md oas/3.1/3.1.1.md a612d2b72d422925616b2cabfa7acc0419ffaaf4b0eb42bfb11dc9b804f92749
sync_file "$spec_repo" "$spec31" versions/3.1.2.md oas/3.1/3.1.2.md 7e30126d65acad53523f420e522d34edb3717af225aae727e73a522abc227da7
sync_file "$spec_repo" "$spec32" versions/3.2.0.md oas/3.2/3.2.0.md 936c11e5f37fd0cf7cbb3c3a8bf5ef4b2ee5c03fbec0eb900ae546fc0a696503

sync_file "$site_repo" "$site" oas/2.0/schema/2017-08-27 schemas/2.0/2017-08-27.json b36871c8016292c5e66dd3b203e69aeff98bfef97e0b3c67c1909036095586a5
sync_file "$site_repo" "$site" oas/3.0/schema/2024-10-18 schemas/3.0/2024-10-18.json 2385f5bbb8c37878daae73baeabe7f34b2f022a4a8c049329ee61f71796f039c
sync_file "$site_repo" "$site" oas/3.1/schema/2025-11-23 schemas/3.1/schema-2025-11-23.json 1b8ccc6e34234b17536f2dd0eb3597142a32bd108438cd42471a5fca4c1a07ef
sync_file "$site_repo" "$site" oas/3.1/schema-base/2025-11-23 schemas/3.1/schema-base-2025-11-23.json ab0bde84a429f43f280624f645a19d05c90b626e63862d7278074a465a20ff54
sync_file "$site_repo" "$site" oas/3.1/dialect/2024-11-10 schemas/3.1/dialect-2024-11-10.json 647f32dfff64949d5020a28ecd1af4ffeffb1e9c695f861a52255a8004e07460
sync_file "$site_repo" "$site" oas/3.1/meta/2024-11-10 schemas/3.1/meta-2024-11-10.json 80706a9a404affedbf84ac4dc1328c9ce0d2a00804cdfc4d95c0ddd0053121dd
sync_file "$site_repo" "$site" oas/3.2/schema/2025-11-23 schemas/3.2/schema-2025-11-23.json 7d48f01f37eeae4799041b371ad5f533f9f533fd2b0caa1011a8ba27c5b48b70
sync_file "$site_repo" "$site" oas/3.2/schema-base/2025-11-23 schemas/3.2/schema-base-2025-11-23.json 423daa88e2285fa343856c08502fe63fd8aa3674cd5b4ef88746ba6f82647af3
sync_file "$site_repo" "$site" oas/3.2/dialect/2025-09-17 schemas/3.2/dialect-2025-09-17.json 4e2c989f3d1e6489d41bc1ca4ade11743278c612b82fba9144c6116c79f1c273
sync_file "$site_repo" "$site" oas/3.2/meta/2025-09-17 schemas/3.2/meta-2025-09-17.json a1959c0aa1f9a7ce58f2b75699be5bc5187c8a25901fe04a227f1d9419ba4e9c

sync_file "$site_repo" "$site" registry/alternative-schema.md registries/alternative-schema.md acc1af468e58da669ec2cff5816c89ebfcd515b27c68710080169ec7d1f38320
sync_file "$site_repo" "$site" registry/draft-feature.md registries/draft-feature.md 01169ba2f4b5a0238787d004710ce3518936bf9917c0b833dcdc58b2af10a7fd
sync_file "$site_repo" "$site" registry/extension.md registries/extension.md 7743424e240b5a9a32f6ecafaa9ab1811039730ed26177007310db16e4d75026
sync_file "$site_repo" "$site" registry/format.md registries/format.md 0cfa5e6504998d22219d8f413fbf80d0e8f3b66ec89d39ee0a2001661ba32b1a
sync_file "$site_repo" "$site" registry/index.md registries/index.md a7eafe7594fab6708599a4e5d4706d8e94c915c965e34f9e938b105a06013166
sync_file "$site_repo" "$site" registry/media-type.md registries/media-type.md 21f5b064fe69291bbeb7ebb5fc6bce610667d5854530ef4f58be0f0ab073a7f3
sync_file "$site_repo" "$site" registry/namespace.md registries/namespace.md 79f73713bad80d4f5eb07df8fad23cf85b302626291da5ea397a6929c18e1366
sync_file "$site_repo" "$site" registry/tag-kind.md registries/tag-kind.md 02993a39f113d8ad049e95963a4937de8aa7d5282a5b44dc248567d78af798da

sync_file "$petstore_repo" "$petstore" LICENSE independent/swagger-petstore/LICENSE b40930bbcf80744c86c46a12bc9da056641d722716c378f5659b9e555ef833e1
sync_file "$petstore_repo" "$petstore" src/main/resources/openapi.yaml independent/swagger-petstore/openapi.yaml 0d810997f6409d5cff6f0cf2c1466814ba52250a784cd841cacb93514c7a8502
sync_file "$github_rest_repo" "$github_rest" LICENSE.md independent/github-rest-api/LICENSE.md 3243761cbac07e6d169a5a2f4e7c25cc544da85248e735df74c3672e055cc87b
sync_file "$github_rest_repo" "$github_rest" descriptions-next/api.github.com/api.github.com.2022-11-28.json independent/github-rest-api/api.github.com.2022-11-28.json 9d85f3a842c0215768f30f83ac7d1595430236fc51ce9c84e344b991a9f6b3da

sync_url https://www.iana.org/assignments/http-status-codes/http-status-codes-1.csv registries/iana/http-status-codes-1.csv 4a9550d4b4ae49cf41cf9050cf9b56b0d6082ad1edfc0e2b09b07f251a36d7a4
sync_url https://www.iana.org/assignments/http-authschemes/authschemes.csv registries/iana/authschemes.csv 9624ab05b6d91d0658f16e082699b8d68a09d16d06c9c030409bebe92cc58347
sync_url https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv registries/iana/iana-ipv4-special-registry-1.csv e3e39e76d00b1677335db8e9a805c7b9480ea2f4dc9e33f0b93cd3a905128d73
sync_url https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv registries/iana/iana-ipv6-special-registry-1.csv 775feea0621dec8735a44fbf30f762e721e8f0a1b3ab7eb341961a88cfce2139
