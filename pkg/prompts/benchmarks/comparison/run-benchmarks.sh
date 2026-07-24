#!/bin/sh
set -eu

benchtime="${BENCHTIME:-10x}"
count="${COUNT:-1}"

for engine in GoPrompts Huh Survey PromptUI Bubbles; do
	GOWORK=off go test . -run '^$' \
		-bench "BenchmarkInteractiveTextPTY/$engine\$" \
		-benchmem -benchtime="$benchtime" -count="$count" -timeout=3m
done
