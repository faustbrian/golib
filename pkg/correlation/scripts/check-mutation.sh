#!/usr/bin/env bash
set -euo pipefail

workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT
mkdir -p "$workspace/baseline"
tar --exclude=.git --exclude=coverage.out -cf - . |
	tar -xf - -C "$workspace/baseline"
mkdir -p "$workspace/identifier"
tar --exclude=.git -cf - -C ../identifier . |
	tar -xf - -C "$workspace/identifier"

run_mutant() {
	local name=$1 file=$2 from=$3 to=$4 package=$5
	local mutant="$workspace/$name"
	mkdir -p "$mutant/correlation" "$mutant/identifier"
	tar -cf - -C "$workspace/baseline" . |
		tar -xf - -C "$mutant/correlation"
	tar -cf - -C "$workspace/identifier" . |
		tar -xf - -C "$mutant/identifier"
	FROM="$from" TO="$to" perl -0pi -e '
$changed = s/\Q$ENV{FROM}\E/$ENV{TO}/;
END { die "mutation source not found: $ENV{FROM}\n" unless $changed }
' "$mutant/correlation/$file"
	if (cd "$mutant/correlation" && go test -timeout=20s "$package" >mutation.log 2>&1); then
		echo "survived mutation: $name" >&2
		cat "$mutant/correlation/mutation.log" >&2
		exit 1
	fi
	printf 'killed mutation: %s\n' "$name"
}

run_mutant trust_correlation generation.go \
	'policy.TrustCorrelation && inbound.CorrelationID != ""' \
	'!policy.TrustCorrelation && inbound.CorrelationID != ""' .
run_mutant trust_causation generation.go \
	'policy.TrustRequestAsCausation && inbound.RequestID != ""' \
	'!policy.TrustRequestAsCausation && inbound.RequestID != ""' .
run_mutant carrier_conflict carrier.go 'value != first' 'value == first' .
run_mutant carrier_overwrite carrier.go \
	'len(carrier.Values(field.name)) != 0' \
	'len(carrier.Values(field.name)) == 0' .
run_mutant proxy_trust http/http.go \
	'middleware.options.Trust != nil && middleware.options.Trust(request)' \
	'middleware.options.Trust == nil || middleware.options.Trust(request)' ./http
run_mutant deterministic_version deterministic.go \
	'binary.BigEndian.PutUint32(encoded[:], strategy.version)' \
	'binary.BigEndian.PutUint32(encoded[:], strategy.version+1)' .
run_mutant disclosure_default disclosure.go \
	'return "[redacted]", nil' 'return value, nil' ./log
run_mutant accept_fresh_request generation.go \
	'values := Values{CorrelationID: correlationID, RequestID: requestID}' \
	'values := Values{CorrelationID: correlationID, RequestID: inbound.RequestID}' .
run_mutant next_immediate_causation generation.go \
	'CausationID:   causationID,' 'CausationID:   parent.CausationID,' .
run_mutant jsonrpc_custom_fields jsonrpc/jsonrpc.go \
	'for _, field := range adapter.fields' \
	'for _, field := range [3]string{CorrelationField, RequestField, CausationField}' ./jsonrpc
run_mutant jsonrpc_value_bound jsonrpc/jsonrpc.go \
	'if len(metadata[field]) > maxMetadataValues' \
	'if len(metadata[field]) >= maxMetadataValues' ./jsonrpc
run_mutant http_value_bound http/http.go \
	'if len(values) > maxHeaderValues' 'if len(values) >= maxHeaderValues' ./http

echo 'mutation score: 12/12 killed (100.0%)'
