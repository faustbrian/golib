#!/bin/sh
set -eu

test -s .github/workflows/ci.yml
test -s .github/workflows/release.yml
test -s ../.github/workflows/keyphrase-ci.yml
grep -q 'make check' .github/workflows/ci.yml
grep -q 'make fuzz mutation bench' .github/workflows/ci.yml
grep -q 'actions/dependency-review-action' .github/workflows/ci.yml
grep -q 'make stable-release-check' .github/workflows/release.yml
grep -q 'working-directory: keyphrase' ../.github/workflows/keyphrase-ci.yml
grep -q 'make release-check' ../.github/workflows/keyphrase-ci.yml
grep -q 'actions/dependency-review-action' ../.github/workflows/keyphrase-ci.yml
