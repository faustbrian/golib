.PHONY: conformance interoperability

conformance:
	GOWORK=off go test ./... -run '^TestOpenIDConnectCore' -count=1

interoperability:
	GOWORK=off go test ./... -run '^TestRepresentativeProviderMetadataProfiles$$' -count=1
