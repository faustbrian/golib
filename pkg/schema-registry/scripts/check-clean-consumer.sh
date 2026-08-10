#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
consumer=$(mktemp -d)
go_cache=$(mktemp -d)
module_cache=$(mktemp -d)
go_path=$(mktemp -d)
cleanup() {
	chmod -R u+w "$consumer" "$go_cache" "$module_cache" "$go_path" 2>/dev/null || true
	find "$consumer" -depth -delete
	find "$go_cache" -depth -delete
	find "$module_cache" -depth -delete
	find "$go_path" -depth -delete
}
trap cleanup EXIT

cd "$consumer"
export GOWORK=off GOCACHE="$go_cache" GOMODCACHE="$module_cache" GOPATH="$go_path"
go mod init example.com/schema-registry-consumer >/dev/null
go mod edit -go=1.26.5 \
	-require=github.com/faustbrian/golib/pkg/schema-registry@v0.0.0 \
	-require=github.com/faustbrian/golib/pkg/schema-registry/providers/confluent@v0.0.0 \
	-require=github.com/faustbrian/golib/pkg/schema-registry/providers/glue@v0.0.0 \
	-replace="github.com/faustbrian/golib/pkg/schema-registry=${root}" \
	-replace="github.com/faustbrian/golib/pkg/schema-registry/providers/confluent=${root}/providers/confluent" \
	-replace="github.com/faustbrian/golib/pkg/schema-registry/providers/glue=${root}/providers/glue"
cat > consumer_test.go <<'EOF'
package consumer_test

import (
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
	registryprotobuf "github.com/faustbrian/golib/pkg/schema-registry/formats/protobuf"
	registryconfluent "github.com/faustbrian/golib/pkg/schema-registry/providers/confluent"
	registryglue "github.com/faustbrian/golib/pkg/schema-registry/providers/glue"
)

func TestPublicModulesCompile(t *testing.T) {
	if schemaregistry.DefaultCompileLimits().MaxSchemaBytes <= 0 ||
		registryavro.New(1) == nil || registryconfluent.ProviderName == "" || registryglue.ProviderName == "" {
		t.Fatal("public contract is unavailable")
	}
	_, _ = registryjsonschema.New(registryjsonschema.Config{})
	_, _ = registryprotobuf.New(registryprotobuf.Config{})
}
EOF
go mod tidy
go test ./...
