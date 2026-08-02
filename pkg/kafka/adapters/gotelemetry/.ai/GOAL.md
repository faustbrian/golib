# Goal: Kafka OpenTelemetry Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `kafka/adapters/gotelemetry` as the optional OpenTelemetry translation
for completed, payload-free Kafka observations. It MUST remain separate from
the Kafka client, MUST NOT reimplement client instrumentation, and MUST NOT
claim propagation that its completion-only seam cannot provide.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Translate producer, consumer, transaction, replay, inspector, health, and
  lifecycle observations into version-pinned messaging conventions and clearly
  named adapter metrics.
- Deny topic, client, and group identity attributes by default; permit only
  bounded exact allowlists.
- Keep partition/offset diagnostics span-only and exclude payloads, keys,
  headers, credentials, endpoints, error text, and panic values.
- Reconstruct completed span timing from stable start/duration values without
  implying active child work or cross-message propagation.
- Validate all observations and construct instruments synchronously.
- Define provider error, canceled context, no-op, sampling, concurrent observer,
  and SDK shutdown behavior without changing Kafka outcomes.

## Documentation And Completion

Document every span, metric, attribute, cardinality rule, semantic-convention
version, timing limitation, propagation exclusion, privacy policy, API,
examples, FAQ, and migration. CI MUST enforce race, fuzz, security, API, docs,
benchmarks, exactly 100% statement coverage, and exactly 100% of viable mutants
killed by meaningful tests.
