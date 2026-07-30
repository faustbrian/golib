# Adoption

Adopt this module when a workflow already has plaintext inside an isolated
process and must persist only an external immutable secret reference.

Before adoption:

1. define a non-secret deterministic name prefix;
2. derive a stable 32–64 byte version token from the logical source mutation;
3. derive a version-unique staging label;
4. configure workload identity and least-privilege IAM;
5. decide whether the AWS-managed or a customer-managed KMS key applies; and
6. persist both returned fields atomically with the application state that
   refers to them.

Do not adopt this module for ordinary application configuration delivered
through environment variables or mounted files, for mutable rotation
workflows, or as a substitute for authorization and data-retention policy.
