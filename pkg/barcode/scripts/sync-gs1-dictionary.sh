#!/bin/sh
set -eu

release=2026-01-27
expected=6461ece7c03420fb27a3f18163ef9a08694aa4bb2491dea25f63bcdf22451a99
url="https://raw.githubusercontent.com/gs1/gs1-syntax-dictionary/${release}/gs1-syntax-dictionary.txt"
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
target="${script_dir}/../specification/gs1/gs1-syntax-dictionary.txt"
temporary=$(mktemp)
trap 'rm -f "$temporary"' EXIT HUP INT TERM

curl -fsSL "$url" -o "$temporary"
actual=$(shasum -a 256 "$temporary" | awk '{print $1}')
if [ "$actual" != "$expected" ]; then
	echo "GS1 dictionary checksum mismatch" >&2
	exit 1
fi

mkdir -p "$(dirname -- "$target")"
mv "$temporary" "$target"
trap - EXIT HUP INT TERM
