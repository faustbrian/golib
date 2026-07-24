#!/bin/sh
set -eu

go test -run '^$' -bench Benchmark -benchmem -benchtime=100ms ./basic ./apikey ./authhttp
(cd jwt && go test -run '^$' -bench Benchmark -benchmem -benchtime=100ms ./...)
(cd oidc && go test -run '^$' -bench Benchmark -benchmem -benchtime=100ms ./...)
