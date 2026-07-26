#!/usr/bin/env bash
set -euo pipefail

workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT
baseline="$workspace/baseline"
mkdir -p "$baseline"
tar --exclude=.git --exclude=coverage.out -cf - . | tar -xf - -C "$baseline"

run_mutant() {
	local name=$1
	local file=$2
	local from=$3
	local to=$4
	local package=$5
	local mutant="$workspace/$name"

	mkdir -p "$mutant"
	tar -cf - -C "$baseline" . | tar -xf - -C "$mutant"
	FROM="$from" TO="$to" perl -0pi -e '
$changed = s/\Q$ENV{FROM}\E/$ENV{TO}/;
END { die "mutation source not found: $ENV{FROM}\n" unless $changed }
' "$mutant/$file"
	if (cd "$mutant" && go test "$package" >mutation.log 2>&1); then
		echo "survived mutation: $name" >&2
		cat "$mutant/mutation.log" >&2
		exit 1
	fi
	printf 'killed mutation: %s\n' "$name"
	rm -rf "$mutant"
}

run_mutant required rules/presence.go \
	'value.IsPresent() && !value.IsEmpty()' \
	'value.IsPresent() || !value.IsEmpty()' ./rules
run_mutant present rules/presence.go \
	'if value.IsPresent() {' 'if false {' ./rules
run_mutant omitted_code rules/presence.go \
	'return state[T]("omitted", validation.MissingState)' \
	'return state[T]("prohibited", validation.MissingState)' ./rules
run_mutant prohibited_code rules/presence.go \
	'return state[T]("prohibited", validation.MissingState)' \
	'return state[T]("omitted", validation.MissingState)' ./rules
run_mutant empty rules/presence.go \
	'value.IsPresent() && value.IsEmpty()' \
	'value.IsPresent() || value.IsEmpty()' ./rules
run_mutant zero_value rules/presence.go \
	'if value.IsZero() {' 'if false {' ./rules
run_mutant presence_state rules/presence.go \
	'value.Presence() == want' 'value.Presence() != want' ./rules
run_mutant range_lower rules/numeric.go \
	'value >= minimum && value <= maximum' \
	'value > minimum && value <= maximum' ./rules
run_mutant range_upper rules/numeric.go \
	'value >= minimum && value <= maximum' \
	'value >= minimum && value < maximum' ./rules
run_mutant greater_than rules/numeric.go \
	'value > boundary' 'value >= boundary' ./rules
run_mutant less_than rules/numeric.go \
	'value < boundary' 'value <= boundary' ./rules
run_mutant finite rules/numeric.go \
	'!math.IsNaN(value) && !math.IsInf(value, 0)' \
	'!math.IsNaN(value) || !math.IsInf(value, 0)' ./rules
run_mutant precision_nonnegative rules/numeric.go \
	'decimalPlaces >= 0' 'decimalPlaces > 0' ./rules
run_mutant multiple_finite rules/numeric.go \
	'!math.IsNaN(quotient)' 'math.IsNaN(quotient)' ./rules
run_mutant multiple_positive rules/numeric.go \
	'divisor > 0' 'divisor < 0' ./rules
run_mutant string_length_lower rules/string.go \
	'actual >= minimum && actual <= maximum' \
	'actual > minimum && actual <= maximum' ./rules
run_mutant string_length_upper rules/string.go \
	'actual >= minimum && actual <= maximum' \
	'actual >= minimum && actual < maximum' ./rules
run_mutant pattern rules/string.go \
	'if compiled.MatchString(value) {' 'if false {' ./rules
run_mutant prefix rules/string.go \
	'if strings.HasPrefix(value, prefix) {' 'if false {' ./rules
run_mutant suffix rules/string.go \
	'if strings.HasSuffix(value, suffix) {' 'if false {' ./rules
run_mutant one_of rules/string.go \
	'if _, ok := allowed[value]; ok {' 'if _, ok := allowed[value]; !ok {' ./rules
run_mutant primitive_predicate rules/primitives.go \
	'if predicate(value) {' 'if false {' ./rules
run_mutant slice_size_lower rules/collection.go \
	'len(values) >= minimum && len(values) <= maximum' \
	'len(values) > minimum && len(values) <= maximum' ./rules
run_mutant slice_size_upper rules/collection.go \
	'len(values) >= minimum && len(values) <= maximum' \
	'len(values) >= minimum && len(values) < maximum' ./rules
run_mutant map_size_lower rules/collection.go \
	'size >= minimum && size <= maximum' \
	'size > minimum && size <= maximum' ./rules
run_mutant map_size_upper rules/collection.go \
	'size >= minimum && size <= maximum' \
	'size >= minimum && size < maximum' ./rules
run_mutant field_equal rules/crossfield.go \
	'left(value) == right(value)' 'left(value) != right(value)' ./rules
run_mutant field_order rules/crossfield.go \
	'left(value) <= right(value)' 'left(value) < right(value)' ./rules
run_mutant required_when rules/crossfield.go \
	'if !condition(value) {' 'if condition(value) {' ./rules
run_mutant excluded_when rules/crossfield.go \
	'!condition(value) || accessor(value).Presence() == validation.MissingState' \
	'!condition(value) && accessor(value).Presence() == validation.MissingState' ./rules
run_mutant nested_path rules/crossfield.go \
	'ctx.WithPath(validation.Field(field)), accessor(value)' \
	'ctx.WithPath(validation.Item()), accessor(value)' ./rules
run_mutant time_lower rules/temporal.go \
	'!value.Before(minimum) && !value.After(maximum)' \
	'value.Before(minimum) && !value.After(maximum)' ./rules
run_mutant time_upper rules/temporal.go \
	'!value.Before(minimum) && !value.After(maximum)' \
	'!value.Before(minimum) && value.After(maximum)' ./rules
run_mutant before rules/temporal.go \
	'if value.Before(boundary) {' 'if false {' ./rules
run_mutant after rules/temporal.go \
	'if value.After(boundary) {' 'if false {' ./rules
run_mutant future rules/temporal.go \
	'if value.After(clock.Now()) {' 'if false {' ./rules
run_mutant past rules/temporal.go \
	'if value.Before(clock.Now()) {' 'if false {' ./rules
run_mutant date rules/temporal.go \
	'if _, err := time.Parse(layout, value); err == nil {' \
	'if _, err := time.Parse(layout, value); err != nil {' ./rules
run_mutant interval rules/temporal.go \
	'if !value.End.Before(value.Start) {' \
	'if value.End.Before(value.Start) {' ./rules
run_mutant all composition.go \
	'mode == ShortCircuit && current.Err() != nil' \
	'mode == CollectAll && current.Err() != nil' .
run_mutant any composition.go \
	'current.Err() == nil' 'current.Err() != nil' .
run_mutant any_advisories composition.go \
	'successes = successes.Merge(current)' 'successes = successes' .
run_mutant not composition.go \
	'validator.Validate(ctx, value).Err() != nil' \
	'validator.Validate(ctx, value).Err() == nil' .
run_mutant when composition.go \
	'predicate != nil && predicate(value)' \
	'predicate != nil && !predicate(value)' .
run_mutant dependent composition.go \
	'report.Err() != nil' 'report.Err() == nil' .
run_mutant dependent_advisories composition.go \
	'return report.Merge(dependent.Validate(ctx, value))' \
	'return dependent.Validate(ctx, value)' .
run_mutant validator_panic validator.go \
	'if recover() != nil {' 'if false {' .
run_mutant async_validator_panic async.go \
	'if recover() != nil {' 'if false {' .
run_mutant async_all_panic async.go \
	'IsolateAsyncPanics(current.validator).
					ValidateAsync(ctx, validationContext, value)' \
	'current.validator.ValidateAsync(ctx, validationContext, value)' .
run_mutant typed_string_limit rules/helpers.go \
	'len(value) <= ctx.Limits().MaxStringLength' \
	'len(value) < ctx.Limits().MaxStringLength' ./rules
run_mutant generic_string_limit rules/helpers.go \
	'return reflect.TypeFor[T]().Kind() == reflect.String' \
	'return false' ./rules
run_mutant report_blocking report.go \
	'violation.severity == Error' 'violation.severity == Warning' .
run_mutant report_diagnostic_gate report.go \
	'if !validDiagnostic(violation, r.limits) {' 'if false {' .
run_mutant report_path_limit report.go \
	'if violation.path.exceedsRenderedLength(r.limits.MaxPathLength) {' \
	'if false {' .
run_mutant path_limit_boundary path.go \
	'if length > remaining {' 'if length >= remaining {' .
run_mutant invalid_severity violation.go \
	'violation.severity != Error && violation.severity != Warning' \
	'false' .
run_mutant diagnostic_length violation.go \
	'value == "" || len(value) > maximum' \
	'value == "" && len(value) > maximum' .
run_mutant diagnostic_utf8 violation.go \
	'if !utf8.ValidString(value) {' 'if false {' .
run_mutant diagnostic_control violation.go \
	"if character < ' ' || character == 0x7f {" 'if false {' .
run_mutant report_path_identity report.go \
	'violation.path.identity()' 'violation.path.String()' .
run_mutant report_parameter_identity report.go \
	'writeIdentityPart(&result, key)
		writeIdentityPart(&result, violation.parameters[key])' \
	'result.WriteString(key)
		result.WriteString(violation.parameters[key])' .
run_mutant report_merge_truncation report.go \
	'if other.truncated {' 'if false {' .
run_mutant report_merge_blocking report.go \
	'if other.hasErrors {' 'if false {' .
run_mutant reflective_required structplan/tags.go \
	'if reflectiveEmpty(value) {' 'if false {' ./structplan
run_mutant reflective_minimum structplan/tags.go \
	'return value >= bound' 'return value > bound' ./structplan
run_mutant reflective_maximum structplan/tags.go \
	'return value <= bound' 'return value < bound' ./structplan
run_mutant reflective_collection_limit structplan/tags.go \
	'measuredValue.Len() > ctx.Limits().MaxCollectionSize' \
	'measuredValue.Len() < ctx.Limits().MaxCollectionSize' ./structplan
run_mutant reflective_string_limit structplan/tags.go \
	'measuredValue.Len() > ctx.Limits().MaxStringLength' \
	'measuredValue.Len() < ctx.Limits().MaxStringLength' ./structplan
run_mutant reflective_field_count structplan/tags.go \
	'if *visited >= limits.MaxStructFields {' \
	'if *visited > limits.MaxStructFields {' ./structplan
run_mutant typed_plan_nil_builder structplan/plan.go \
	'builder == nil || name == ""' 'false || name == ""' ./structplan
run_mutant typed_plan_path structplan/plan.go \
	'validator.Validate(fieldContext, accessor(value))' \
	'validator.Validate(ctx, accessor(value))' ./structplan
run_mutant nil_cache structplan/cache.go \
	'if cache == nil {' 'if false {' ./structplan
run_mutant collection_unique rules/collection.go \
	'if _, exists := seen[value]; exists {' \
	'if _, exists := seen[value]; !exists {' ./rules
run_mutant collection_items rules/collection.go \
	'ctx.WithPath(validation.Index(index)), value' \
	'ctx.WithPath(validation.Index(index)), *new(T)' ./rules
run_mutant collection_keys rules/collection.go \
	'ctx.WithPath(validation.Key(fmt.Sprint(key))), key' \
	'ctx.WithPath(validation.Key(fmt.Sprint(key))), *new(K)' ./rules
run_mutant collection_values rules/collection.go \
	'ctx.WithPath(validation.Key(fmt.Sprint(key))), values[key],' \
	'ctx.WithPath(validation.Key(fmt.Sprint(key))), *new(V),' ./rules
run_mutant rpc_path validationrpc/rpc.go \
	'violation.Path().String()' 'violation.Path().JSONPointer()' \
	./validationrpc
run_mutant rpc_severity validationrpc/rpc.go \
	'value == validation.Warning' 'value == validation.Error' \
	./validationrpc
run_mutant rpc_truncation validationrpc/rpc.go \
	'Truncated: report.Truncated()' 'Truncated: false' ./validationrpc
run_mutant rpc_blocking validationrpc/rpc.go \
	'HasErrors: report.HasErrors()' 'HasErrors: false' ./validationrpc
run_mutant http_path validationhttp/http.go \
	'violation.Path().String()' 'violation.Path().JSONPointer()' \
	./validationhttp
run_mutant http_severity validationhttp/http.go \
	'value == validation.Warning' 'value == validation.Error' \
	./validationhttp
run_mutant http_truncation validationhttp/http.go \
	'Truncated: report.Truncated()' 'Truncated: false' ./validationhttp
run_mutant http_blocking validationhttp/http.go \
	'if report.HasErrors() {' 'if false {' ./validationhttp
run_mutant jsonapi_path validationjsonapi/jsonapi.go \
	'violation.Path().JSONPointer()' 'violation.Path().String()' \
	./validationjsonapi
run_mutant jsonapi_severity validationjsonapi/jsonapi.go \
	'violation.Severity() == validation.Warning' \
	'violation.Severity() == validation.Error' ./validationjsonapi
run_mutant jsonapi_truncation validationjsonapi/jsonapi.go \
	'Truncated: report.Truncated()' 'Truncated: false' ./validationjsonapi
run_mutant translation_panic validationtext/text.go \
	'if recover() != nil {' 'if false {' ./validationtext
run_mutant translation_length validationtext/text.go \
	'len(text) > maximum' 'len(text) >= maximum' ./validationtext
run_mutant translation_escape validationtext/text.go \
	'return html.EscapeString(text)' 'return text' ./validationtext

echo 'mutation score: 90/90 killed (100.0%)'
