# Sanitized HTTP test fixtures

The fixture transports support deterministic vendor contract tests without
persisting live credentials or depending on a live service. Replay is strict
and ordered: each request must match the next interaction, and `Verify` fails
when a script leaves interactions unused.

## Script a contract

Use `NewScriptedTransport` for fixtures authored in code. Requests are matched
by case-sensitive method, canonical origin, normalized path, sorted query, an
optional selected-header allowlist, and either a bounded raw body or SHA-256
body digest. Extension methods therefore remain distinct from differently
cased methods.

Responses can include selected headers, trailers, an explicit content length,
and a bounded body. Stable failure categories model timeout, cancellation,
transport failure, malformed response, and truncated response body behavior
without storing an original error string.

```go
script, err := httpclient.NewScriptedTransport(httpclient.Fixture{
	SchemaVersion: httpclient.FixtureSchemaVersion,
	RecordedAt:    time.Now().UTC(),
	Interactions: []httpclient.FixtureInteraction{{
		Request: httpclient.FixtureRequest{
			Method: http.MethodGet,
			URL:    "https://api.example.test/widgets?page=1",
		},
		Response: httpclient.FixtureResponse{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: []byte(`{"items":[]}`),
		},
	}},
}, httpclient.ReplayOptions{})
if err != nil {
	return err
}

client := &http.Client{Transport: script}
// Exercise the generated or handwritten vendor client.
if err := script.Verify(); err != nil {
	return err
}
```

Replay returns immutable response snapshots. An unmatched request does not
consume the expected interaction, so the resulting mismatch can be corrected
and retried deterministically.

## Record safely

`NewRecorderTransport` wraps an explicit base transport. Capture is bounded by
`MaximumBodyBytes` and a finite internal interaction limit. The recorder
preserves the live request and response while storing only canonical,
sanitized data.

Safety defaults are deliberately restrictive:

- Authorization, proxy authorization, cookies, API-key headers, and common
  token headers are never stored.
- Common secret query names are replaced before storage. Add vendor-specific
  names with `RedactedQueryParameters`.
- Request bodies are never persisted. Only a SHA-256 digest used for matching
  is retained; do not treat that digest as protection for guessable input.
- Response bodies are omitted unless `ResponseBodyRedactor` is explicitly
  supplied. The redactor receives a bounded copy and must return sanitized
  bytes within the same bound.
- Response headers and trailers use explicit allowlists. Volatile fields are
  omitted, and `SensitiveHeaders` adds vendor-specific prohibitions.
- Transport failures retain only a stable category, never the live error text.

```go
recorder, err := httpclient.NewRecorderTransport(base, httpclient.RecorderOptions{
	MatchHeaders:            []string{"X-API-Version"},
	RedactedQueryParameters: []string{"vendor_signature"},
	ResponseHeaders:         []string{"Content-Type"},
	ResponseTrailers:        []string{"X-Checksum"},
	SensitiveHeaders:        []string{"X-Vendor-Token"},
	MaximumBodyBytes:        1 << 20,
	TTL:                     30 * 24 * time.Hour,
	ResponseBodyRedactor: httpclient.FixtureBodyRedactorFunc(
		func(body []byte) ([]byte, error) {
			return redactVendorPayload(body)
		},
	),
})
```

Review recorded output before committing it. Recording is a development and
test workflow, not a production traffic sink.

## Persist and migrate

`RecorderTransport.WriteFixture` emits deterministic versioned JSON.
`ReadFixture` enforces a file-size bound, rejects unknown fields and trailing
documents, rejects persisted raw request bodies, checks expiry, and validates
the complete replay contract. Expired fixtures fail unless `AllowExpired` is
explicitly enabled.

Schema upgrades are opt-in through `FixtureLoadOptions.Migrations`. Register a
migrator only for a known old schema and return a current
`FixtureSchemaVersion`; migration failures and panics are contained. This keeps
fixture compatibility visible in code review instead of silently accepting an
ambiguous document.

For contract coverage, combine three layers:

1. Hand-authored scripts for protocol branches and stable failure modes.
2. `httptest.Server` checks for real framing, trailers, redirects, and
   connection behavior.
3. Sanitized recordings for representative vendor payload compatibility.
