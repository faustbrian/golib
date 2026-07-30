# Security policy

Report vulnerabilities through the repository's private vulnerability
reporting channel. Never attach secret values, production identifiers, AWS
responses, account details, KMS key identifiers, or customer data.

The module validates and bounds public request fields, copies secret bytes
before calling AWS, best-effort zeroizes its copy, returns only opaque
references, and keeps formatted errors value-free. It does not protect a
compromised process, caller-retained plaintext, swap or crash dumps, overly
broad IAM, malicious AWS administrators, insecure AWS configuration, or
application logging outside this boundary.

Secret names, version tokens, staging labels, and KMS identifiers can appear in
AWS control-plane telemetry. They must contain stable non-secret identifiers
only.
