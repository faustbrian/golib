# Roadmap

## Version 1.0

- Publish the pinned sibling module commits before hosted release verification.
- Preserve exact, match, fallback, encoding, and error semantics through the
  `international/locale` boundary.
- Publish the verified API baseline and compatibility report.

## Later candidates

- Additional `wire` formats only when their map semantics preserve locale
  identity and duplicate detection.
- Three-way merge only after a complete base/left/right conflict table exists.
- Source-aware provenance only as a distinct type whose equality contract
  includes provenance intentionally.

Translation catalogs, message formatting, pluralization, language detection,
machine translation, and remote translation clients remain permanent
non-goals.
