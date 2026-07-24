#!/usr/bin/env bash
set -euo pipefail

minimum_score=${MUTATION_MIN_SCORE:-0.65}
python3 - "$minimum_score" <<'PY'
import sys

minimum = float(sys.argv[1])
if minimum < 0.65 or minimum > 1.0:
    raise SystemExit("MUTATION_MIN_SCORE must be within [0.65, 1.0]")
PY

workspace=$(mktemp -d)
trap 'chmod -R u+w "$workspace"; rm -rf "$workspace"' EXIT

rsync -a --exclude '.git' --exclude '*.tmp' ./ "$workspace/repository/"
cd "$workspace/repository"

go run github.com/avito-tech/go-mutesting/cmd/go-mutesting@v0.0.0-20251226130216-48d0401f00fb \
	--config=.go-mutesting.yml \
	--exec-timeout 20 \
	value.go schedule.go exception.go query.go ranges.go timezone.go \
	search.go composition.go

python3 - "$minimum_score" <<'PY'
import json
import sys
from pathlib import Path

minimum = float(sys.argv[1])
report = json.loads(Path("report.json").read_text(encoding="utf-8"))
stats = report["stats"]
score = float(stats["msi"])
errors = int(stats["errorCount"])
total = int(stats["totalMutantsCount"])
killed = int(stats["killedCount"])
escaped = int(stats["escapedCount"])

print(
    f"mutation evidence: score={score:.6f} killed={killed} "
    f"escaped={escaped} total={total} errors={errors}"
)
if total == 0:
    raise SystemExit("mutation gate produced no mutants")
if errors != 0:
    raise SystemExit(f"mutation gate reported {errors} execution errors")
if score < minimum:
    raise SystemExit(
        f"mutation score {score:.6f} is below required {minimum:.6f}"
    )
PY
