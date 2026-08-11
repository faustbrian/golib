#!/usr/bin/env bash
set -euo pipefail

profile=${1:?coverage profile is required}
read -r covered total < <(awk '
	NR > 1 {
		total += $2
		if ($3 > 0) {
			covered += $2
		}
	}
	END { print covered + 0, total + 0 }
' "$profile")
printf 'production statements covered: %s/%s\n' "$covered" "$total"
test "$total" -gt 0
test "$covered" -eq "$total"
