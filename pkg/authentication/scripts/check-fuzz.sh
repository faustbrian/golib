#!/bin/sh
set -eu

duration=${FUZZ_TIME:-10s}

go test . -run '^$' -fuzz '^FuzzPrincipalClaims$' -fuzztime="$duration"
go test ./authhttp -run '^$' -fuzz '^FuzzAuthorizationExtraction$' -fuzztime="$duration"
go test ./authhttp -run '^$' -fuzz '^FuzzCredentialHeaderSet$' -fuzztime="$duration"
go test ./authhttp -run '^$' -fuzz '^FuzzChallengeFormatting$' -fuzztime="$duration"
go test ./authhttp -run '^$' -fuzz '^FuzzBearerQueryExtraction$' -fuzztime="$duration"
(cd jwt && go test . -run '^$' -fuzz '^FuzzInspectCompactJWT$' -fuzztime="$duration")
(cd oidc && go test . -run '^$' -fuzz '^FuzzInspectCompactToken$' -fuzztime="$duration")
(cd oidc && go test . -run '^$' -fuzz '^FuzzRemoteURL$' -fuzztime="$duration")
