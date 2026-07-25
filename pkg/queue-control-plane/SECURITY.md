# Security policy

## Supported versions

No stable version has been released. Until a release is published, only the
current default branch receives security fixes and the project must not be
treated as production ready.

## Reporting a vulnerability

Do not disclose a suspected vulnerability in a public issue. Use GitHub's
private vulnerability reporting for this repository. If that facility is not
available, contact the maintainer privately through the contact method on the
maintainer's GitHub profile.

Include the affected revision, impact, reproduction conditions, and any known
workaround. Do not include real credentials, queue payloads, tenant data, or
production endpoints. The maintainer will acknowledge the report, assess the
affected versions, coordinate a fix and disclosure, and credit the reporter
when requested.

## Security boundary

The control plane is an administrative system, not a queue backend. Reports
about bypassing authentication, tenant authorization, idempotency, audit-chain
integrity, payload privacy, request bounds, CORS/CSRF admission, Kubernetes
namespace scope, or the no-raw-backend boundary are in scope.

Operational exposure caused solely by deploying without TLS, leaking the
static access file, or granting broader Kubernetes or PostgreSQL permissions
than documented is normally a deployment issue, but reports that reveal an
unsafe default or unclear contract are welcome.
