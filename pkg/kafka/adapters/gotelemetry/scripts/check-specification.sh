#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${module_root}/specification/manifest.json"
version="$({
    awk -F '"' '/MessagingSemanticConventionVersion =/ { print $2 }' \
        "${module_root}/instrumentation.go"
})"

[[ "${version}" == "1.44.0" ]]

jq -e --arg version "v${version}" '
    .schema == 1 and
    .reviewed_at == "2026-08-10" and
    (.sources | length) == 2 and
    .sources[0].name == "OpenTelemetry Semantic Conventions" and
    .sources[0].repository == "https://github.com/open-telemetry/semantic-conventions" and
    .sources[0].version == $version and
    .sources[0].revision == "e10a930844c6951757a43b849d364f7d056ac32b" and
    .sources[0].license == "Apache-2.0" and
    .sources[0].status == "Development" and
    .sources[0].documents == [
        {
            "path": "docs/messaging/kafka.md",
            "sha256": "ecdd39f4c01ae4b0e1725ceff885404d42e58c162dfc622c8f719fd76c8e16d4"
        },
        {
            "path": "docs/messaging/messaging-metrics.md",
            "sha256": "0010f8dc0ab6af531a11a6a2bd2d2fbb908d58f7394262e522bd6bcbe2e37861"
        },
        {
            "path": "docs/messaging/messaging-spans.md",
            "sha256": "4200bc249cf520930412785bc0c52c1e96d6db86be29fba8dbb0445690455046"
        }
    ] and
    .sources[1].name == "OpenTelemetry Go" and
    .sources[1].repository == "https://github.com/open-telemetry/opentelemetry-go" and
    .sources[1].version == $version and
    .sources[1].tag_revision == "7318f81507f41cce02ada9406ea62e0d8b13958a" and
    .sources[1].revision == "b62d92831b2dd142f5a0cc89c828270274196877" and
    .sources[1].license == "Apache-2.0" and
    .adapter_decisions.producer_send_span_kind == "NONE" and
    .adapter_decisions.poll_receive_span_kind == "NONE" and
    .adapter_decisions.consumer_process_span_kind == "NONE" and
    .adapter_decisions.metric_identity_policy != "" and
    .adapter_decisions.provider_boundary != ""
' "${manifest}" >/dev/null

awk -v version="v${version}" '
    $1 ~ /^go.opentelemetry.io\/otel(\/|$)/ {
        count++
        if ($2 != version) {
            exit 1
        }
    }
    END {
        if (count != 5) {
            exit 1
        }
    }
' "${module_root}/go.mod"

rg -q -F "semantic conventions **${version}**" "${module_root}/README.md"
