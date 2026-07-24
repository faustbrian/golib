#!/usr/bin/env bash
set -euo pipefail

root="$(pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

repositories=(service http-middleware jsonrpc)
revisions=(
  258473c234466be0958762b1275d161929754503
  85c4cac44c89f30c53f5281dbde4f271479f8668
  5c344af1ab723fe0bd8c2f6fb1c64439c9387df8
)
for index in "${!repositories[@]}"; do
  repository="${repositories[$index]}"
  local_path="$root/../$repository"
  if [[ "${ROUTER_INTEGRATION_REMOTE_ONLY:-0}" != 1 && -f "$local_path/go.mod" ]]; then
    ln -s "$local_path" "$temporary/$repository"
    continue
  fi
  git init -q "$temporary/$repository"
  git -C "$temporary/$repository" fetch -q --depth=1 \
    "https://github.com/faustbrian/${repository}.git" "${revisions[$index]}"
  git -C "$temporary/$repository" checkout -q FETCH_HEAD
done

mkdir "$temporary/integration"
cat >"$temporary/integration/go.mod" <<EOF
module routerintegration

go 1.26.5

require (
  github.com/faustbrian/golib/pkg/http-middleware v0.0.0
  github.com/faustbrian/golib/pkg/jsonrpc v0.0.0
  github.com/faustbrian/golib/pkg/router v0.0.0
  github.com/faustbrian/golib/pkg/service v0.0.0
)

replace github.com/faustbrian/golib/pkg/router => $root
replace github.com/faustbrian/golib/pkg/service => $temporary/service
replace github.com/faustbrian/golib/pkg/http-middleware => $temporary/http-middleware
replace github.com/faustbrian/golib/pkg/jsonrpc => $temporary/jsonrpc
EOF
cat >"$temporary/integration/integration_test.go" <<'EOF'
package integration_test

import (
  "context"
  "encoding/json"
  "net"
  "net/http"
  "net/http/httptest"
  "strings"
  "testing"

  middleware "github.com/faustbrian/golib/pkg/http-middleware"
  jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
  router "github.com/faustbrian/golib/pkg/router"
  "github.com/faustbrian/golib/pkg/service/serverhttp"
)

func TestOwnedHTTPBoundaries(t *testing.T) {
  registry := jsonrpc.NewRegistry()
  if err := registry.Register("ping", func(context.Context, json.RawMessage) (any, error) {
    return "pong", nil
  }); err != nil { t.Fatal(err) }
  rpc := jsonrpc.NewHTTPHandler(jsonrpc.NewDispatcher(registry))
  chain, err := middleware.New(func(next http.Handler) http.Handler { return next })
  if err != nil { t.Fatal(err) }
  wrappedRPC, err := chain.Handler(rpc)
  if err != nil { t.Fatal(err) }

  builder := router.New()
  if err := builder.Mount("/rpc", wrappedRPC, router.MountOptions{StripPrefix: true}); err != nil { t.Fatal(err) }
  if err := builder.Register(router.Route{
    Name: "track.webhook", Methods: []string{http.MethodPost}, Path: "/webhooks/track",
    Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
  }); err != nil { t.Fatal(err) }
  compiled, err := builder.Compile()
  if err != nil { t.Fatal(err) }

  var serverConstructor func(net.Listener, http.Handler, ...serverhttp.Option) (*serverhttp.Server, error) = serverhttp.New
  _ = serverConstructor
  request := httptest.NewRequest(http.MethodPost, "/rpc/", strings.NewReader(`{"jsonrpc":"2.0","method":"ping","id":1}`))
  request.Header.Set("Content-Type", "application/json")
  response := httptest.NewRecorder()
  compiled.ServeHTTP(response, request)
  if response.Code != http.StatusOK { t.Fatalf("RPC status: %d", response.Code) }
}
EOF
(cd "$temporary/integration" && go mod tidy && go test -race ./...)
