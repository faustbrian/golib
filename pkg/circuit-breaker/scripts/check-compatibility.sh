#!/bin/sh
set -eu

module=$(go list -m -f '{{.Path}}')
head=$(git rev-parse HEAD)
baseline=""
for tag in $(git tag --list 'v[0-9]*' --sort=-v:refname); do
	if [ "$(git rev-list -n 1 "$tag")" != "$head" ]; then
		baseline=$tag
		break
	fi
done

if [ -z "$baseline" ]; then
	echo "API compatibility: no released baseline; current API establishes v1"
	exit 0
fi

if ! command -v apidiff >/dev/null 2>&1; then
	echo "apidiff is required when a release baseline exists" >&2
	echo "run 'make tools' to install the pinned apidiff version" >&2
	exit 1
fi

apidiff -m "$module@$baseline" "$module"
