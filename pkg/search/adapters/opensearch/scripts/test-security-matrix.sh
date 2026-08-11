#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$script_dir/opensearch-images.env"
. "$script_dir/docker-test-ownership.sh"

opensearch_run_id="$(date +%s)-$$-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
owner_label="$opensearch_owner_label_key=$opensearch_run_id"
first=''
second=''
ca_dir=''
image=''
remove_image=0

remove_ca_dir() {
	if [ -z "$ca_dir" ]; then
		return
	fi
	case "$(basename -- "$ca_dir")" in
	golib-opensearch-secure.*) ;;
	*)
		printf 'refusing to remove unexpected secure test directory: %s\n' "$ca_dir" >&2
		return 1
		;;
	esac
	find "$ca_dir" -depth -delete
	ca_dir=''
}

cleanup() {
	if [ -n "$first" ]; then opensearch_remove_owned_if_present container "$first"; fi
	if [ -n "$second" ]; then opensearch_remove_owned_if_present container "$second"; fi
	remove_ca_dir
	if [ "$remove_image" -eq 1 ] && [ -n "$image" ]; then
		docker image rm "$image" >/dev/null 2>&1 || true
		remove_image=0
		image=''
	fi
}
trap cleanup EXIT HUP INT TERM

wait_secure_node() {
	container=$1
	password=$2
	port=$(docker port "$container" 9200/tcp | sed -n 's/.*://p')
	for _ in $(seq 1 180); do
		if curl --connect-timeout 2 --max-time 5 --fail --silent --insecure \
			--user "admin:$password" "https://127.0.0.1:$port/" >/dev/null; then
			printf '%s\n' "$port"
			return 0
		fi
		sleep 1
	done
	docker logs "$container" >&2
	return 1
}

create_security_role() {
	port=$1
	admin_password=$2
	role_name=$3
	role_body=$4
	printf '%s' "$role_body" |
		curl --connect-timeout 2 --max-time 10 --fail --silent --show-error --insecure \
			--user "admin:$admin_password" -H 'Content-Type: application/json' \
			--request PUT --data-binary @- \
			"https://127.0.0.1:$port/_plugins/_security/api/roles/$role_name" \
			>/dev/null
}

create_security_user() {
	port=$1
	admin_password=$2
	username=$3
	password=$4
	role_name=$5
	printf '{"password":"%s","opendistro_security_roles":["%s"]}' "$password" "$role_name" |
		curl --connect-timeout 2 --max-time 10 --fail --silent --show-error --insecure \
			--user "admin:$admin_password" -H 'Content-Type: application/json' \
			--request PUT --data-binary @- \
			"https://127.0.0.1:$port/_plugins/_security/api/internalusers/$username" \
			>/dev/null
}

configure_least_privilege_users() {
	port=$1
	admin_password=$2
	user_password=$3
	runtime_role='golib_tenant_a_runtime'
	operator_role='golib_cluster_operator'
	create_security_role "$port" "$admin_password" "$runtime_role" \
		'{"cluster_permissions":["indices:data/write/bulk"],"index_permissions":[{"index_patterns":["golib-secure-tenant-a-*"],"allowed_actions":["read","write","indices:admin/aliases/get"]}],"tenant_permissions":[]}'
	create_security_role "$port" "$admin_password" "$operator_role" \
		'{"cluster_permissions":["cluster_monitor"],"index_permissions":[{"index_patterns":["golib-secure-recovery-*"],"allowed_actions":["manage"]}],"tenant_permissions":[]}'
	create_security_user "$port" "$admin_password" 'golib_runtime' "$user_password" "$runtime_role"
	create_security_user "$port" "$admin_password" 'golib_operator' "$user_password" "$operator_role"
}

seed_tenant_indices() {
	port=$1
	admin_password=$2
	for tenant in a b; do
		physical="golib-secure-tenant-$tenant-documents-v1-$opensearch_run_id"
		alias="golib-secure-tenant-$tenant-documents-$opensearch_run_id"
		printf '%s' '{"settings":{"number_of_shards":1,"number_of_replicas":0},"mappings":{"dynamic":"strict","properties":{"value":{"type":"keyword"}}}}' |
			curl --connect-timeout 2 --max-time 10 --fail --silent --show-error --insecure \
				--user "admin:$admin_password" -H 'Content-Type: application/json' \
				--request PUT --data-binary @- "https://127.0.0.1:$port/$physical" >/dev/null
		printf '{"actions":[{"add":{"index":"%s","alias":"%s","is_write_index":true}}]}' "$physical" "$alias" |
			curl --connect-timeout 2 --max-time 10 --fail --silent --show-error --insecure \
				--user "admin:$admin_password" -H 'Content-Type: application/json' \
				--request POST --data-binary @- "https://127.0.0.1:$port/_aliases" >/dev/null
	done
}

run_secure_node() {
	name=$1
	node_name=$2
	password=$3
	image=$4
	docker run -d --name "$name" --label "$owner_label" -p 127.0.0.1::9200 \
		--cpus=1 --memory=1g --pids-limit=512 --ulimit nofile=1024:1024 \
		-e discovery.type=single-node \
		-e cluster.default_number_of_replicas=0 \
		-e plugins.security.restapi.roles_enabled=all_access,security_rest_api_access \
		-e "cluster.name=golib-secure-$opensearch_run_id-$node_name" \
		-e "node.name=$node_name" \
		-e "OPENSEARCH_INITIAL_ADMIN_PASSWORD=$password" \
		-e OPENSEARCH_JAVA_OPTS='-Xms512m -Xmx512m' \
		"$image" >/dev/null
}

for release in ${OPENSEARCH_SECURITY_RELEASES:-old new}; do
	case "$release" in
	old) version=$opensearch_old_version; digest=$opensearch_old_digest ;;
	new) version=$opensearch_new_version; digest=$opensearch_new_digest ;;
	esac
	image="$opensearch_image_repository@$digest"
	remove_image=0
	if ! docker image inspect "$image" >/dev/null 2>&1; then remove_image=1; fi
	old_password="Aa1!$(openssl rand -hex 16)"
	new_password="Bb2@$(openssl rand -hex 16)"
	runtime_username='golib_runtime'
	operator_username='golib_operator'
	first="golib-opensearch-secure-a-$version-$opensearch_run_id"
	second="golib-opensearch-secure-b-$version-$opensearch_run_id"
	run_secure_node "$first" secure-a "$old_password" "$image"
	run_secure_node "$second" secure-b "$new_password" "$image"
	first_port=$(wait_secure_node "$first" "$old_password")
	second_port=$(wait_secure_node "$second" "$new_password")
	configure_least_privilege_users "$first_port" "$old_password" "$old_password"
	configure_least_privilege_users "$second_port" "$new_password" "$new_password"
	seed_tenant_indices "$first_port" "$old_password"
	ca_dir="${TMPDIR:-/tmp}/golib-opensearch-secure.$opensearch_run_id"
	mkdir "$ca_dir"
	docker cp "$first:/usr/share/opensearch/config/root-ca.pem" "$ca_dir/root-ca.pem"
	OPENSEARCH_TLS_SERVER_NAME=node-0.example.com \
	OPENSEARCH_FIRST_DIAL_ADDRESS="127.0.0.1:$first_port" \
	OPENSEARCH_SECOND_DIAL_ADDRESS="127.0.0.1:$second_port" \
	OPENSEARCH_CA_FILE="$ca_dir/root-ca.pem" \
	OPENSEARCH_USERNAME="$runtime_username" \
	OPENSEARCH_OLD_PASSWORD="$old_password" \
	OPENSEARCH_NEW_PASSWORD="$new_password" \
	OPENSEARCH_EXPECTED_VERSION="$version" \
	OPENSEARCH_TENANT_A_ALIAS="golib-secure-tenant-a-documents-$opensearch_run_id" \
	OPENSEARCH_TENANT_A_PHYSICAL="golib-secure-tenant-a-documents-v1-$opensearch_run_id" \
	OPENSEARCH_TENANT_B_PHYSICAL="golib-secure-tenant-b-documents-v1-$opensearch_run_id" \
		go test -tags=integration -run '^TestRealOpenSearchLeastPrivilegeTenantIsolation$' -count=1 .
	OPENSEARCH_TLS_SERVER_NAME=node-0.example.com \
	OPENSEARCH_FIRST_DIAL_ADDRESS="127.0.0.1:$first_port" \
	OPENSEARCH_SECOND_DIAL_ADDRESS="127.0.0.1:$second_port" \
	OPENSEARCH_CA_FILE="$ca_dir/root-ca.pem" \
	OPENSEARCH_OPERATOR_USERNAME="$operator_username" \
	OPENSEARCH_OLD_PASSWORD="$old_password" \
	OPENSEARCH_NEW_PASSWORD="$new_password" \
	OPENSEARCH_EXPECTED_VERSION="$version" \
		go test -tags=integration -run '^TestRealOpenSearchSecureTLSCredentialRotationDNSAndRecovery$' -count=1 .
	opensearch_assert_container_limits "$first"
	opensearch_assert_container_limits "$second"
	opensearch_remove_owned container "$first"
	opensearch_remove_owned container "$second"
	remove_ca_dir
	if [ "$remove_image" -eq 1 ]; then
		docker image rm "$image" >/dev/null
		remove_image=0
	fi
	image=''
done
