#!/usr/bin/env bash
set -euo pipefail

go test ./jsonast -count=1
go test . -run 'TestJSONDefinitionRoundTripPreservesEvaluation|TestEvaluationErrorsStayBoundedRedactedAndIndeterminate' -count=1
