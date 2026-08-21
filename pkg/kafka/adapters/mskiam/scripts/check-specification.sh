#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${module_root}/specification/manifest.json"

jq -e '
    .schema == 1 and
    .reviewed_at == "2026-08-11" and
    (.sources | length) == 4 and
    .sources[0].version == "v1.0.4" and
    .sources[0].revision == "53637de1b411b2a2c8b2ccb8f103fc1d6b761c07" and
    .sources[0].sha256 == "915687aa464cfb3fc9a184de94d990be4f8924fecbbb8161043fb212051ed286" and
    .sources[1].version == "v1.43.0" and
    .sources[1].revision == "4fef3455fe2dcb5ea3de4e9fbacf889b84c8a255" and
    .sources[1].sha256 == "c04036c140e8088ad06e25a6ecff16cc14b0a76f37fb7d88d680e03b1f1e755d" and
    .sources[2].retrieved_at == "2026-08-19" and
    .sources[2].snapshot_sha256 == "f9fc6f3101baa42aad36740eb8d4729420ff2a70fa223f40f85df46da035879d" and
    .sources[3].retrieved_at == "2026-08-19" and
    .sources[3].snapshot_sha256 == "bd806ae3f9af1fdd892b478e0c72fc38014c4e7eccd54d84eab0fb91509286d6"
' "${manifest}" >/dev/null

grep -qF -- 'aws-msk-iam-sasl-signer-go' "${module_root}/README.md"
grep -qF -- 'docs/specification-decisions.md' "${module_root}/README.md"

GOWORK=off go test -count=1 -run \
    '^(TestProviderGeneratesOwnedExpiringMSKIAMToken|TestTokenRejectsInvalidSignerResults|TestProviderRefreshesExpiringCredentialsAndCapsTokenExpiry|TestMSKCompatibilityConfigRejectsUnboundedInputs|TestMSKControlPlaneValidation)$' \
    "${module_root}"
