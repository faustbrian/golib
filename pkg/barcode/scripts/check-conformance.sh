#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
register="${root}/docs/specification-decisions.md"
manifest="${root}/specification/manifest.json"

[[ -s "${register}" && -s "${manifest}" ]] || {
	printf 'missing barcode specification governance\n' >&2
	exit 1
}

[[ "$(grep -Ec '^## BARCODE-DEC-[0-9]{3}:' "${register}")" -eq 16 ]] || {
	printf 'barcode decision register must contain 16 decisions\n' >&2
	exit 1
}

cd "${root}"
./scripts/check-docs.sh
GOWORK=off go test ./... -run \
	'^(TestManifestProvenanceIsWellFormed|TestCapabilityMetadataIdentifiesExactGoverningEditions|TestCapabilitiesReflectSoftwareScope|TestEncodeHonorsMaskECIAndGS1Controls|TestEncodeGS1AcceptsValidatedStructuredElements|TestEncodeSupportsStructuredAppendBoundaries|TestEncodeSupportsECIAndMacroControlBlocks|TestEncodeSupportsAutomaticAndForcedLayers|TestDecodeEnforcesBoundsBeforeImageAllocation|TestDecodeQRDocumentedImageDegradationThresholds|TestInvalidInputErrorsAreClassifiedAndPayloadRedacted|TestDecodeIndependentWriters|TestSymbolsDecodeWithIndependentReaders|TestRenderFixtureGoldensCoverEveryFormat)$' \
	-count=1
