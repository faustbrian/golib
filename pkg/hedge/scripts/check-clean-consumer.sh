#!/usr/bin/env bash
set -euo pipefail

module=github.com/faustbrian/golib/pkg/hedge
source_root=$(pwd -P)
temp_root=${TMPDIR:-/tmp}
workspace=$(mktemp -d "${temp_root%/}/hedge-consumer.XXXXXX")
case "$workspace" in
	"${temp_root%/}"/hedge-consumer.*) ;;
	*) echo 'unexpected consumer workspace' >&2; exit 1 ;;
esac
cleanup_workspace() {
	if [[ -n "${workspace:-}" && -d "$workspace" && ! -L "$workspace" ]]; then
		rm -rf -- "${workspace:?}"
	fi
}
trap cleanup_workspace EXIT

cd "$workspace"
GOWORK=off go mod init example.com/hedge-consumer >/dev/null
GOWORK=off go mod edit -go=1.26.6
GOWORK=off go mod edit -replace "${module}=${source_root}"
GOWORK=off go get "${module}@v0.0.0"

cat > consumer_test.go <<'EOF'
package consumer_test

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestPublicConsumer(t *testing.T) {
	budget, err := hedge.NewOutstandingBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := hedge.NewPolicy(hedge.Config[string]{
		MaxHedges:      1,
		ReplaySafe:     true,
		Delay:          time.Hour,
		TotalTimeout:   time.Second,
		CleanupTimeout: time.Second,
		Clock:          hedge.RealClock{},
		Budget:         budget,
		Classifier: hedge.ClassifyFunc[string](func(context.Context, hedge.AttemptResult[string]) (hedge.Classification, error) {
			return hedge.ClassificationSuccess, nil
		}),
		Disposer:           hedge.DisposeFunc[string](func(context.Context, string) error { return nil }),
		Resource:           "clean-consumer",
		FactoryFailureMode: hedge.FactoryFailureStop,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, report, err := hedge.Do(context.Background(), policy, hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "ok", nil }, "endpoint-a", nil
	}))
	if err != nil || value != "ok" || report.WinnerOrdinal != 0 || report.AttemptsStarted != 1 {
		t.Fatalf("Do() = (%q, %+v, %v)", value, report, err)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
EOF

GOWORK=off go test ./...
