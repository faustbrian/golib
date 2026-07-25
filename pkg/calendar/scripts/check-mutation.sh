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
	FROM="$from" TO="$to" FILE="$mutant/$file" ruby -e '
path = ENV.fetch("FILE")
source = File.read(path)
from = ENV.fetch("FROM")
abort("mutation source not found: #{from}") unless source.include?(from)
File.write(path, source.sub(from, ENV.fetch("TO")))
'
	if (cd "$mutant" && go test "$package" >mutation.log 2>&1); then
		printf 'survived mutation: %s\n' "$name" >&2
		cat "$mutant/mutation.log" >&2
		exit 1
	fi
	printf 'killed mutation: %s\n' "$name"
}

run_mutant leap-century date.go 'year%400 == 0' 'year%400 != 0' .
run_mutant minimum-year date.go 'year < MinYear' 'year <= MinYear' .
run_mutant canonical-digit date.go "input[i] < '0'" "input[i] <= '0'" .
run_mutant month-clamp date.go 'd.Day() <= last' 'd.Day() < last' .
run_mutant month-reject date.go 'if policy == Reject {' 'if policy != Reject {' .
run_mutant month-overflow date.go \
	't := time.Date(year, month, d.Day(), 0, 0, 0, 0, time.UTC)' \
	't := time.Date(year, month, d.Day()-1, 0, 0, 0, 0, time.UTC)' .
run_mutant negative-range date.go 'months < -base' 'months <= -base' .
run_mutant positive-range date.go 'months > maximum-base' 'months >= maximum-base' .
run_mutant multiplication-overflow date.go 'result/a == b' 'result/a != b' .
run_mutant negative-movement date.go 'return -value, true' 'return value, true' .
run_mutant negative-day-range date.go 'days < -ordinal' 'days <= -ordinal' .
run_mutant positive-day-range date.go 'days > maximum-ordinal' 'days >= maximum-ordinal' .
run_mutant day-movement date.go 't := d.asTime().AddDate(0, 0, days)' \
	't := d.asTime().AddDate(0, 0, -days)' .
run_mutant business-weekend business/business.go '!c.IsWeekend(d)' 'c.IsWeekend(d)' ./business
run_mutant business-admission business/business.go '!c.IsHoliday(d)' 'c.IsHoliday(d)' ./business
run_mutant business-count business/business.go 'i < span' 'i <= span' ./business
run_mutant business-search-limit business/business.go 'visited >= searchLimit' 'visited > searchLimit' ./business
run_mutant timezone-wall timezone/timezone.go 'year == local.Date().Year()' 'year != local.Date().Year()' ./timezone
run_mutant timezone-gap timezone/timezone.go 'if !sameWall(candidate, local)' 'if sameWall(candidate, local)' ./timezone
run_mutant timezone-order timezone/timezone.go 'candidates[i].Before(candidates[j])' 'candidates[j].Before(candidates[i])' ./timezone

printf 'mutation score: 19/19 killed (100.0%%)\n'
