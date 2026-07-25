# Versioning and releases

The project follows Semantic Versioning. Before v1, minor releases may change
the public API; patch releases preserve documented parsing behavior. After v1,
row shape, normalization order, error categories, default limits, and
configuration meaning are compatibility-sensitive.

Every release requires a changelog entry, green CI, 100% production-statement
coverage, fuzz-target smoke verification, benchmarks, vulnerability scanning,
and documentation examples. Tags use `vMAJOR.MINOR.PATCH` and the release
workflow publishes release notes from the corresponding changelog section.

Breaking changes must describe the old and new row/error behavior and provide
migration guidance. Security releases may omit exploit details until users
have had reasonable time to update.
