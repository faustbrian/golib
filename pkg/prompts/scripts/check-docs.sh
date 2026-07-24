#!/bin/sh
set -eu

required='README.md
CHANGELOG.md
SECURITY.md
CONTRIBUTING.md
CODE_OF_CONDUCT.md
SUPPORT.md
GOVERNANCE.md
docs/accessibility.md
docs/accessibility-review.md
docs/api.md
docs/architecture.md
docs/benchmarks.md
docs/compatibility.md
docs/dependency-evaluation.md
docs/faq.md
docs/forms.md
docs/hardening-evidence.md
docs/integrations.md
docs/interactive-input.md
docs/migrations.md
docs/mutation.md
docs/progress-and-presentation.md
docs/prompt-types.md
docs/release.md
docs/rendering.md
docs/secrets.md
docs/security.md
docs/selection.md
docs/terminal-adapter.md
docs/troubleshooting.md
docs/validation.md
scripts/accessibility-review.go'

printf '%s\n' "$required" | while IFS= read -r document; do
	test -s "$document" || {
		printf 'required documentation is missing or empty: %s\n' "$document" >&2
		exit 1
	}
done

workflow='../.github/workflows/prompts-ci.yml'
if test -f "$workflow" && grep -qi 'windows-latest' "$workflow"; then
	printf 'unsupported Windows runner remains in prompts CI\n' >&2
	exit 1
fi

review_binary="$(mktemp)"
trap 'rm -f "$review_binary"' EXIT HUP INT TERM
GOWORK=off go build -o "$review_binary" ./scripts/accessibility-review.go
GOWORK=off go test ./scripts/accessibility-review.go \
	./scripts/accessibility-review_test.go -count=1

GOWORK=off go test ./... -run '^Example' -count=1
