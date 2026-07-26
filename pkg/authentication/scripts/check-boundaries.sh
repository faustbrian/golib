#!/bin/sh
set -eu

unexpected_modules=$(go list -m all | awk '
	$1 != "github.com/faustbrian/golib/pkg/authentication" &&
	$1 != "github.com/faustbrian/golib/pkg/clock" { print $1 }
')
if [ -n "$unexpected_modules" ]; then
	printf 'root module has forbidden dependencies:\n%s\n' \
		"$unexpected_modules" >&2
	exit 1
fi

dependencies=$(cd authotel && go list -deps .)
for forbidden in go.opentelemetry.io/otel/sdk github.com/faustbrian/golib/pkg/telemetry; do
	if printf '%s\n' "$dependencies" | grep -Eq "^${forbidden}(/|$)"; then
		printf 'authotel has forbidden production dependency: %s\n' "$forbidden" >&2
		exit 1
	fi
done

for module in . jwt oidc authotel; do
	dependencies=$(cd "$module" && go list -deps ./...)
	for forbidden in \
		github.com/faustbrian/golib/pkg/service \
		github.com/faustbrian/golib/pkg/http-client \
		github.com/faustbrian/golib/pkg/authorization
	do
		if printf '%s\n' "$dependencies" | grep -Eq "^${forbidden}(/|$)"; then
			printf '%s has forbidden production dependency: %s\n' \
				"$module" "$forbidden" >&2
			exit 1
		fi
	done
done
