#!/usr/bin/env bash
set -euo pipefail

test -s docs/holiday-datasets.md
if [[ -d datasets ]] && find datasets -type f -print -quit | grep -q .; then
	printf 'bundled datasets require a dedicated deterministic provenance verifier\n' >&2
	exit 1
fi
grep -qF 'No holiday dataset is bundled' docs/holiday-datasets.md
printf 'holiday dataset policy verified: no bundled dataset\n'
