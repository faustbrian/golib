# Compatibility

The minimum toolchain is Go 1.26.5, the latest stable release at implementation
time. The module supports standard-library IANA behavior on Linux, macOS, and
Windows. Timezone results follow the installed or embedded tzdata snapshot.

PostgreSQL integration targets maintained versions 14 through 18. pgx v5.10.0
is pinned. Public API drift is checked against `api/baseline.txt`; deliberate
breaking changes require a major version after v1.
