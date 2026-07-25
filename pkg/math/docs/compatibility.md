# Compatibility

The module targets Go 1.26.5 as both its minimum and CI toolchain. Public API
changes are checked against `api/baseline.txt`. Binary encodings carry a version
byte; unknown versions fail. Text and JSON forms are canonical strings.

`money` and `measurement` should import only the numeric family they need
and keep currency, unit, locale, and presentation policy in their own domains.
