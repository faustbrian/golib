.PHONY: conformance interoperability

conformance:
	GOWORK=off go test ./... -count=1

interoperability:
	../../../scripts/test-oidc-keycloak-interoperability.sh
