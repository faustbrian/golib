#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
subject="$root/scripts/workflow_policy.sh"
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-workflow-policy.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

write_valid_workflow() {
	directory=$1
	mkdir -p "$directory"
	cat > "$directory/ci.yml" <<'EOF'
name: CI
on: push
permissions:
  contents: read
jobs:
  codeql:
    strategy:
      matrix:
        os:
          - ubuntu-latest
          - macos-latest
          - windows-latest
    permissions:
      contents: read
      security-events: write
    steps:
      - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567
      - uses: actions/setup-go@fedcba9876543210fedcba9876543210fedcba98
        with:
          go-version-file: .go-version
      - uses: github/codeql-action/init@89abcdef0123456789abcdef0123456789abcdef
        with:
          build-mode: manual
          languages: go
      - name: Compile Go for CodeQL
        run: go build -trimpath ./...
      - uses: github/codeql-action/analyze@89abcdef0123456789abcdef0123456789abcdef
      - uses: ./local-action
      - name: Run portable Windows gates
        if: runner.os == 'Windows'
        run: |
          go vet ./...
          go test ./...
          go test -race ./...
          go build -trimpath ./cmd/golib-analysis
EOF
}

expect_failure() {
	directory=$1
	message=$2
	if "$subject" "$directory" > "$temporary/output" 2>&1; then
		printf 'workflow policy unexpectedly accepted %s\n' "$directory" >&2
		exit 1
	fi
	if ! grep -F "$message" "$temporary/output" >/dev/null; then
		printf 'workflow policy did not report %s\n' "$message" >&2
		cat "$temporary/output" >&2
		exit 1
	fi
}

valid="$temporary/valid"
write_valid_workflow "$valid"
"$subject" "$valid"

unpinned="$temporary/unpinned"
write_valid_workflow "$unpinned"
sed -i.bak 's/@0123456789abcdef0123456789abcdef01234567/@v7/' \
	"$unpinned/ci.yml"
rm "$unpinned/ci.yml.bak"
expect_failure "$unpinned" 'action is not pinned to a commit SHA'

named_unpinned="$temporary/named-unpinned"
write_valid_workflow "$named_unpinned"
sed -i.bak \
	's|- uses: actions/checkout@0123456789abcdef0123456789abcdef01234567|- name: Check out source\
        uses: actions/checkout@v7|' "$named_unpinned/ci.yml"
rm "$named_unpinned/ci.yml.bak"
expect_failure "$named_unpinned" 'action is not pinned to a commit SHA'

missing_codeql="$temporary/missing-codeql"
write_valid_workflow "$missing_codeql"
sed -i.bak '/github\/codeql-action\/init@/d; /github\/codeql-action\/analyze@/d' \
	"$missing_codeql/ci.yml"
rm "$missing_codeql/ci.yml.bak"
expect_failure "$missing_codeql" \
	'CI requires pinned CodeQL init and analyze actions on one revision'

automatic_codeql="$temporary/automatic-codeql"
write_valid_workflow "$automatic_codeql"
sed -i.bak 's/build-mode: manual/build-mode: autobuild/' \
	"$automatic_codeql/ci.yml"
rm "$automatic_codeql/ci.yml.bak"
expect_failure "$automatic_codeql" \
	'CodeQL must use the reviewed manual Go build'

missing_codeql_build="$temporary/missing-codeql-build"
write_valid_workflow "$missing_codeql_build"
sed -i.bak '/name: Compile Go for CodeQL/,+1d' \
	"$missing_codeql_build/ci.yml"
rm "$missing_codeql_build/ci.yml.bak"
expect_failure "$missing_codeql_build" \
	'CodeQL must use the reviewed manual Go build'

missing_windows="$temporary/missing-windows"
write_valid_workflow "$missing_windows"
sed -i.bak '/windows-latest/d' "$missing_windows/ci.yml"
rm "$missing_windows/ci.yml.bak"
expect_failure "$missing_windows" \
	'CI must execute analyzer tests on Linux, macOS, and Windows'

missing_windows_gate="$temporary/missing-windows-gate"
write_valid_workflow "$missing_windows_gate"
sed -i.bak '/name: Run portable Windows gates/,$d' \
	"$missing_windows_gate/ci.yml"
rm "$missing_windows_gate/ci.yml.bak"
expect_failure "$missing_windows_gate" \
	'CI must run the portable Windows analyzer gate'

hardcoded_toolchain="$temporary/hardcoded-toolchain"
write_valid_workflow "$hardcoded_toolchain"
sed -i.bak 's/go-version-file: .go-version/go-version: 1.26.5/' \
	"$hardcoded_toolchain/ci.yml"
rm "$hardcoded_toolchain/ci.yml.bak"
expect_failure "$hardcoded_toolchain" \
	'workflow must use the canonical .go-version toolchain'

unauthorized="$temporary/unauthorized"
write_valid_workflow "$unauthorized"
sed -i.bak '/security-events: write/a\
      issues: write' "$unauthorized/ci.yml"
rm "$unauthorized/ci.yml.bak"
expect_failure "$unauthorized" 'unauthorized write permission'

wrong_workflow="$temporary/wrong-workflow"
write_valid_workflow "$wrong_workflow"
sed -i.bak 's/security-events: write/contents: write/' \
	"$wrong_workflow/ci.yml"
rm "$wrong_workflow/ci.yml.bak"
expect_failure "$wrong_workflow" 'unauthorized write permission'

broad="$temporary/broad"
write_valid_workflow "$broad"
sed -i.bak 's/permissions:/permissions: write-all/' "$broad/ci.yml"
rm "$broad/ci.yml.bak"
expect_failure "$broad" 'workflow permissions must default to contents read'

printf 'workflow policy runner tests passed\n'
