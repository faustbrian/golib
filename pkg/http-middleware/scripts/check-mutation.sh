#!/usr/bin/env bash
set -euo pipefail

workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT
baseline="$workspace/baseline"
mkdir -p "$baseline"
tar --exclude=.git --exclude=coverage.out -cf - . | tar -xf - -C "$baseline"

run_mutant() {
	local name=$1 file=$2 from=$3 to=$4 package=$5
	local mutant="$workspace/$name"
	mkdir -p "$mutant"
	tar -cf - -C "$baseline" . | tar -xf - -C "$mutant"
	FROM="$from" TO="$to" perl -0pi -e '
$changed = s/\Q$ENV{FROM}\E/$ENV{TO}/;
END { die "mutation source not found: $ENV{FROM}\n" unless $changed }
' "$mutant/$file"
	if (cd "$mutant" && go test -timeout=5s "$package" >mutation.log 2>&1); then
		echo "survived mutation: $name" >&2
		cat "$mutant/mutation.log" >&2
		exit 1
	fi
	printf 'killed mutation: %s\n' "$name"
	rm -rf "$mutant"
}

run_mutant chain_depth chain.go 'len(middleware) > MaxChainDepth' 'len(middleware) < MaxChainDepth' .
run_mutant duplicate chain.go '!previous.allowDuplicate || !descriptor.allowDuplicate' '!previous.allowDuplicate && !descriptor.allowDuplicate' .
run_mutant condition chain.go 'if predicate(r) {' 'if !predicate(r) {' .
run_mutant condition_nil chain.go 'if wrapped == nil {' 'if wrapped != nil {' .
run_mutant duplicate_after chain.go 'lastPositions[target]' 'positions[target]' .
run_mutant request_id_trust requestid/requestid.go 'policy.TrustInbound && len(values) == 1' '!policy.TrustInbound && len(values) == 1' ./requestid
run_mutant request_id_control requestid/requestid.go 'char > 0x7e' 'char < 0x7e' ./requestid
run_mutant body_known_limit bodylimit/bodylimit.go 'r.ContentLength > policy.MaxBytes' 'r.ContentLength < policy.MaxBytes' ./bodylimit
run_mutant body_streaming_limit bodylimit/bodylimit.go 'overflow.Load() && !response.Committed' '!overflow.Load() && !response.Committed' ./bodylimit
run_mutant timeout_bound deadline/timeout.go 'w.payload.Len()+len(payload) > w.maximum' 'w.payload.Len()+len(payload) < w.maximum' ./deadline
run_mutant proxy_trust proxy/proxy.go 'if isTrusted(info.ClientIP, trusted) {' 'if !isTrusted(info.ClientIP, trusted) {' ./proxy
run_mutant proxy_client proxy/proxy.go 'index >= 0 && isTrusted(current, trusted)' 'index >= 0 && !isTrusted(current, trusted)' ./proxy
run_mutant proxy_prefix_limit proxy/proxy.go 'len(policy.Trusted) > maximumTrustedPrefixes' 'len(policy.Trusted) < maximumTrustedPrefixes' ./proxy
run_mutant proxy_parameter proxy/proxy.go '!validParameterName(key)' 'validParameterName(key)' ./proxy
run_mutant proxy_duplicate_parameter proxy/proxy.go 'seen[key]' '!seen[key]' ./proxy
run_mutant cors_origin cors/cors.go 'if configuration.origins[origin] {' 'if !configuration.origins[origin] {' ./cors
run_mutant cors_method cors/cors.go '!c.methodsWildcard && !c.methods[method]' '!c.methodsWildcard && c.methods[method]' ./cors
run_mutant cors_method_token cors/cors.go '!present || !valid || !validToken(methodValue)' '!present || !valid' ./cors
run_mutant cors_method_case cors/cors.go 'method := methodValue' 'method := strings.ToUpper(methodValue)' ./cors
run_mutant cors_origin_port cors/cors.go 'strings.HasSuffix(parsed.Host, ":")' 'strings.HasPrefix(parsed.Host, ":")' ./cors
run_mutant hsts_ack secureheader/secureheader.go 'policy.HSTS != "" && !policy.AcknowledgeHSTS' 'policy.HSTS != "" && policy.AcknowledgeHSTS' ./secureheader
run_mutant coding_quality compress/compress.go 'gzipQ <= 0 || gzipQ < identityQ' 'gzipQ <= 0 && gzipQ < identityQ' ./compress
run_mutant compression_head compress/compress.go 'r.Method == http.MethodHead' 'r.Method != http.MethodHead' ./compress
run_mutant compression_no_content compress/compress.go 'status == http.StatusNoContent' 'status != http.StatusNoContent' ./compress
run_mutant compression_not_modified compress/compress.go 'status == http.StatusNotModified' 'status != http.StatusNotModified' ./compress
run_mutant compression_request_range compress/compress.go 'r.Header.Get("Range") != ""' 'r.Header.Get("Range") == ""' ./compress
run_mutant compression_response_range compress/compress.go 'header.Get("Content-Range") != ""' 'header.Get("Content-Range") == ""' ./compress
run_mutant compression_existing_coding compress/compress.go 'header.Get("Content-Encoding") != ""' 'header.Get("Content-Encoding") == ""' ./compress
run_mutant compression_no_transform compress/compress.go 'strings.Contains(strings.ToLower(header.Get("Cache-Control")), "no-transform")' '!strings.Contains(strings.ToLower(header.Get("Cache-Control")), "no-transform")' ./compress
run_mutant compression_size compress/compress.go 'size < minimum' 'size > minimum' ./compress
run_mutant compression_excluded_type compress/compress.go 'strings.EqualFold(mediaType, excludedType)' '!strings.EqualFold(mediaType, excludedType)' ./compress
run_mutant compression_spill compress/compress.go 'w.compression != nil && shouldCompressSize' 'w.compression != nil && !shouldCompressSize' ./compress
run_mutant compression_exclusion_limit compress/compress.go 'len(value) > 256' 'len(value) < 256' ./compress
run_mutant compression_vary compress/compress.go 'httpx.AddVary(w.Header(), "Accept-Encoding")' 'w.Header().Del("Vary")' ./compress
run_mutant compression_stream_close compress/compress.go '_ = w.encoder.Close()' '_ = w.encoder.Flush()' ./compress
run_mutant compression_buffer_close compress/compress.go '_ = encoder.Close()' '_ = encoder.Flush()' ./compress
run_mutant compression_digest_trailer compress/compress.go 'w.compressed && representationHeader(name)' 'w.compressed && !representationHeader(name)' ./compress
run_mutant accept_quality content/content.go 'q > 0 && matchesAny' 'q < 0 && matchesAny' ./content
run_mutant content_configuration_limit content/content.go 'len(policy.RequestTypes) > policy.MaxValues' 'len(policy.RequestTypes) < policy.MaxValues' ./content
run_mutant content_wildcard content/content.go 'major == "*" && minor == "*"' 'major == "*" && minor != "*"' ./content
run_mutant content_type_singular content/content.go 'len(values) != 1' 'len(values) == 1' ./content
run_mutant content_match content/content.go 'matched = true' 'matched = false' ./content
run_mutant admission_limit admission/admission.go 'policy.MaxInFlight < 1' 'policy.MaxInFlight > 1' ./admission
run_mutant admission_wait admission/admission.go 'policy.Wait > maximumWait' 'policy.Wait < maximumWait' ./admission
run_mutant admission_cancellation admission/admission.go 'r.Context().Err() != nil' 'r.Context().Err() == nil' ./admission
run_mutant deadline_maximum deadline/deadline.go 'policy.Timeout > maximumTimeout' 'policy.Timeout < maximumTimeout' ./deadline
run_mutant timeout_concurrency deadline/timeout.go 'policy.MaxConcurrent > 65_536' 'policy.MaxConcurrent < 65_536' ./deadline
run_mutant timeout_informational deadline/timeout.go 'status != http.StatusSwitchingProtocols' 'status == http.StatusSwitchingProtocols' ./deadline
run_mutant compression_switch compress/compress.go 'status == http.StatusSwitchingProtocols' 'status != http.StatusSwitchingProtocols' ./compress
run_mutant status_code internal/httpx/response.go 'status < 100 || status > 999' 'status < 100 && status > 999' ./internal/httpx
run_mutant no_store responsepolicy/responsepolicy.go 'header.Set("Cache-Control", "no-store")' 'header.Set("Cache-Control", "public")' ./responsepolicy
run_mutant panic_commit recovery/recovery.go 'if !recorder.Committed {' 'if recorder.Committed {' ./recovery
run_mutant observer_panic observe/observe.go 'if panicValue != nil {' 'if panicValue == nil {' ./observe
run_mutant observer_duration observe/observe.go 'duration < 0' 'duration > 0' ./observe
run_mutant observer_method observe/observe.go 'return "OTHER"' 'return "CUSTOM"' ./observe
run_mutant observer_metadata observe/observe.go 'metadata(policy.Route, r, 128)' 'metadata(nil, r, 128)' ./observe
run_mutant observer_metadata_panic observe/observe.go 'defer func() { _ = recover() }()' 'defer func() {}()' ./observe
run_mutant observer_recorded_route observe/observe.go 'routeName := route.load()' 'routeName := ""' ./observe
run_mutant observer_route_bound observe/observe.go 'state.value = bounded(route, 128)' 'state.value = route' ./observe

echo 'mutation score: 59/59 killed (100.0%)'
