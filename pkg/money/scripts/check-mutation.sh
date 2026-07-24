#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
gremlins_version='v0.6.0'

go run "github.com/go-gremlins/gremlins/cmd/gremlins@$gremlins_version" \
  unleash . \
  --exclude-files 'internal/.*' \
  --workers "${MUTATION_WORKERS:-2}" \
  --test-cpu 1 \
  --timeout-coefficient 10 \
  --output-statuses l \
  --threshold-efficacy 100 \
  --threshold-mcover 97

run_mutant() {
  name="$1"
  file="$2"
  expression="$3"
  directory="$temporary/$name"
  mkdir -p "$directory"
  cp -R . "$directory/money"
  ln -s "$(pwd)/../math" "$directory/math"
  ln -s "$(pwd)/../international" "$directory/international"
  before="$(shasum -a 256 "$directory/money/$file")"
  sed -i.bak "$expression" "$directory/money/$file"
  rm "$directory/money/$file.bak"
  after="$(shasum -a 256 "$directory/money/$file")"
  test "$before" != "$after" || { echo "mutation $name did not apply" >&2; exit 1; }
  if (cd "$directory/money" && GOWORK=off go test ./... >/dev/null 2>&1); then
    echo "mutation survived: $name" >&2
    exit 1
  fi
  echo "mutation killed: $name"
}

run_mutant currency-mismatch money.go \
  's/if money.currency != other.currency {/if false {/'
run_mutant context-mismatch money.go \
  's/if money.context != other.context {/if false {/'
run_mutant precision-loss money.go \
  's/if amount.Scale() > int32(context.scale) {/if false {/'
run_mutant remainder-sign allocation.go \
  's/delta := integer.New(int64(remainder.Sign()))/delta := integer.New(1)/'
run_mutant remainder-order allocation.go \
  's/Cmp(remainders\[order\[right\]\]) > 0/Cmp(remainders[order[right]]) < 0/'
run_mutant negative-rate rate.go \
  's/value.Sign() < 0/false/'
run_mutant cash-rounding rational_money.go \
  's/if target.kind != ContextCash {/if true {/'
run_mutant tax-direction tax.go \
  's/tax, err := gross.Sub(net)/tax, err := net.Sub(gross)/'
run_mutant discount-direction discount.go \
  's/final, err := original.Sub(discount)/final, err := discount.Sub(original)/'
run_mutant conversion-base conversion.go \
  's/if source.currency != rate.base {/if false {/'
