#!/bin/sh
set -eu

report=docs/security-review.md

grep -qx 'Status: approved' "$report"

reviewer=$(sed -n 's/^Reviewer: //p' "$report")
organization=$(sed -n 's/^Organization: //p' "$report")
review_date=$(sed -n 's/^Review date: //p' "$report")
reviewed_commit=$(sed -n 's/^Reviewed commit: //p' "$report")
findings=$(sed -n 's/^Findings: //p' "$report")
resolutions=$(sed -n 's/^Resolutions: //p' "$report")
residual_risks=$(sed -n 's/^Residual risks: //p' "$report")

for value in "$reviewer" "$organization" "$findings" "$resolutions" "$residual_risks"; do
    test -n "$value"
    test "$value" != pending
done

printf '%s\n' "$review_date" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
printf '%s\n' "$reviewed_commit" | grep -Eq '^[0-9a-f]{40}$'
grep -qx 'Approval: approved' "$report"
