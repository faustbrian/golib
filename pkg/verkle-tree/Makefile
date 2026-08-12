.PHONY: conformance interoperability

conformance:
	GOWORK=off go test ./internal/... -count=1

interoperability:
	./scripts/check-interoperability.sh
