#!/bin/sh
set -eu

cd specification/gda
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum -c SHA256SUMS
else
	shasum -a 256 -c SHA256SUMS
fi
