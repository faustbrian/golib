# Residual-Risk Register

No risk in this register is accepted unless the `Acceptance` field names the
user decision and date. An open risk is not a waiver, and package-local green
checks do not close an operational or release boundary.

## GL-RISK-001: Composed Operational Behavior Is Not Yet Certified

- **Severity:** High
- **Status:** Open
- **Owner:** Repository operational-assurance campaign and consuming service owners
- **Exposure:** Independently passing modules do not prove composed startup,
  failover, drain, recovery, rolling deployment, rollback, load, or soak
  behavior under production resource limits.
- **Mitigation:** Execute `.ai/GOAL_OPERATIONAL_ASSURANCE.md` with maintained
  reference services, pinned environments, failure injection, and a complete
  requirement-to-evidence matrix.
- **Review condition:** Close only with a current `ready` verdict, or record
  each narrower accepted residual risk separately.
- **Acceptance:** None.

## GL-RISK-002: Initial Release Provenance Is Incomplete

- **Severity:** High
- **Status:** Open
- **Owner:** Repository release maintainers
- **Exposure:** Local source-proxy dry-runs do not prove signed immutable tags,
  attestations, public Go proxy resolution, checksum availability, or final
  GitHub Actions results.
- **Mitigation:** Complete every module's dependency-ordered `v1.0.0` release
  gate, sign and attest artifacts, and verify clean public consumers without
  replacements.
- **Review condition:** Close after immutable release artifacts and public
  resolution evidence exist for every released module.
- **Acceptance:** None.

## GL-RISK-003: Specialist Specification Evidence Is Still Converging

- **Severity:** Medium
- **Status:** Open
- **Owner:** Kafka and OpenSearch specialist scopes
- **Exposure:** The aggregate specification decision gate currently reports
  incomplete Kafka adapter metadata and decision registers plus incomplete
  OpenSearch decision consequences.
- **Mitigation:** Consume the specialists' current conformance, provenance,
  interoperability, and decision evidence without restarting unaffected
  content-identical package campaigns.
- **Review condition:** Close when the aggregate specification decision gate
  passes for the final affected content.
- **Acceptance:** None.

## GL-RISK-004: Dependency Findings Need Release Disposition

- **Severity:** Medium
- **Status:** Open
- **Owner:** Repository dependency and security maintainers
- **Exposure:** `govulncheck` found no reachable vulnerable call paths in the
  recorded audit, but vulnerable versions can still exist in required or
  fixture dependency graphs and may become reachable after code changes.
- **Mitigation:** Review every reported dependency, update or constrain it
  where practical, retain current reachability evidence, and re-run on affected
  dependency or source changes.
- **Review condition:** Close or replace with narrower records after every
  reported dependency has an explicit version and reachability disposition.
- **Acceptance:** None.

## GL-RISK-005: Live Credential And Key Lifecycle Drills Are Incomplete

- **Severity:** High
- **Status:** Open
- **Owner:** Operational-assurance campaign and consuming service owners
- **Exposure:** Local and provider-emulated tests do not prove deployed secret,
  signing-key, OIDC/JWKS, KMS/HSM, database, broker, or object-storage credential
  rotation and revocation without outage or stale authorization.
- **Mitigation:** Exercise overlap, refresh, revocation, compromise, rollback,
  and redaction under the supported deployment platform.
- **Review condition:** Close only with pinned live-environment drill evidence
  and verified runbooks and alerts.
- **Acceptance:** None.

## GL-RISK-006: Privacy Operations Remain Service-Specific

- **Severity:** High
- **Status:** Open
- **Owner:** Consuming service data owners
- **Exposure:** Generic redaction and retention primitives cannot determine
  each service's personal-data classification, lawful retention, erasure,
  legal hold, forensic export, or telemetry sampling policy.
- **Mitigation:** Each service must publish and exercise its data inventory,
  retention/erasure flows, legal-hold behavior, access controls, and incident
  export procedures.
- **Review condition:** Evaluate per service before deployment; this repository
  risk cannot be globally accepted on behalf of consumers.
- **Acceptance:** None.
