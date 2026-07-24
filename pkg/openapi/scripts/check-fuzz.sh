#!/bin/sh
set -eu

fuzz_time=${FUZZ_TIME:-2s}
fuzz_parallel=${FUZZ_PARALLEL:-2}

case "$fuzz_parallel" in
	''|*[!0-9]*)
		printf 'FUZZ_PARALLEL must be a positive integer\n' >&2
		exit 2
		;;
esac
test "$fuzz_parallel" -ge 1 || {
	printf 'FUZZ_PARALLEL must be a positive integer\n' >&2
	exit 2
}

run_fuzz() {
	go test "$1" -run '^$' -parallel "$fuzz_parallel" \
		-fuzz "^${2}$" -fuzztime="$fuzz_time"
}

run_fuzz ./parse FuzzJSONParserDeterminism
run_fuzz ./parse FuzzYAMLParserProducesJSONSemantics
run_fuzz ./jsonvalue FuzzValueMarshalJSONWithLimits
run_fuzz ./jsonschema FuzzSchemaObjectCompilationAndEvaluation
run_fuzz ./media FuzzServerSentEventParsingDeterminism
run_fuzz ./internal/quality/cmd/mutationgate FuzzMutationReportDecoder
run_fuzz ./internal/specification/cmd/provenance FuzzProvenanceManifestDecoder
run_fuzz ./internal/specification/cmd/specmatrix FuzzSpecmatrixManifestDecoder
run_fuzz ./internal/modelgen/cmd/modelgen FuzzModelgenFieldInventoryDecoder
run_fuzz ./reference FuzzPointerCanonicalRoundTrip
run_fuzz ./reference FuzzFragmentClassificationIsStable
run_fuzz ./reference FuzzBundleInternalDocumentsIsIdentity
run_fuzz ./reference FuzzDereferenceObjectsIsDeterministic
run_fuzz ./reference FuzzFileResolverIdentifierBoundary
run_fuzz ./reference FuzzHTTPResolverResponseBoundary
run_fuzz ./expression FuzzRuntimeExpressionParse
run_fuzz ./expression FuzzRuntimeExpressionTemplateParse
run_fuzz ./parameter FuzzOpenAPI32QueryDecode
run_fuzz ./parameter FuzzSwagger20QueryDecode
run_fuzz ./diff FuzzOperationDiffDeterminism
run_fuzz ./compose FuzzFilterOperationsKeepAllIsIdentity
run_fuzz ./compose FuzzMergeIdenticalDocumentsIsIdentity
run_fuzz ./convert FuzzConvertPatchAndForwardVersions
run_fuzz ./convert FuzzConvertOpenAPI31To30
run_fuzz ./convert FuzzConvertOpenAPI30ToSwagger20
run_fuzz ./serialize FuzzJSONAndYAMLSemanticRoundTrip
run_fuzz ./validate FuzzDocumentValidationDeterminism
