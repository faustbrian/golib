#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository_root="$(cd "${module_root}/../.." && pwd)"
task_root="$(mktemp -d)"
task_gocache="$(mktemp -d)"
task_modcache="$(mktemp -d)"

cleanup() {
	chmod -R u+w "${task_root}" "${task_gocache}" "${task_modcache}" 2>/dev/null || true
	find "${task_root}" -depth -delete
    find "${task_gocache}" -depth -delete
	find "${task_modcache}" -depth -delete
}
trap cleanup EXIT

export GOCACHE="${task_gocache}"
export GOMODCACHE="${task_modcache}"
export GOWORK=off
export WORKFLOW_SOAK_DURATION="${WORKFLOW_SOAK_DURATION:-48h}"

run_root="${module_root}"
if [[ "${WORKFLOW_SOAK_ALLOW_SHORT:-}" != "1" ]]; then
	if [[ -n "$(git -C "${repository_root}" status --porcelain --untracked-files=all -- pkg/workflow)" ]]; then
		printf 'workflow soak requires a clean committed pkg/workflow snapshot\n' >&2
		exit 1
	fi
	execution_revision="$(git -C "${repository_root}" rev-parse HEAD)"
	input_digest="$(
		git -C "${repository_root}" ls-tree -r --full-tree "${execution_revision}" -- pkg/workflow |
			shasum -a 256 |
			awk '{print $1}'
	)"
	git -C "${repository_root}" archive "${execution_revision}" pkg/workflow |
		tar -x -C "${task_root}"
	run_root="${task_root}/pkg/workflow"
	printf 'workflow_soak_input revision=%s input_digest=%s duration=%s\n' \
		"${execution_revision}" "${input_digest}" "${WORKFLOW_SOAK_DURATION}"
fi

cd "${run_root}"
go test . \
    -run '^TestWorkflowMultiDaySoakKeepsReplayAndWorkerResourcesBounded$' \
    -count=1 \
    -timeout=0 \
    -v
