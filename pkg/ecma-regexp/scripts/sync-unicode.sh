#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
archive=$(mktemp "${TMPDIR:-/tmp}/ecma-regexp-ucd.XXXXXX")
sequences=$(mktemp "${TMPDIR:-/tmp}/ecma-regexp-emoji-sequences.XXXXXX")
zwj=$(mktemp "${TMPDIR:-/tmp}/ecma-regexp-emoji-zwj.XXXXXX")
trap 'rm -f "$archive" "$sequences" "$zwj"' EXIT HUP INT TERM

curl --fail --location --silent --show-error \
    https://www.unicode.org/Public/zipped/16.0.0/UCD.zip \
    --output "$archive"
curl --fail --location --silent --show-error \
    https://unicode.org/Public/emoji/16.0/emoji-sequences.txt \
    --output "$sequences"
curl --fail --location --silent --show-error \
    https://unicode.org/Public/emoji/16.0/emoji-zwj-sequences.txt \
    --output "$zwj"

cd "$root"
go run ./internal/cmd/unicodegen \
    -ucd "$archive" \
    -emoji-sequences "$sequences" \
    -emoji-zwj-sequences "$zwj" \
    -output unicode_tables_generated.go
