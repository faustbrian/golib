#!/bin/sh
set -eu

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

git -C "$temporary" init -q
git -C "$temporary" config user.name "Reproducibility Test"
git -C "$temporary" config user.email "reproducibility@example.invalid"
mkdir -p "$temporary/knapsack/scripts" "$temporary/sibling"
printf '%s\n' 'module example.invalid/knapsack' > "$temporary/knapsack/go.mod"
printf '%s\n' 'test license' > "$temporary/knapsack/LICENSE"
cp scripts/check-reproducible.sh "$temporary/knapsack/scripts/"
printf '%s\n' 'one' > "$temporary/sibling/value.txt"
git -C "$temporary" add -- knapsack sibling/value.txt
git -C "$temporary" commit -q -m "test: create fixture" -m "Create a module and sibling tree for archive testing."

(
	cd "$temporary/knapsack"
	./scripts/check-reproducible.sh --archive "$temporary/first.tar.gz"
)

printf '%s\n' 'two' > "$temporary/sibling/value.txt"
git -C "$temporary" add -- sibling/value.txt
git -C "$temporary" commit -q -m "test: change sibling" -m "Change only the sibling tree between module archives."

(
	cd "$temporary/knapsack"
	./scripts/check-reproducible.sh --archive "$temporary/second.tar.gz"
)

cmp "$temporary/first.tar.gz" "$temporary/second.tar.gz"
