# Changelog

All notable changes to this module will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Add a deterministic JSON payload codec with generic explicit event
  registrations, decode-only aliases, strict unknown-field mode, exact typed
  integers and times, duplicate-key and invalid-UTF-8 rejection, and bounded
  payload nesting and container sizes.
- Add an aggregate lifecycle helper with explicit decoded-event identities,
  immediate application, retry-safe change sets, ordered split-event
  reconstitution, exact persistence acknowledgement, version tracking, and
  poisoned-state containment for failed or panicking application handlers.
- Add immutable, bounded pending and persisted event messages with defensive
  ownership, typed validation errors, explicit stream and schema identities,
  optional correlation metadata, and store-assigned positions.
- Pin the EventSauce 3.9.1 compatibility baseline and inventory every
  documentation page.
- Document the proposed idiomatic Go API, ownership rules, lifecycle, and
  independent adapter boundaries before implementation.
