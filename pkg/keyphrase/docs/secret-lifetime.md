# Secret lifetime

Prefer `keyphrase.Secret`, keep ownership narrow, avoid converting results to
strings, and call `Secret.Clear` as soon as the value is no longer needed. Its
formatting, structured logging, and standard text/JSON encoding are redacted.
Mnemonic and parsed-passphrase observation paths use the same defaults.
Caller-owned buffer APIs avoid an extra long-lived caller allocation when
practical and do not expose partial values on failure.

Clearing is best effort only. Go may create compiler temporaries, stack copies,
heap copies, immutable strings, interface copies, garbage-collector copies, and
runtime diagnostics. The operating system may retain pages in swap, hibernation,
core dumps, or crash reports. Downstream APIs, terminals, clipboards, telemetry,
and logs may create additional copies. This library cannot prove or guarantee
complete erasure.

Errors, debug formatting, and standard encoding omit secret material and
wrapped diagnostic text. Wrapped source causes remain available through
`errors.Is` and `errors.As`; do not log the extracted hardware or
injected-source cause until its disclosure behavior has been reviewed.

The library does not emit logs, traces, metrics, or panic with generated
material. Its secret-bearing types redact default formatting, `log/slog`, and
text/JSON encoding, including formatting of a recovered panic value. A caller
can still disclose a secret by explicitly converting it to `string` or
`[]byte`, extracting mnemonic words or entropy, or attaching those values to
telemetry. Instrumentation boundaries must therefore avoid unwrapping secrets.
