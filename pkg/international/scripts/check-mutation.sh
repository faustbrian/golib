#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

gremlins_version='v0.6.0'

run_acceptance_mutants() {
  package="$1"
  GOWORK=off go run \
    "github.com/go-gremlins/gremlins/cmd/gremlins@$gremlins_version" \
    unleash "./$package" \
    --exclude-files 'encoding.go' \
    --exclude-files 'dataset.go' \
    --exclude-files 'provenance.go' \
    --exclude-files 'data_generated.go' \
    --workers "${MUTATION_WORKERS:-2}" \
    --test-cpu 1 \
    --timeout-coefficient 10 \
    --output-statuses l \
    --threshold-efficacy 100 \
    --threshold-mcover 100
}

run_mutant() {
  name="$1"
  file="$2"
  package="$3"
  expression="$4"
  directory="$temporary/$name"
  mkdir -p "$directory"
  tar --exclude=.git --exclude=build --exclude=data --exclude='.idea' \
    --exclude=go.work --exclude=go.work.sum -cf - . | tar -xf - -C "$directory"
  if ! (cd "$directory" && GOWORK=off go test "$package" >/dev/null 2>&1); then
    echo "mutation baseline failed before applying $name" >&2
    exit 1
  fi
  before="$(shasum -a 256 "$directory/$file")"
  sed -i.bak "$expression" "$directory/$file"
  rm "$directory/$file.bak"
  after="$(shasum -a 256 "$directory/$file")"
  test "$before" != "$after" || { echo "mutation $name did not apply" >&2; exit 1; }
  if (cd "$directory" && GOWORK=off go test "$package" >/dev/null 2>&1); then
    echo "mutation survived: $name" >&2
    exit 1
  fi
  echo "mutation killed: $name"
}

run_mutant country-canonicalization country/country.go ./country \
  's/return Parse(strings.ToUpper(input))/return Parse(input)/'
run_mutant country-historic-status country/country.go ./country \
  's/return options.AllowHistoric/return false/'
run_mutant country-numeric-mapping country/country.go ./country \
  's/alpha2: code.value/alpha2: ""/'
run_mutant subdivision-history subdivision/subdivision.go ./subdivision \
  's/record.status == international.StatusDeleted && !options.AllowHistoric/false/'
run_mutant language-canonicalization language/language.go ./language \
  's/base.String() != input || canonical.String() != input/false/'
run_mutant locale-underscore locale/locale.go ./locale \
  's/strings.Contains(input, "_")/false/'
run_mutant currency-history currency/currency.go ./currency \
  's/record.status == international.StatusHistoric/false/'
run_mutant currency-numeric-ambiguity currency/currency.go ./currency \
  's/if alphabetic != ""/if false/'
run_mutant phone-validity phone/phone.go ./phone \
  's/valid:         phonenumbers.IsValidNumber(parsed)/valid:         true/'
run_mutant postal-control postal/postal.go ./postal \
  's/hasControl(input)/false/'
run_mutant status-encoding status.go . \
  's/case "deleted":/case "deleted-disabled":/'
run_mutant dataset-status-classification dataset.go . \
  's/oldRecord.Status != newRecord.Status/false/'
run_mutant parse-error-kind errors.go . \
  's/kind = diagnosticKind(kind)/kind = kind/'

for package in country subdivision language locale currency phone postal; do
  run_acceptance_mutants "$package"
done
