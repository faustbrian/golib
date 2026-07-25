#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    printf 'usage: %s <enforce|discover> <module-directory>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
mode="$1"
module="$2"
if [[ "${mode}" != "enforce" && "${mode}" != "discover" ]]; then
    printf 'mutation mode must be enforce or discover\n' >&2
    exit 2
fi
discover_only=0
if [[ "${mode}" == "discover" ]]; then
    discover_only=1
fi
directory="${root}/${module}"
artifact="${root}/.artifacts/${module}"
report="${artifact}/mutation.json"
run_directory="${artifact}/mutation-run-${RANDOM}"
checkpoint_directory="${artifact}/mutation-checkpoints"
history_migrations="${root}/.golib/mutation-history-migrations.json"
mkdir -p "${run_directory}" "${checkpoint_directory}"
active_build_cache=""
historical_root_base="${run_directory}/historical-inputs"
mutation_arguments=()
execution_revision="$(git -C "${root}" rev-parse HEAD)"

cleanup() {
    if [[ -z "${active_build_cache}" || ! -d "${active_build_cache}" ]]; then
        :
    else
        case "${active_build_cache}" in
            "${run_directory}"/*.go-cache)
                find "${active_build_cache}" -depth -delete
                ;;
            *)
                printf 'refusing to remove unexpected mutation cache: %s\n' \
                    "${active_build_cache}" >&2
                return 1
                ;;
        esac
        active_build_cache=""
    fi
    if [[ -d "${historical_root_base}" ]]; then
        find "${historical_root_base}" -depth -delete
    fi
}
trap cleanup EXIT INT TERM

# shellcheck disable=SC1091
source "${root}/.golib/versions.env"
# shellcheck disable=SC1091
source "${root}/scripts/internal/mutation-command.sh"
gremlins_binary="$("${root}/scripts/build-golib-gremlins.sh")"
environment_identity="$(go env -json GOVERSION GOOS GOARCH CGO_ENABLED)"
legacy_gate_input_digest=""
shared_coverage="${run_directory}/integration.coverage"
shared_coverage_elapsed=""
modfile=""
owned_count="$(jq -r --arg directory "${module}" '
    .modules[] | select(.directory == $directory) | .owned_dependencies | length
' "${root}/modules.json")"
if [[ "${owned_count}" -gt 0 ]]; then
    modfile="${run_directory}/golib-mutation.mod"
    cp "${directory}/go.mod" "${modfile}"
    if [[ -f "${directory}/go.sum" ]]; then
        cp "${directory}/go.sum" "${modfile%.mod}.sum"
    fi
    while IFS=$'\t' read -r owned_path owned_directory; do
        [[ -n "${owned_path}" && -n "${owned_directory}" ]] || continue
        GOWORK=off go mod edit -modfile="${modfile}" \
            -replace="${owned_path}=${root}/${owned_directory}"
    done < <(jq -r '.modules[]
        | select(.directory != "." and .kind != "fixture")
        | [.module_path, .directory]
        | @tsv' "${root}/modules.json")
fi
cd "${directory}"

tags="$(jq -r --arg directory "${module}" \
    '.modules[] | select(.directory == $directory) | .test_tags | join(",")' \
    "${root}/modules.json")"
packages=()
while IFS= read -r package_directory; do
    [[ -n "${package_directory}" ]] && packages+=("${package_directory}")
done < <(jq -r --arg directory "${module}" '
    .modules[]
    | select(.directory == $directory)
    | .packages[]
    | select(.coverage_required == true)
    | .directory
' "${root}/modules.json")
[[ "${#packages[@]}" -gt 0 ]] || {
    printf 'mutation package selection is empty for %s\n' "${module}" >&2
    exit 1
}

expected_packages_file="${run_directory}/expected-packages.json"
printf '%s\n' "${packages[@]}" | jq -R -s \
    'split("\n") | map(select(length > 0))' >"${expected_packages_file}"
reports=()

ensure_shared_coverage() {
    if [[ -s "${shared_coverage}" && -n "${shared_coverage_elapsed}" ]]; then
        return
    fi
    if [[ -n "${modfile}" ]]; then
        shared_coverage_elapsed="$(
            GOWORK=off GOFLAGS="-modfile=${modfile} -mod=mod" \
                "${root}/scripts/internal/mutation-coverage.sh" \
                "${shared_coverage}" "${tags}"
        )"
    else
        shared_coverage_elapsed="$(
            GOWORK=off "${root}/scripts/internal/mutation-coverage.sh" \
                "${shared_coverage}" "${tags}"
        )"
    fi
    [[ -s "${shared_coverage}" ]] || {
        printf 'shared mutation coverage profile is missing for %s\n' \
            "${module}" >&2
        exit 1
    }
}

historical_package_digest() {
    local revision="$1"
    local package_directory="$2"
    local snapshot="${historical_root_base}/${revision}"
    case "${revision}" in
        *[!0-9a-f]*|"")
            printf 'invalid checkpoint execution revision: %s\n' \
                "${revision}" >&2
            return 1
            ;;
    esac
    if [[ ! -d "${snapshot}" ]]; then
        mkdir -p "${snapshot}"
        if ! git -C "${root}" cat-file -e "${revision}^{commit}" 2>/dev/null; then
            printf 'checkpoint revision is unavailable: %s\n' \
                "${revision}" >&2
            find "${snapshot}" -depth -delete
            return 1
        fi
        if ! git -C "${root}" archive "${revision}" | tar -x -C "${snapshot}"; then
            find "${snapshot}" -depth -delete
            return 1
        fi
        cp "${root}/scripts/gate-input-digest.sh" \
            "${snapshot}/scripts/gate-input-digest.sh"
        chmod +x "${snapshot}/scripts/gate-input-digest.sh"
    fi
    GOLIB_ROOT="${snapshot}" \
        "${snapshot}/scripts/gate-input-digest.sh" \
        mutation "${module}" "${package_directory}"
}

checkpoint_report_digest() {
    jq -S -c '.report' "$1" | shasum -a 256 | awk '{print $1}'
}

approved_history_migration() {
    local checkpoint="$1"
    local current_package_digest="$2"
    local report_digest
    [[ -s "${history_migrations}" ]] || return 1
    report_digest="$(checkpoint_report_digest "${checkpoint}")"
    jq -e \
        --arg module "${module}" \
        --arg package "${package_directory}" \
        --arg execution_revision "$(
            jq -r '.execution_revision' "${checkpoint}"
        )" \
        --arg gate_input_digest "$(
            jq -r '.gate_input_digest' "${checkpoint}"
        )" \
        --arg current_gate_input_digest "${current_package_digest}" \
        --arg gremlins_version "${GREMLINS_VERSION}" \
        --arg report_digest "${report_digest}" \
        '
            .schema_version == 2 and
            ([.entries[] | select(
                .module == $module and
                .package == $package and
                .execution_revision == $execution_revision and
                .gate_input_digest == $gate_input_digest and
                (
                    .replacement_gate_input_digest //
                    .gate_input_digest
                ) == $current_gate_input_digest and
                .gremlins_version == $gremlins_version and
                .report_sha256 == $report_digest
            )] | length == 1)
        ' "${history_migrations}" >/dev/null
}

write_aggregate() {
    local aggregate_tmp validated_revision
    aggregate_tmp="$(mktemp "${report}.tmp.XXXXXX")"
    validated_revision="$(git -C "${root}" rev-parse HEAD)"
    jq -s \
        --arg module "${module}" \
        --arg validated_revision "${validated_revision}" \
        --slurpfile expected "${expected_packages_file}" '
        {
            schema_version: 3,
            module: $module,
            execution_revisions: ([.[].execution_revision] | unique),
            validated_revision: $validated_revision,
            gate_input_digests: (
                map({key: .package, value: .gate_input_digest})
                | from_entries
            ),
            gremlins_versions: ([.[].gremlins_version] | unique),
            environments: ([.[].environment] | unique),
            expected_packages: $expected[0],
            completed_packages: [.[].package],
            complete: (length == ($expected[0] | length)),
            packages: .
        }
    ' "${reports[@]}" >"${aggregate_tmp}"
    mv "${aggregate_tmp}" "${report}"
}

for package_directory in "${packages[@]}"; do
    target="."
    slug="root"
    if [[ "${package_directory}" != "." ]]; then
        target="./${package_directory}"
        slug="${package_directory//\//-}"
    fi
    package_report="${run_directory}/${slug}.json"
    checkpoint="${checkpoint_directory}/${slug}.json"
    package_input_digest="$(
        "${root}/scripts/gate-input-digest.sh" \
            mutation "${module}" "${package_directory}"
    )"
    active_build_cache="${run_directory}/${slug}.go-cache"
    mkdir -p "${active_build_cache}"
    build_mutation_arguments \
        "${target}" "${package_report}" "${tags}" "${discover_only}"

    reviewed_zero_mutant() {
        local source_directory source_digest reviewed
        source_directory="${module}"
        if [[ "${package_directory}" != "." ]]; then
            source_directory="${module}/${package_directory}"
        fi
        source_digest="$("${root}/scripts/package-source-digest.sh" \
            "${source_directory}")"
        reviewed="$(jq --arg module "${module}" \
            --arg package "${package_directory}" \
            --arg digest "${source_digest}" \
            --arg version "${GREMLINS_VERSION}" '
                [.packages[] | select(
                    .module_directory == $module and
                    .package_directory == $package and
                    .source_digest == $digest and
                    .gremlins_version == $version
                )] | length
            ' "${root}/.golib/mutation-zero-inventory.json")"
        [[ "${reviewed}" -eq 1 ]]
    }

    if [[ "${discover_only}" -eq 0 && -s "${checkpoint}" ]] &&
        jq -e \
            --arg module "${module}" \
            --arg package "${package_directory}" \
            --arg digest "${package_input_digest}" \
            --arg version "${GREMLINS_VERSION}" '
                .schema_version == 3 and
                .module == $module and
                .package == $package and
                .gate_input_digest == $digest and
                .gremlins_version == $version and
                (.report.files | type == "array") and
                ([.report.files[].mutations[]? | select(.status != "KILLED")] | length == 0)
            ' "${checkpoint}" >/dev/null; then
        checkpoint_total="$(
            jq '[.report.files[].mutations[]?] | length' "${checkpoint}"
        )"
        if [[ "${checkpoint_total}" -gt 0 ]] || reviewed_zero_mutant; then
            checkpoint_tmp="$(mktemp "${checkpoint}.tmp.XXXXXX")"
            jq --arg revision "$(git -C "${root}" rev-parse HEAD)" \
                '.validated_revision = $revision' \
                "${checkpoint}" >"${checkpoint_tmp}"
            mv "${checkpoint_tmp}" "${checkpoint}"
            reports+=("${checkpoint}")
            write_aggregate
            printf '[%s] %s reused content-identical mutation evidence\n' \
                "${module}" "${target}"
            continue
        fi
    fi

    legacy_package_digest="$(
        GOLIB_MUTATION_DIGEST_RESOLUTION=caller \
            "${root}/scripts/gate-input-digest.sh" \
            mutation "${module}" "${package_directory}"
    )"
    if [[ "${discover_only}" -eq 0 && -s "${checkpoint}" ]] &&
        jq -e \
            --arg module "${module}" \
            --arg package "${package_directory}" \
            --arg digest "${legacy_package_digest}" \
            --arg version "${GREMLINS_VERSION}" '
                .schema_version == 3 and
                .module == $module and
                .package == $package and
                .gate_input_digest == $digest and
                .gremlins_version == $version and
                (.report.files | type == "array") and
                ([.report.files[].mutations[]? | select(.status != "KILLED")] | length == 0)
            ' "${checkpoint}" >/dev/null; then
        checkpoint_total="$(
            jq '[.report.files[].mutations[]?] | length' "${checkpoint}"
        )"
        if [[ "${checkpoint_total}" -gt 0 ]] || reviewed_zero_mutant; then
            checkpoint_tmp="$(mktemp "${checkpoint}.tmp.XXXXXX")"
            jq \
                --arg revision "$(git -C "${root}" rev-parse HEAD)" \
                --arg digest "${package_input_digest}" '
                .validated_revision = $revision
                | .identity_migration = {
                    reason: "canonical-workspace-resolution",
                    previous_gate_input_digest: .gate_input_digest
                }
                | .gate_input_digest = $digest
            ' "${checkpoint}" >"${checkpoint_tmp}"
            mv "${checkpoint_tmp}" "${checkpoint}"
            reports+=("${checkpoint}")
            write_aggregate
            printf '[%s] %s migrated caller-dependent mutation identity\n' \
                "${module}" "${target}"
            continue
        fi
    fi

    if [[ "${discover_only}" -eq 0 && -s "${checkpoint}" ]] &&
        jq -e \
            --arg module "${module}" \
            --arg package "${package_directory}" \
            --arg version "${GREMLINS_VERSION}" '
                .schema_version == 3 and
                .module == $module and
                .package == $package and
                .gremlins_version == $version and
                (.report.files | type == "array") and
                ([.report.files[].mutations[]? | select(.status != "KILLED")] | length == 0)
            ' "${checkpoint}" >/dev/null &&
        approved_history_migration \
            "${checkpoint}" "${package_input_digest}"; then
        checkpoint_total="$(
            jq '[.report.files[].mutations[]?] | length' "${checkpoint}"
        )"
        if [[ "${checkpoint_total}" -gt 0 ]] || reviewed_zero_mutant; then
            checkpoint_tmp="$(mktemp "${checkpoint}.tmp.XXXXXX")"
            jq \
                --arg revision "$(git -C "${root}" rev-parse HEAD)" \
                --arg replacement_scope_sha256 "$(
                    jq -r '.replacement_scope_sha256' "${history_migrations}"
                )" \
                --arg digest "${package_input_digest}" '
                .validated_revision = $revision
                | .history_migration = {
                    replacement_scope_sha256: $replacement_scope_sha256,
                    previous_gate_input_digest: .gate_input_digest
                }
                | .gate_input_digest = $digest
            ' "${checkpoint}" >"${checkpoint_tmp}"
            mv "${checkpoint_tmp}" "${checkpoint}"
            reports+=("${checkpoint}")
            write_aggregate
            printf '[%s] %s migrated reset-safe mutation evidence\n' \
                "${module}" "${target}"
            continue
        fi
    fi

    if [[ "${discover_only}" -eq 0 && -s "${checkpoint}" ]] &&
        jq -e \
            --arg module "${module}" \
            --arg package "${package_directory}" \
            --arg version "${GREMLINS_VERSION}" '
                .schema_version == 3 and
                .module == $module and
                .package == $package and
                .gremlins_version == $version and
                (.report.files | type == "array") and
                ([.report.files[].mutations[]? | select(.status != "KILLED")] | length == 0)
            ' "${checkpoint}" >/dev/null; then
        checkpoint_revision="$(
            jq -r '.validated_revision // .execution_revision' "${checkpoint}"
        )"
        if historical_input_digest="$(
            historical_package_digest \
                "${checkpoint_revision}" "${package_directory}"
        )" && [[ "${historical_input_digest}" == "${package_input_digest}" ]]; then
            checkpoint_total="$(
                jq '[.report.files[].mutations[]?] | length' "${checkpoint}"
            )"
            if [[ "${checkpoint_total}" -gt 0 ]] || reviewed_zero_mutant; then
                checkpoint_tmp="$(mktemp "${checkpoint}.tmp.XXXXXX")"
                jq \
                    --arg revision "$(git -C "${root}" rev-parse HEAD)" \
                    --arg digest "${package_input_digest}" '
                    .validated_revision = $revision
                    | .previous_gate_input_digest = .gate_input_digest
                    | .historical_package_input_digest = $digest
                    | .gate_input_digest = $digest
                ' "${checkpoint}" >"${checkpoint_tmp}"
                mv "${checkpoint_tmp}" "${checkpoint}"
                reports+=("${checkpoint}")
                write_aggregate
                printf '[%s] %s migrated historically identical mutation evidence\n' \
                    "${module}" "${target}"
                continue
            fi
        fi
    fi

    if [[ "${discover_only}" -eq 0 && -s "${checkpoint}" ]] &&
        jq -e \
            --arg module "${module}" \
            --arg package "${package_directory}" \
            --arg version "${GREMLINS_VERSION}" '
                .schema_version == 2 and
                .module == $module and
                .package == $package and
                .gremlins_version == $version and
                (.report.files | type == "array") and
                ([.report.files[].mutations[]? | select(.status != "KILLED")] | length == 0)
            ' "${checkpoint}" >/dev/null; then
        if [[ -z "${legacy_gate_input_digest}" ]]; then
            legacy_gate_input_digest="$(
                "${root}/scripts/gate-input-digest.sh" \
                    mutation-legacy "${module}"
            )"
        fi
        if jq -e --arg digest "${legacy_gate_input_digest}" \
            '.gate_input_digest == $digest' "${checkpoint}" >/dev/null; then
            checkpoint_total="$(
                jq '[.report.files[].mutations[]?] | length' "${checkpoint}"
            )"
            if [[ "${checkpoint_total}" -gt 0 ]] || reviewed_zero_mutant; then
                checkpoint_tmp="$(mktemp "${checkpoint}.tmp.XXXXXX")"
                jq \
                    --arg revision "$(git -C "${root}" rev-parse HEAD)" \
                    --arg digest "${package_input_digest}" '
                    .schema_version = 3
                    | .validated_revision = $revision
                    | .legacy_module_gate_input_digest = .gate_input_digest
                    | .gate_input_digest = $digest
                ' "${checkpoint}" >"${checkpoint_tmp}"
                mv "${checkpoint_tmp}" "${checkpoint}"
                reports+=("${checkpoint}")
                write_aggregate
                printf '[%s] %s migrated content-identical mutation evidence\n' \
                    "${module}" "${target}"
                continue
            fi
        fi
        checkpoint_revision="$(jq -r '.execution_revision' "${checkpoint}")"
        if historical_input_digest="$(
            historical_package_digest \
                "${checkpoint_revision}" "${package_directory}"
        )" && [[ "${historical_input_digest}" == "${package_input_digest}" ]]; then
            checkpoint_total="$(
                jq '[.report.files[].mutations[]?] | length' "${checkpoint}"
            )"
            if [[ "${checkpoint_total}" -gt 0 ]] || reviewed_zero_mutant; then
                checkpoint_tmp="$(mktemp "${checkpoint}.tmp.XXXXXX")"
                jq \
                    --arg revision "$(git -C "${root}" rev-parse HEAD)" \
                    --arg digest "${package_input_digest}" '
                    .schema_version = 3
                    | .validated_revision = $revision
                    | .legacy_module_gate_input_digest = .gate_input_digest
                    | .historical_package_input_digest = $digest
                    | .gate_input_digest = $digest
                ' "${checkpoint}" >"${checkpoint_tmp}"
                mv "${checkpoint_tmp}" "${checkpoint}"
                reports+=("${checkpoint}")
                write_aggregate
                printf '[%s] %s migrated historically identical mutation evidence\n' \
                    "${module}" "${target}"
                continue
            fi
        fi
    fi

    printf '[%s] mutation package %s\n' "${module}" "${target}"
    ensure_shared_coverage
    status=0
    if [[ "${discover_only}" -eq 1 ]]; then
        if [[ -n "${modfile}" ]]; then
            GOCACHE="${active_build_cache}" GOWORK=off \
                GOLIB_GREMLINS_COVERAGE_PROFILE="${shared_coverage}" \
                GOLIB_GREMLINS_COVERAGE_ELAPSED="${shared_coverage_elapsed}" \
                GOFLAGS="-modfile=${modfile} -mod=mod" \
                "${gremlins_binary}" "${mutation_arguments[@]}" \
                >"${run_directory}/${slug}.log" 2>&1 || status=$?
        else
            GOCACHE="${active_build_cache}" GOWORK=off \
                GOLIB_GREMLINS_COVERAGE_PROFILE="${shared_coverage}" \
                GOLIB_GREMLINS_COVERAGE_ELAPSED="${shared_coverage_elapsed}" \
                "${gremlins_binary}" "${mutation_arguments[@]}" \
                >"${run_directory}/${slug}.log" 2>&1 || status=$?
        fi
    elif [[ -n "${modfile}" ]]; then
        GOCACHE="${active_build_cache}" GOWORK=off \
            GOLIB_GREMLINS_COVERAGE_PROFILE="${shared_coverage}" \
            GOLIB_GREMLINS_COVERAGE_ELAPSED="${shared_coverage_elapsed}" \
            GOFLAGS="-modfile=${modfile} -mod=mod" \
            "${gremlins_binary}" "${mutation_arguments[@]}" || status=$?
    else
        GOCACHE="${active_build_cache}" GOWORK=off \
            GOLIB_GREMLINS_COVERAGE_PROFILE="${shared_coverage}" \
            GOLIB_GREMLINS_COVERAGE_ELAPSED="${shared_coverage_elapsed}" \
            "${gremlins_binary}" "${mutation_arguments[@]}" || status=$?
    fi
    cleanup
    if [[ "${status}" -ne 0 ]]; then
        if [[ "${discover_only}" -eq 1 ]]; then
            cat "${run_directory}/${slug}.log" >&2
        fi
        printf 'mutation tool failed for %s %s (status %s)\n' \
            "${module}" "${target}" "${status}" >&2
        exit 1
    fi
    if [[ "${discover_only}" -eq 1 && ! -s "${package_report}" ]]; then
        source_directory="${module}"
        if [[ "${package_directory}" != "." ]]; then
            source_directory="${module}/${package_directory}"
        fi
        source_digest="$("${root}/scripts/package-source-digest.sh" \
            "${source_directory}")"
        printf 'ZERO\t%s\t%s\t%s\n' \
            "${module}" "${package_directory}" "${source_digest}"
        continue
    fi
    if [[ ! -s "${package_report}" ]]; then
        if reviewed_zero_mutant; then
            printf '{"files":[]}\n' >"${package_report}"
        else
            printf 'mutation report is missing for %s %s\n' \
                "${module}" "${target}" >&2
            exit 1
        fi
    fi
    if ! jq -e '.files | type == "array"' "${package_report}" >/dev/null; then
        printf 'mutation report is malformed for %s %s\n' "${module}" "${target}" >&2
        exit 1
    fi
    total="$(jq '[.files[].mutations[]] | length' "${package_report}")"
    if [[ "${discover_only}" -eq 1 ]]; then
        printf 'MUTANTS\t%s\t%s\t%s\n' \
            "${module}" "${package_directory}" "${total}"
        continue
    fi
    if [[ "${total}" -eq 0 ]]; then
        if ! reviewed_zero_mutant; then
            printf 'mutation report unexpectedly contains no reviewed mutants for %s %s\n' \
                "${module}" "${target}" >&2
            exit 1
        fi
    fi
    unexpected="$(jq '[.files[].mutations[] | select(.status != "KILLED")] | length' \
        "${package_report}")"
    if [[ "${status}" -ne 0 || "${unexpected}" -ne 0 ]]; then
        jq -r '.files[] as $file | $file.mutations[] | select(.status != "KILLED") | "\($file.file_name):\(.line):\(.column) \(.type) \(.status)"' \
            "${package_report}" >&2
        printf 'mutation gate failed for %s %s (tool status %s)\n' \
            "${module}" "${target}" "${status}" >&2
        exit 1
    fi
    current_input_digest="$(
        "${root}/scripts/gate-input-digest.sh" \
            mutation "${module}" "${package_directory}"
    )"
    if [[ "${current_input_digest}" != "${package_input_digest}" ]]; then
        printf 'mutation inputs changed while running %s %s\n' \
            "${module}" "${target}" >&2
        exit 1
    fi
    checkpoint_tmp="$(mktemp "${checkpoint}.tmp.XXXXXX")"
    jq \
        --arg module "${module}" \
        --arg package "${package_directory}" \
        --arg execution_revision "${execution_revision}" \
        --arg validated_revision "$(git -C "${root}" rev-parse HEAD)" \
        --arg gate_input_digest "${package_input_digest}" \
        --arg gremlins_version "${GREMLINS_VERSION}" \
        --argjson environment "${environment_identity}" '
        {
            schema_version: 3,
            module: $module,
            package: $package,
            execution_revision: $execution_revision,
            validated_revision: $validated_revision,
            gate_input_digest: $gate_input_digest,
            gremlins_version: $gremlins_version,
            environment: $environment,
            report: .
        }
    ' "${package_report}" >"${checkpoint_tmp}"
    mv "${checkpoint_tmp}" "${checkpoint}"
    reports+=("${checkpoint}")
    write_aggregate
    if [[ "${total}" -eq 0 ]]; then
        printf '[%s] %s has a hash-pinned zero-viable-mutant review\n' \
            "${module}" "${target}"
    else
        printf '[%s] %s killed %s/%s viable mutants\n' \
            "${module}" "${target}" "${total}" "${total}"
    fi
done

if [[ "${discover_only}" -eq 1 ]]; then
    exit 0
fi

if ! jq -e \
    '
        .schema_version == 3 and
        .complete == true and
        ([.packages[] | select(.schema_version != 3)] | length == 0)
    ' "${report}" >/dev/null; then
    printf 'aggregate mutation report is incomplete for %s\n' "${module}" >&2
    exit 1
fi
