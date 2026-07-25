#!/bin/sh
set -eu

go test -run '^Example' ./...
for module in jwt oidc authotel; do
	(cd "$module" && go test -run '^Example' ./...)
done
