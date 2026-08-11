#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$script_dir/opensearch-images.env"

case "$opensearch_image_repository" in
opensearchproject/opensearch) ;;
*)
	printf 'unexpected OpenSearch image repository: %s\n' "$opensearch_image_repository" >&2
	exit 1
	;;
esac

validate_release() {
	label=$1
	version=$2
	digest=$3

	case "$version" in
	[0-9]*.[0-9]*.[0-9]*) ;;
	*)
		printf '%s OpenSearch version is invalid: %s\n' "$label" "$version" >&2
		exit 1
		;;
	esac
	hex=${digest#sha256:}
	if [ "$hex" = "$digest" ] || [ "${#hex}" -ne 64 ]; then
		printf '%s OpenSearch digest is not a sha256 identity\n' "$label" >&2
		exit 1
	fi
	case "$hex" in
	*[!0-9a-f]*)
		printf '%s OpenSearch digest contains non-hexadecimal characters\n' "$label" >&2
		exit 1
		;;
	esac
}

validate_release old "$opensearch_old_version" "$opensearch_old_digest"
validate_release new "$opensearch_new_version" "$opensearch_new_digest"

if [ "$opensearch_old_version" != '2.19.6' ] ||
	[ "$opensearch_old_digest" != 'sha256:8690b204fe914c60ca76d451ac73bc0481e034d32d3779944c8caca56a2b003f' ] ||
	[ "$opensearch_new_version" != '3.8.0' ] ||
	[ "$opensearch_new_digest" != 'sha256:bcc1797519726ceb6d651d4a3e60b7c30da91793914a8dfe75fd441d4f641509' ]; then
	printf 'OpenSearch release labels or immutable identities differ from the supported matrix\n' >&2
	exit 1
fi

if [ "$opensearch_old_version" = "$opensearch_new_version" ] ||
	[ "$opensearch_old_digest" = "$opensearch_new_digest" ]; then
	printf 'OpenSearch compatibility releases must have distinct labels and identities\n' >&2
	exit 1
fi

for script in test-version-matrix.sh test-rolling-upgrade.sh test-security-matrix.sh; do
	if ! grep -F '. "$script_dir/opensearch-images.env"' "$script_dir/$script" >/dev/null; then
		printf '%s does not load the canonical OpenSearch image identities\n' "$script" >&2
		exit 1
	fi
	if grep -E 'opensearchproject/opensearch:[0-9]' "$script_dir/$script" >/dev/null; then
		printf '%s contains a mutable tagged OpenSearch image reference\n' "$script" >&2
		exit 1
	fi
done

if ! grep -F 'image="$opensearch_image_repository@$digest"' "$script_dir/test-version-matrix.sh" >/dev/null ||
	! grep -F 'image="$opensearch_image_repository@$digest"' "$script_dir/test-security-matrix.sh" >/dev/null ||
	! grep -F 'old_image="$opensearch_image_repository@$opensearch_old_digest"' "$script_dir/test-rolling-upgrade.sh" >/dev/null ||
	! grep -F 'new_image="$opensearch_image_repository@$opensearch_new_digest"' "$script_dir/test-rolling-upgrade.sh" >/dev/null; then
	printf 'OpenSearch execution scripts do not consume the canonical image identities\n' >&2
	exit 1
fi
