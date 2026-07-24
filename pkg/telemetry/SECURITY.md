# Security policy

## Reporting

Do not open public issues for suspected vulnerabilities or telemetry data
leaks. Use GitHub private vulnerability reporting for this repository. Include
the affected version, configuration, reproduction, expected trust boundary,
and whether secrets or untrusted identifiers were exported.

## Supported versions

Until v1, only the latest released minor receives security fixes. After v1,
the current major and the immediately previous supported minor receive fixes
according to the compatibility matrix.

## Security model

`telemetry` treats inbound headers, HTTP metadata, SQL, cache keys, queue
messages, error strings, and payloads as untrusted. Default instrumentation
does not export them. Baggage is disabled until a trusted allow-list and bounds
are configured. Export credentials remain caller-owned configuration.

The project assumes no production access and contains no remote management,
dynamic code loading, cgo, `unsafe`, or `go:linkname` paths.
