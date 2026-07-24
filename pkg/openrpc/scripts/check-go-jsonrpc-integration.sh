#!/usr/bin/env bash
set -euo pipefail

openrpc_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
jsonrpc_root="${openrpc_root}/../jsonrpc"
if [[ ! -f "${jsonrpc_root}/go.mod" ]]; then
    echo "sibling jsonrpc checkout is required for integration verification" >&2
    exit 1
fi

integration_dir=$(mktemp -d)
trap 'rm -rf "${integration_dir}"' EXIT

cat > "${integration_dir}/go.mod" <<EOF
module integration.test/openrpcjsonrpc

go 1.26.5

require (
    github.com/faustbrian/golib/pkg/jsonrpc v0.0.0
    github.com/faustbrian/golib/pkg/openrpc v0.0.0
)

replace github.com/faustbrian/golib/pkg/jsonrpc => ${jsonrpc_root}
replace github.com/faustbrian/golib/pkg/openrpc => ${openrpc_root}
EOF

cat > "${integration_dir}/integration_test.go" <<'EOF'
package integration_test

import (
    "context"
    "encoding/json"
    "testing"

    gojsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
    "github.com/faustbrian/golib/pkg/openrpc/discovery"
    openrpcjsonrpc "github.com/faustbrian/golib/pkg/openrpc/jsonrpc"
    openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
)

func TestRegisterDiscoveryWithGoJSONRPCRegistry(t *testing.T) {
    parsed, err := openrpcparse.Decode([]byte(`{
        "openrpc":"1.4.1",
        "info":{"title":"Integration","version":"1"},
        "methods":[]
    }`), openrpcparse.DefaultOptions())
    if err != nil {
        t.Fatal(err)
    }
    service, err := discovery.NewService(discovery.Static(parsed.Document()), nil)
    if err != nil {
        t.Fatal(err)
    }
    registry := gojsonrpc.NewRegistry()
    if err := openrpcjsonrpc.RegisterDiscovery[gojsonrpc.Handler](registry, service); err != nil {
        t.Fatal(err)
    }
    handler, found := registry.Lookup(discovery.MethodName)
    if !found {
        t.Fatal("rpc.discover was not registered")
    }
    result, err := handler(context.Background(), json.RawMessage(`[]`))
    if err != nil {
        t.Fatal(err)
    }
    raw, ok := result.(json.RawMessage)
    if !ok || !json.Valid(raw) {
        t.Fatalf("result = %#v", result)
    }
}
EOF

(
    cd "${integration_dir}"
    go mod tidy
    go test ./... -count=1
)
