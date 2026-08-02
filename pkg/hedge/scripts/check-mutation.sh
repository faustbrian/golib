#!/usr/bin/env bash
set -euo pipefail

temp_root=${TMPDIR:-/tmp}
workspace=$(mktemp -d "${temp_root%/}/hedge-mutation.XXXXXX")
case "$workspace" in
	"${temp_root%/}"/hedge-mutation.*) ;;
	*) echo 'unexpected mutation workspace' >&2; exit 1 ;;
esac
cleanup_workspace() {
	if [[ -n "${workspace:-}" && -d "$workspace" && ! -L "$workspace" ]]; then
		rm -rf -- "${workspace:?}"
	fi
}
trap cleanup_workspace EXIT
baseline="$workspace/baseline"
mkdir -p "$baseline"
tar --exclude=.git --exclude=.tools --exclude=coverage.out --exclude=dist -cf - . | tar -xf - -C "$baseline"

run_mutant() {
	local name=$1 file=$2 from=$3 to=$4
	local mutant="$workspace/$name"
	mkdir -p "$mutant"
	tar -cf - -C "$baseline" . | tar -xf - -C "$mutant"
	FROM="$from" TO="$to" perl -0pi -e '
$changed = s/\Q$ENV{FROM}\E/$ENV{TO}/;
END { die "mutation source not found: $ENV{FROM}\n" unless $changed }
' "$mutant/$file"
	if (cd "$mutant" && GOWORK=off go test -timeout=15s ./... >mutation.log 2>&1); then
		echo "survived mutation: $name" >&2
		cat "$mutant/mutation.log" >&2
		exit 1
	fi
	printf 'killed mutation: %s\n' "$name"
}

run_mutant replay policy.go 'case !config.ReplaySafe:' 'case config.ReplaySafe:'
run_mutant max_hedges policy.go 'config.MaxHedges == 0 || config.MaxHedges > MaxHedges' 'config.MaxHedges == 0 && config.MaxHedges > MaxHedges'
run_mutant budget_bound budget.go 'current >= budget.limit' 'current > budget.limit'
run_mutant budget_release budget.go 'permit.budget.outstanding.Add(^uint64(0))' 'permit.budget.outstanding.Add(0)'
run_mutant winner execution.go 'completion.classification == ClassificationSuccess' 'completion.classification == ClassificationFailure'
run_mutant tie execution.go 'left.result.Ordinal < right.result.Ordinal' 'left.result.Ordinal > right.result.Ordinal'
run_mutant cancellation execution.go 'if err := ctx.Err(); err != nil {' 'if err := ctx.Err(); err == nil {'
run_mutant budget_denial execution.go 'if !admitted || permit == nil {' 'if admitted || permit == nil {'
run_mutant cleanup execution.go 'if completion.hasValue {' 'if !completion.hasValue {'
run_mutant observer_panic execution.go 'defer func() { _ = recover() }()' 'defer func() {}()'

echo 'mutation score: 10/10 killed (100.0%)'
