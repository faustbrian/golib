#!/bin/sh
set -eu

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

cat > "$temporary/darwin.txt" <<'EOF'
        0.01 real         0.00 user         0.00 sys
          12345678  maximum resident set size
EOF

cat > "$temporary/linux.txt" <<'EOF'
	Command being timed: "benchmark"
	Maximum resident set size (kbytes): 12345
EOF

cat > "$temporary/linux-large.txt" <<'EOF'
	Maximum resident set size (kbytes): 1234567
EOF

darwin="$(./scripts/benchmark-rss.sh --parse darwin "$temporary/darwin.txt")"
test "$darwin" = "12345678" || {
	printf 'Darwin RSS parser returned %s\n' "$darwin" >&2
	exit 1
}

linux="$(./scripts/benchmark-rss.sh --parse linux "$temporary/linux.txt")"
test "$linux" = "12641280" || {
	printf 'Linux RSS parser returned %s\n' "$linux" >&2
	exit 1
}

linux_large="$(./scripts/benchmark-rss.sh --parse linux "$temporary/linux-large.txt")"
test "$linux_large" = "1264196608" || {
	printf 'Linux large RSS parser returned %s\n' "$linux_large" >&2
	exit 1
}

if ./scripts/benchmark-rss.sh --parse unknown "$temporary/darwin.txt" \
	> /dev/null 2>&1; then
	printf '%s\n' 'unsupported RSS formats must fail closed' >&2
	exit 1
fi
