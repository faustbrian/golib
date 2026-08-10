.PHONY: adversarial conformance differential docs fault-injection interoperability stress

conformance: interoperability adversarial differential fault-injection

interoperability:
	go test ./... -run 'RFC7515|RFC7520|SupportedAlgorithmAndKeyMatrix|GolangJWT.*Interoperability|Differential' -count=1

adversarial:
	go test ./... -run 'Rejects|Hostile|Boundary|Bounds|Ownership' -count=1

differential:
	go test ./... -run 'Differential' -count=1

fault-injection:
	go test ./... -run 'Remote.*(Failure|Outage|Redirect|Timeout|Cancellation|Rotation|Refresh|Close|Fleet)' -count=1

stress:
	go test -race ./... -run 'Remote.*(Refresh|Close|Concurrent|Race)' -count=20

docs:
	go test ./... -run '^Example' -count=1
	test -s README.md
	test -s docs/api.md
	test -s docs/hardening.md
	test -s specification/README.md
	test -s specification/manifest.tsv
