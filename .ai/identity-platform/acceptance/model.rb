# frozen_string_literal: true

require "json"
require "digest"
require "shellwords"

module IdentityPlatformAcceptance
  ROOT = File.expand_path("..", __dir__)
  REPOSITORY_ROOT = File.expand_path("../..", ROOT)
  SOURCE = File.join(ROOT, "END_STATE_ACCEPTANCE.json")
  GOALS = File.join(ROOT, "GOAL_MANIFEST.json")
  OPERATIONS = File.join(ROOT, "OPERATION_SEMANTICS.json")
  RUNBOOK_CATALOG = File.join(ROOT, "OPERATIONS_RUNBOOK_CATALOG.json")
  API_OPERATIONS = File.join(ROOT, "API_OPERATIONS.md")
  PUBLIC_CONTRACTS = File.join(ROOT, "PUBLIC_CONTRACTS.json")
  CONFIGURATION_CATALOGS = File.join(ROOT, "CONFIGURATION_CATALOGS.json")
  REFERENCE_CONFIGURATION = File.join(ROOT, "REFERENCE_CONFIGURATION.md")
  PROTOCOL_CONFORMANCE = File.join(ROOT, "PROTOCOL_CONFORMANCE_MANIFEST.json")
  CATALOG = File.join(ROOT, "ACCEPTANCE_ARTIFACTS.json")
  SCHEMA_ROOT = File.join(__dir__, "v1", "schemas")

  PRIMITIVE_PREREQUISITES = [
    {"unit" => "primitive/authentication-identity-contracts", "goal_path" => "goals/primitive-authentication-identity-contracts.md", "module_gates" => ["make check MODULES=pkg/authentication"]},
    {"unit" => "primitive/authorization-identity-contracts", "goal_path" => "goals/primitive-authorization-identity-contracts.md", "module_gates" => ["make check MODULES=pkg/authorization"]},
    {"unit" => "primitive/capability-identity-contracts", "goal_path" => "goals/primitive-capability-identity-contracts.md", "module_gates" => ["make check MODULES=pkg/capability"]},
    {"unit" => "primitive/capability-postgres-identity-contracts", "goal_path" => "goals/primitive-capability-postgres-identity-contracts.md", "module_gates" => ["make check MODULES=pkg/capability/postgres"]},
    {"unit" => "primitive/identifier-identity-contracts", "goal_path" => "goals/primitive-identifier-identity-contracts.md", "module_gates" => ["make check MODULES=pkg/identifier"]},
    {"unit" => "primitive/password-secret-contracts", "goal_path" => "goals/primitive-password-secret-contracts.md", "module_gates" => ["make check MODULES=pkg/password"]}
  ].map { |row| row.merge("required_before_artifact_producers" => true, "produces_end_state_artifacts" => false) }.freeze

  RUNBOOK_ACTION_DEFINITIONS = {
    "startup" => ["identity/reference", "configuration has been validated and required dependencies are reachable", "start the composed identity service and enable traffic only after readiness passes", "verify health, readiness, operation registration, and dependency probes", "stop the service and withdraw readiness without accepting new work", "escalate when readiness cannot be established or startup changes durable state"],
    "shutdown" => ["identity/reference", "planned termination or loss of readiness has been requested", "withdraw readiness, drain admitted work, stop workers, then close dependencies", "verify no new admission, bounded drain completion, and no leaked workers or leases", "restart only from a clean stopped state after shutdown failure is reconciled", "escalate when admitted work or durable outcomes remain unknown"],
    "backup" => ["identity/reference", "the scheduled backup window or an authorized manual backup has started", "capture an encrypted consistent PostgreSQL backup and the configuration needed for restoration", "verify completion, checksum, encryption metadata, retention, and restore eligibility", "discard an incomplete backup record and retain the last verified recoverable backup", "escalate when no backup satisfies the recovery-point objective"],
    "restore" => ["identity/reference", "an authorized recovery declares the primary state unavailable or a rehearsal begins", "restore the selected verified backup into an isolated target before controlled cutover", "verify schema compatibility, integrity, identity invariants, and recovery objectives", "abort cutover and return traffic to the prior authority when it remains trustworthy", "escalate on integrity failure, ambiguous authority, or missed recovery objectives"],
    "credential-rotation" => ["identity/reference", "a credential reaches policy age, is suspected exposed, or rotation is requested", "issue the successor credential, deploy overlap where permitted, verify adoption, then revoke the predecessor", "verify successor use, predecessor rejection, audit events, and absence of secret disclosure", "revoke the successor and retain the predecessor only while policy permits safe overlap", "escalate on suspected compromise, orphaned consumers, or ambiguous revocation"],
    "signing-key-rotation" => ["oauth-server", "a signing key reaches policy age, is compromised, or an emergency rotation is authorized", "publish the successor public key before signing with it, observe overlap, then retire the predecessor", "verify JWKS propagation, successor signatures, predecessor retirement, and token validation", "resume predecessor signing only when it is uncompromised and still within policy", "escalate on compromise, propagation failure, or unverifiable issued tokens"],
    "postgres-outage" => ["identity/postgres", "PostgreSQL is unavailable, timing out, or returns an ambiguous commit outcome", "fail closed for authority-changing work and reconcile command outcomes before any retry", "verify readiness degradation, bounded errors, command-ledger reconciliation, and recovery", "keep mutation traffic disabled until the authoritative outcome is known", "escalate on possible data loss, split authority, or unreconciled outcomes"],
    "valkey-outage" => ["identity/session/valkey", "Valkey is unavailable, stale, or inconsistent with PostgreSQL authority", "treat PostgreSQL as authority, bypass or invalidate unsafe cache state, and bound degraded operation", "verify stale sessions cannot authorize and cache state converges after recovery", "disable the cache path and continue only for operations whose authoritative fallback is safe", "escalate on authorization divergence, replay exposure, or unbounded degradation"],
    "provider-outage" => ["identity/oauth/providers", "an external identity, delivery, CAPTCHA, HIBP, or federation provider is unavailable", "apply the operation-specific fail-closed or retry policy without fabricating provider success", "verify attributable outage classification, bounded retries, redaction, and recovery behavior", "disable the affected provider route or return the documented stable unavailable outcome", "escalate on widespread lockout, security-policy bypass, or unknown provider state"],
    "unknown-outcome-recovery" => ["identity/reference", "a durable operation reports timeout, disconnect, or another possibly committed outcome", "query the authoritative command journal using the original command identity and canonical fingerprint", "verify the exact recorded terminal result is returned and side effects are not reapplied", "block a new attempt until reconciliation establishes a safe terminal outcome", "escalate when the journal is unavailable, mismatched, or cannot establish authority"]
  }.freeze

  def runbook_catalog_document
    operations_bytes = File.binread(OPERATIONS)
    actions = RUNBOOK_ACTION_DEFINITIONS.each_with_index.map do |(action_id, values), index|
      owner, *component_values = values
      components = %w[trigger procedure verification rollback escalation].each_with_index.to_h do |component, component_index|
        content = component_values.fetch(component_index)
        [component, {"locator" => ".ai/identity-platform/OPERATIONS_RUNBOOK_CATALOG.json#/actions/#{index}/components/#{component}/content",
                     "content" => content, "content_sha256" => "sha256:#{Digest::SHA256.hexdigest(content.b)}"}]
      end
      {"action_id" => action_id, "owner" => owner, "version" => 1, "components" => components}
    end
    {"schema_version" => 1, "authority" => ".ai/identity-platform/OPERATION_SEMANTICS.json",
     "operation_semantics_sha256" => "sha256:#{Digest::SHA256.hexdigest(operations_bytes)}",
     "operation_count" => JSON.parse(operations_bytes).fetch("operations").length, "actions" => actions}
  end
  module_function :runbook_catalog_document

  COMMON_SCENARIOS = %w[success failure].freeze
  SUPPLEMENTAL_OPERATION_CLAIMS = {
    "api-key-http-lifecycle-report" => %w[identity.apikey.create identity.apikey.delete identity.apikey.rotate identity.apikey.session-authenticate identity.apikey.verify],
    "bearer-continuation-report" => %w[identity.session.bearer-authorize identity.session.bearer-issue],
    "platform-authority-version-report" => %w[identity.platform.permission-statement.create identity.platform.permission-statement.delete identity.platform.permission-statement.update identity.platform.role.create identity.platform.role.delete identity.platform.role.update],
    "impersonation-quorum-report" => %w[identity.impersonation.approve identity.impersonation.deny identity.impersonation.quorum identity.impersonation.request identity.impersonation.revoke identity.impersonation.start identity.impersonation.stop],
    "mfa-recovery-report" => %w[identity.admin.mfa-recovery-issue identity.admin.mfa-reset],
    "organization-invitation-lifecycle-report" => %w[identity.organization.invitation-send identity.organization.invitation.accept identity.organization.invitation.cancel identity.organization.invitation.expire identity.organization.invitation.get identity.organization.invitation.list identity.organization.invitation.list-mine identity.organization.invitation.reject identity.organization.invitation.resend],
    "remember-policy-propagation-report" => %w[identity.anonymous.signin identity.anonymous.upgrade identity.email.verification-confirm identity.magic-link.consume identity.mfa.otp-verify identity.mfa.recovery-use identity.mfa.security-key-assert-verify identity.mfa.totp-verify identity.oauth.callback identity.oauth.callback-form-post identity.oauth.onetap-callback identity.oauth.signin-token identity.otp.email-verify identity.otp.signin identity.passkey.register-verify identity.passkey.signin-verify identity.password.signin identity.password.signup identity.phone.password-signin identity.phone.signin identity.phone.verify identity.session.refresh identity.sso.oauth-callback identity.sso.oidc-callback identity.sso.saml-acs identity.sso.saml-idp-init],
    "sso-provider-lifecycle-report" => %w[identity.sso.directory-sync-apply identity.sso.directory-sync-cancel identity.sso.directory-sync-start identity.sso.directory-sync-status identity.sso.oauth-callback identity.sso.oidc-callback identity.sso.provider.enable identity.sso.provider.get identity.sso.provider.list identity.sso.saml-acs identity.sso.saml-idp-init identity.sso.saml-logout-start identity.sso.saml-metadata identity.sso.saml-slo identity.sso.saml-start],
    "unknown-outcome-reconciliation-report" => %w[identity.delivery.cancel identity.delivery.enqueue identity.delivery.receipt-record identity.delivery.reconcile identity.delivery.status-get],
    "oauth-provider-matrix-report" => %w[identity.account.link-start identity.account.unlink identity.oauth.callback identity.oauth.callback-form-post identity.oauth.logout-complete identity.oauth.logout-start identity.oauth.signin-start],
    "session-profile-contract-report" => %w[identity.session.get identity.session.list identity.session.refresh identity.session.revoke-all identity.session.revoke-one identity.session.revoke-other identity.session.signout identity.session.update],
    "otp-reservation-report" => %w[identity.otp.check identity.otp.send identity.otp.signin],
    "scim-reconciliation-report" => %w[identity.sso.directory-sync-apply identity.sso.directory-sync-start identity.sso.directory-sync-status]
  }.transform_values { |ids| ids.sort_by(&:b).freeze }.freeze
  SUPPLEMENTAL_OPERATION_PREFIXES = {
    "platform-authority-version-report" => %w[identity.admin. identity.platform.],
    "reference-http-identity-lifecycle" => %w[identity.anonymous. identity.deletion. identity.profile. identity.user. identity.username.],
    "api-key-http-lifecycle-report" => %w[identity.apikey.],
    "audit-retention-plan-confirm-report" => %w[identity.audit-retention.],
    "reference-http-password-journey" => %w[identity.email. identity.password. identity.phone.],
    "locale-negotiation-report" => %w[identity.i18n.],
    "identitytest-cycle-free-report" => %w[identity.identitytest.],
    "mfa-recovery-report" => %w[identity.mfa.],
    "oauth-provider-matrix-report" => %w[identity.account. identity.oauth.],
    "oauth-oidc-conformance-report" => %w[identity.oauth-server.],
    "organization-concurrency-report" => %w[identity.organization.],
    "otp-reservation-report" => %w[identity.otp.],
    "clean-reference-stack-report" => %w[identity.reference.],
    "scim-connection-lifecycle-report" => %w[identity.scim.connection-],
    "session-profile-contract-report" => %w[identity.session.],
    "sso-provider-lifecycle-report" => %w[identity.sso.]
  }.freeze
  EXTRA_SCENARIOS = {
    "denial" => %w[
      api-key-http-lifecycle-report audit-retention-plan-confirm-report authorization-cross-tenant-report
      browser-security-report captcha-four-provider-report custom-fields-report device-authorization-report
      domain-verification-report download-capability-report enterprise-oidc-logout-report
      export-minimization-redaction-report impersonation-quorum-report integrated-threat-report
      mfa-recovery-report native-token-mode-report oauth-oidc-conformance-report oauth-popup-browser-report
      operational-api-redaction-report organization-archive-restore-delete-report
      organization-invitation-lifecycle-report passkey-browser-ceremony-report passwordless-http-report
      phone-reset-risk-evidence-report privacy-boundary-report privacy-export-lifecycle-report
      phone-reset-risk-evidence-report reauthentication-proof-report reference-http-identity-lifecycle reference-http-password-journey
      risk-policy-report scim-connection-lifecycle-report scim-reconciliation-report
      scim-rfc-conformance-report secret-redaction-report session-profile-contract-report
      session-transfer-report sso-enforcement-break-glass-report sso-provider-lifecycle-report
      typed-fields-schema-report typed-hooks-report webauthn-conformance-report
    ],
    "outage" => %w[
      api-key-postgres-valkey-race-report backup-restore-rehearsal bearer-continuation-report
      captcha-four-provider-report device-authorization-report distributed-rate-limit-report
      enterprise-oidc-logout-report hibp-interoperability-report instrumentation-report
      native-token-mode-report oauth-oidc-conformance-report oauth-provider-matrix-report
      oauth-proxy-no-write-report operational-api-redaction-report organization-concurrency-report otp-reservation-report
      phone-reset-risk-evidence-report postgres-lifecycle-cascade-report postgres-valkey-failure-report
      privacy-export-lifecycle-report provider-evidence-index scim-connection-lifecycle-report
      scim-reconciliation-report session-transfer-report signer-jwks-profile-report
      sso-provider-lifecycle-report unknown-outcome-reconciliation-report
    ],
    "replay" => %w[
      api-key-http-lifecycle-report audit-retention-plan-confirm-report bearer-continuation-report
      captcha-four-provider-report device-authorization-report domain-verification-report
      enterprise-oidc-logout-report mfa-recovery-report native-token-mode-report
      oauth-oidc-conformance-report oauth-popup-browser-report organization-invitation-lifecycle-report
      otp-reservation-report passkey-browser-ceremony-report passwordless-http-report
      reauthentication-proof-report reference-http-identity-lifecycle reference-http-password-journey
      scim-reconciliation-report session-transfer-report sso-provider-lifecycle-report
      webauthn-conformance-report
    ],
    "concurrency" => %w[
      api-key-postgres-valkey-race-report audit-retention-plan-confirm-report backup-restore-rehearsal
      distributed-rate-limit-report impersonation-quorum-report mfa-recovery-report
      organization-archive-restore-delete-report organization-concurrency-report
      organization-invitation-lifecycle-report otp-reservation-report phone-reset-risk-evidence-report postgres-lifecycle-cascade-report
      postgres-valkey-failure-report privacy-export-lifecycle-report reference-http-identity-lifecycle
      scim-reconciliation-report session-transfer-report sso-provider-lifecycle-report
      unknown-outcome-reconciliation-report
    ],
    "cleanup" => %w[
      backup-restore-rehearsal clean-reference-stack-report device-authorization-report
      external-module-clean-consumer-report identitytest-cycle-free-report mfa-recovery-report
      oauth-popup-browser-report operational-api-redaction-report organization-archive-restore-delete-report
      organization-invitation-lifecycle-report otp-reservation-report passkey-browser-ceremony-report phone-reset-risk-evidence-report
      postgres-lifecycle-cascade-report privacy-export-lifecycle-report reference-http-identity-lifecycle
      scim-connection-lifecycle-report session-transfer-report sso-provider-lifecycle-report
      unknown-outcome-reconciliation-report webauthn-conformance-report
    ],
    "unknown-outcome" => %w[
      api-key-postgres-valkey-race-report audit-retention-plan-confirm-report backup-restore-rehearsal
      device-authorization-report enterprise-oidc-logout-report organization-archive-restore-delete-report
      organization-concurrency-report organization-invitation-lifecycle-report otp-reservation-report phone-reset-risk-evidence-report
      postgres-lifecycle-cascade-report postgres-valkey-failure-report privacy-export-lifecycle-report
      scim-reconciliation-report session-transfer-report sso-provider-lifecycle-report
      unknown-outcome-reconciliation-report
    ],
    "rollback" => %w[phone-reset-risk-evidence-report],
    "expiry" => %w[phone-reset-risk-evidence-report]
  }.transform_values(&:freeze).freeze
  EXTERNAL_PROFILE_SOURCES = {
    "captcha-recaptcha" => ["captcha-four-provider-report", "source", "recaptcha-siteverify-20260801153447"],
    "captcha-turnstile" => ["captcha-four-provider-report", "source", "turnstile-siteverify-20260728114909"],
    "captcha-hcaptcha" => ["captcha-four-provider-report", "source", "hcaptcha-siteverify-20260719190355"],
    "captcha-captchafox" => ["captcha-four-provider-report", "source", "captchafox-siteverify-20260517115151"],
    "hibp-pwned-passwords" => ["hibp-interoperability-report", "source", "hibp-pwned-passwords-20260807131257"],
    "protocol-openid-conformance-suite" => ["oauth-oidc-conformance-report", "tool", "openid-conformance-suite"],
    "protocol-web-platform-tests" => ["webauthn-conformance-report", "tool", "web-platform-tests"],
    "protocol-unboundid-scim2-sdk" => ["scim-rfc-conformance-report", "tool", "unboundid-scim2-sdk"],
    "protocol-simplesamlphp" => ["sso-provider-lifecycle-report", "tool", "simplesamlphp"]
  }.freeze

  PROFILE_ROWS = <<~'ROWS'
    affected-gate-report|make check MODULES=pkg/identity/reference|module,input_root|selected module gate pending|all selected checks produce attributable pass records|selected_module_count:count,passed_module_count:count,gate_report_digest:digest
    api-key-http-lifecycle-report|identity.apikey.create,identity.apikey.verify,identity.apikey.rotate,identity.apikey.delete,identity.apikey.session-authenticate|tenant_id,key_name,scopes,expires_at,idempotency_key,api_key_secret,session_authentication_enabled,revocation_version|no key exists for key_name and API-key session authentication profile is explicitly configured|create returns one secret, verify authenticates, rotate invalidates the old secret, delete and revocation prevent authentication, disabled session authentication denies without session creation, and enabled authentication creates only a bounded profile-compliant session that observes revocation|api_key_id:id,created_secret_digest:digest,rotated_secret_digest:digest,old_secret_rejected:boolean,deleted_secret_rejected:boolean,disabled_profile_rejected:boolean,enabled_session_authenticated:boolean,revocation_propagated:boolean
    api-key-postgres-valkey-race-report|identity.apikey.create,identity.apikey.rotate,identity.apikey.verify|tenant_id,api_key_id,rotation_version,idempotency_key,debit_operation_id,debit_failure_point|PostgreSQL owns the active key lifecycle and Valkey contains only a current positive metadata/quota projection|one concurrent rotation wins; every allow follows a fresh PostgreSQL-authoritative decision; cache miss, stale projection, epoch loss and nil authority never allow; a disconnect around quota mutation returns definitely-not-debited, debited, or possibly-debited with the same operation ID, possible debit blocks retry, and reconciliation converges without a duplicate debit|api_key_id:id,debit_operation_id:id,winner_version:version,conflict_count:count,possible_debit_count:count,reconciled_debit_count:count,duplicate_debit_count:count,authoritative_verification_count:count,postgres_state_digest:digest,valkey_state_digest:digest,debit_journal_digest:digest
    audit-retention-plan-confirm-report|identity.audit-retention.deletion.plan,identity.audit-retention.deletion.confirm|tenant_id,policy_version,cutoff,plan_id,plan_digest|eligible audit rows exist outside legal holds|confirm accepts only the exact unexpired plan digest and deletes exactly its eligible row set|plan_id:id,planned_record_count:count,deleted_record_count:count,legal_hold_record_count:count,plan_digest:digest
    authorization-cross-tenant-report|identity.organization.get,identity.session.get|actor_tenant_id,target_tenant_id,target_resource_id|actor has authority only in actor_tenant_id|target-tenant reads return forbidden and emit no target payload|target_resource_id:id,stable_error:error,target_payload_absent:boolean,authorization_event_digest:digest
    backup-restore-rehearsal|make check MODULES=pkg/identity/reference|backup_id,source_revision,restore_target,encryption_key_id,credential_fixture_digest,replay_checkpoint|source database contains identities, encrypted credential material, sessions, organizations, audit rows, and a persisted replay checkpoint|the reference module gate creates a backup, restores it into an empty rehearsal database, decrypts key and credential fixtures, resumes from the exact replay checkpoint, and proves restored row and integrity digests equal the source revision|backup_id:id,encryption_key_id:id,restored_identity_count:count,restored_credential_count:count,decrypted_credential_count:count,replayed_checkpoint_count:count,source_digest:digest,restore_digest:digest
    bearer-continuation-report|identity.session.bearer-issue,identity.session.bearer-authorize|tenant_id,session_id,audience,continuation_ttl|active browser session exists|issued bearer continuation preserves subject, tenant, audience, expiry, revocation, and freshness policy|session_id:id,continuation_id:id,subject_id:id,audience_digest:digest,revocation_observed:boolean
    browser-security-report|identity.oauth.popup-complete,identity.openapi.document|origin,csrf_token,callback_state,cookie_attributes|trusted origin and bound browser state are configured|every HTTP operation is independently checked against its exact method, path, access, cookie and CSRF or origin policy; trusted callbacks succeed while untrusted origins, missing CSRF, ambient-cookie substitution and insecure cookie variants are rejected|trusted_origin_count:count,csrf_rejected:boolean,origin_rejected:boolean,secure_cookie_observed:boolean,same_site_mode:cookie_mode
    captcha-four-provider-report|identity.risk.captcha-verify|provider,token,hostname,action,remote_ip,tenant_id,subject_or_anonymous_flow_id,flow_context_variant,preauth_transaction_id,session_id,admin_actor_id,canonical_request_fingerprint,protected_target_id|exact sandbox or pinned fixture evidence exists for recaptcha, turnstile, hcaptcha, and captchafox and the canonical API middleware attachment table defines every protected target|exactly four unique canonical provider results prove verified sandbox or pinned fixture execution; every configured CAPTCHA target from the canonical API attachment table has one middleware-attached result with its exact permitted flow contexts and attributable evidence; identity/risk/postgres alone reserves, applies, and finalizes durable evidence for every target while the stateless verifier performs no durable write|provider_ids:captcha_provider_ids,provider_results:captcha_provider_results,provider_count:count,verified_provider_count:count,protected_target_ids:captcha_target_ids,protected_target_results:captcha_target_results,protected_target_count:count,middleware_attached_target_count:count,hostname_mismatch_rejected:boolean,replay_rejected:boolean,durable_reservation_count:count,durable_apply_count:count,durable_finalize_count:count,stateless_verifier_durable_write_count:count,flow_binding_digest:digest,durable_owner_digest:digest,provider_matrix_digest:digest
    clean-reference-stack-report|identity.reference.config-validate,identity.reference.migration-apply,identity.health|reference_config,migration_plan,service_deadline|empty PostgreSQL and Valkey instances are reachable|configuration validates, migrations apply once, and health reports the initialized stack ready|migration_count:count,applied_migration_count:count,health_status:health_status,configuration_digest:digest,schema_digest:digest
    composition-collision-report|identity.reference.config-validate|route_ids,schema_ids,migration_ids,middleware_ids|two selected modules contribute the same identifier|composition rejects duplicate route, schema, migration, and middleware identifiers before service startup|route_collision_count:count,schema_collision_count:count,migration_collision_count:count,middleware_collision_count:count,stable_error:error
    coverage-mutation-report|make check MODULES=pkg/identity/reference|package_set,coverage_profiles,mutation_profiles|all affected production packages and viable mutants are selected|every package reports exact 100 percent statement coverage and every viable mutant is killed|package_count:count,statement_coverage_basis_points:basis_points,viable_mutant_count:count,killed_mutant_count:count,report_digest:digest
    custom-fields-report|identity.user.create,identity.user.update-admin,identity.session.get|tenant_id,field_schema,input_fields,actor_permissions|typed additional fields have defaults, validation, read, write, and sensitivity declarations|valid fields round-trip with types while forbidden writes and sensitive reads are rejected or redacted|field_schema_digest:digest,round_trip_field_count:count,forbidden_write_count:count,redacted_field_count:count,persistence_digest:digest
    device-authorization-report|identity.oauth-server.device-authorize,identity.oauth-server.device-approve,identity.oauth-server.device-deny,identity.oauth-server.device-token|client_id,device_code,user_code,scopes,interval|registered device client has no authorization grant|pending polling respects interval, approval issues one grant, denial returns access_denied, and expired or replayed codes issue no token|device_code_digest:digest,user_code_digest:digest,poll_count:count,token_issued_count:count,terminal_status:device_status
    distributed-rate-limit-report|identity.risk.evaluate|tenant_id,route_id,client_ip,limit,window|PostgreSQL and Valkey counters start at zero for the canonical client key|concurrent requests admit exactly limit operations and all later operations receive rate_limited with Retry-After|admitted_count:count,rejected_count:count,retry_after_seconds:count,postgres_counter:count,valkey_counter:count
    domain-verification-report|identity.sso.domain-challenge,identity.sso.domain-verify|tenant_id,domain,challenge_method,challenge_value|domain is unverified and no other tenant owns it|exact DNS or HTTPS proof verifies the canonical domain once; suffix, stale, and cross-tenant proofs remain denied|domain_digest:digest,challenge_id:id,verified:boolean,suffix_attack_rejected:boolean,cross_tenant_rejected:boolean
    download-capability-report|identity.privacy-export.download-capability-issue,identity.privacy-export.download|tenant_id,export_id,subject_id,capability_ttl|completed export belongs to subject_id and has no active capability|one bounded capability downloads the bound export and expiry, subject mismatch, and replay return denial|export_id:id,capability_digest:digest,download_count:count,replay_rejected:boolean,expiry_rejected:boolean
    enterprise-oidc-logout-report|identity.sso.oidc-logout,identity.sso.oidc-logout-complete|tenant_id,provider_id,id_token_hint,post_logout_redirect_uri,state|provider session and local relying-party session are active|RP-initiated logout validates redirect and state, completes provider callback once, and revokes the local session|provider_id:id,session_id:id,logout_state_digest:digest,local_session_revoked:boolean,replay_rejected:boolean
    export-minimization-redaction-report|identity.privacy-export.request,identity.privacy-export.download|tenant_id,subject_id,requested_categories|subject has exportable identity data plus secret-shaped and third-party fields|export contains only allowed categories and replaces or omits credentials, tokens, internal risk data, and third-party secrets|export_id:id,included_record_count:count,redacted_field_count:count,secret_match_count:count,export_digest:digest
    external-module-clean-consumer-report|identity.reference.schema-generate,identity.openapi.document|external_module_path,module_version,selected_extensions|clean module cache contains only published golib modules|external module compiles, composes its extension, and receives complete generated schema and OpenAPI declarations|module_version_digest:digest,compiled:boolean,extension_route_count:count,generated_schema_digest:digest,openapi_digest:digest
    health-readiness-route-report|identity.health,identity.readiness|dependency_status,drain_state,route_request|liveness process is running and readiness dependencies are independently controllable|health remains live during dependency outage while ready returns unavailable until dependencies recover and drain is clear|health_status:health_status,readiness_status:readiness_status,failed_dependency_count:count,retry_after_seconds:count,response_digest:digest
    hibp-interoperability-report|identity.risk.hibp-check,identity.password.signup,identity.password.set,identity.password.change,identity.password.reset-complete,identity.admin.user-password-set|range_endpoint,password_sha1_prefix,user_agent,follow_redirects,add_padding_header,padded_response,provider_outcome,password_operation|pinned HIBP range fixtures contain separately attributable known-compromised and absent/no-match padded responses plus malformed, redirect, unavailable, and unknown outcomes for every password credential creation, change, reset, and administrative set path|both known-compromised and absent/no-match cases send only a five-uppercase-hex prefix to the exact HTTPS range endpoint with a nonsecret User-Agent, redirects disabled, Add-Padding true, and zero password or full-hash disclosure; the compromised case proves a positive suffix match while the absent case proves exactly zero matches; unavailable or unknown evidence makes signup, set, change, reset, and administrative set deny or return unavailable and never allow|range_endpoint:hibp_endpoint,transmitted_prefix:hibp_prefix,user_agent:hibp_user_agent,user_agent_secret_match_count:count,redirect_follow_count:count,add_padding_header:hibp_add_padding,padding_row_count:count,padded_response_valid:boolean,outcome_cases:hibp_outcome_cases,known_compromised_match_count:count,absent_match_count:count,unavailable_case_count:count,unknown_case_count:count,password_signup_denied:boolean,password_set_denied:boolean,password_change_denied:boolean,password_reset_denied:boolean,admin_password_set_denied:boolean,timeout_status:timeout_status,response_digest:digest
    identitytest-cycle-free-report|identity.identitytest.organization-create,identity.identitytest.organization-save,identity.identitytest.organization-delete|consumer_module,fixture_seed,organization_fixture|clean consumer imports identitytest without importing persistence adapters|fixture helpers create deterministic state, save explicit changes, delete owned fixtures, and leave no dependency cycle|consumer_module_digest:digest,fixture_id:id,created_fixture_count:count,remaining_fixture_count:count,dependency_graph_digest:digest
    impersonation-quorum-report|identity.impersonation.request,identity.impersonation.approve,identity.impersonation.start,identity.impersonation.stop|tenant_id,requester_id,approver_ids,target_user_id,reason|requester and distinct approvers satisfy configured roles but quorum is not yet reached|duplicate or self approval is denied, exact quorum enables one bounded session, and stop revokes it with audit events|request_id:id,distinct_approval_count:count,required_quorum:count,session_id:id,audit_event_digest:digest
    instrumentation-report|identity.reference.diagnostics|operation_id,correlation_id,secret_shaped_inputs,deadline|metrics, logs, and traces are enabled with bounded exporters|one operation produces correlated metric, log, and span records with stable error class and no secret-shaped value|metric_record_count:count,log_record_count:count,span_record_count:count,redaction_match_count:count,telemetry_digest:digest
    integrated-threat-report|identity.risk.evaluate,identity.session.revoke-all|tenant_id,subject_id,signals,policy_version|subject has active sessions and risk signals cross a configured action threshold|risk decision is deterministic, step_up or denial is enforced, and a separately authorized response policy invokes session revoke-all after deny without inventing a revoke risk action|decision_id:id,risk_score_basis_points:basis_points,action:risk_action,revoked_session_count:count,security_event_digest:digest
    locale-negotiation-report|make check MODULES=pkg/identity/i18n|explicit_locale,accept_language,tenant_default,platform_default|catalog contains translations for supported and partially supported locales|selection follows explicit, header, tenant, platform order and missing messages fall back without changing the stable error identity|selected_locale:locale,fallback_locale:locale,fallback_count:count,catalog_digest:digest,message_digest:digest
    localized-error-identity-report|identity.password.signin|locale,credential,expected_error_class|same invalid credential is submitted under every supported locale|localized message changes by locale while error class, code, HTTP status, and enumeration resistance remain identical|locale_count:count,stable_error:error,http_status:http_status,localized_message_digest:digest,identity_equivalence_digest:digest
    mfa-recovery-report|identity.mfa.recovery-regenerate,identity.mfa.recovery-use,identity.admin.mfa-recovery-issue,identity.admin.mfa-reset|tenant_id,user_id,recovery_code,reauthentication_proof,reset_generation,approval_id,approved_recovery_channel,reason|MFA user has an existing recovery-code set and administrative reset requires a fresh recovery administrator plus independent approval|regeneration invalidates every old code, one new code is consumed once, administrative reset removes factors and advances one generation only after approval, and recovery issuance binds that completed generation and approval while revealing no factor secret|recovery_set_id:id,generated_code_count:count,consumed_code_count:count,old_code_rejected:boolean,reset_generation:version,independent_approval_count:count,recovery_capability_id:id,audit_event_digest:digest
    native-token-mode-report|identity.account.link-token|provider,token_mode,provider_token,nonce,audience|provider catalog declares the submitted native token mode|supported token validates issuer, audience, nonce, and provider subject; unsupported mode or mismatched claim links no account|provider_id:id,token_mode:token_mode,linked_account_id:id,nonce_rejected:boolean,audience_rejected:boolean
    oauth-oidc-conformance-report|identity.oauth-server.authorize,identity.oauth-server.continue,identity.oauth-server.token,identity.oauth-server.session-token,identity.oauth-server.userinfo,identity.oauth-server.introspect,identity.oauth-server.revoke,identity.oauth-server.dynamic-register,identity.oauth-server.discovery-oauth,identity.oauth-server.discovery-oidc,identity.oauth-server.jwks,identity.oauth-server.end-session,identity.oauth-server.protected-resource-metadata|client_id,redirect_uri,response_type,grant_type,scope,state,nonce,code_verifier,authorization_code,refresh_token,client_assertion,client_assertion_jti,access_token,resource|the OAuth and OIDC fixture clients, protected resource, pinned RFC fixtures, and pinned OpenID Foundation conformance suite revision are registered with authorization-code, refresh-token, client-credentials and private-key-JWT profiles and RFC 7592 management selected as an explicit unsupported closure|exact endpoint and operation-specific replay execution proves authorization, bounded continuation, single-use authorization codes, refresh-family rotation and reuse revocation, client credentials, client-assertion jti replay rejection, session-token issuance, UserInfo, introspection, revocation, dynamic registration, discovery, JWKS, logout, and protected-resource metadata; every result is independently attributable to pinned suite or fixture evidence|endpoint_operation_ids:oauth_oidc_operation_ids,endpoint_operation_count:count,interoperability_claim_ids:oauth_oidc_interoperability_claims,conformance_suite_revision:oauth_oidc_suite_revision,authorization_code_digest:digest,access_token_digest:digest,id_token_digest:digest,userinfo_digest:digest,continue_verified:boolean,session_token_verified:boolean,introspection_active_verified:boolean,introspection_inactive_verified:boolean,revocation_verified:boolean,dynamic_registration_verified:boolean,registration_access_token_issued_count:count,oauth_discovery_verified:boolean,oidc_discovery_verified:boolean,jwks_verified:boolean,logout_verified:boolean,protected_resource_metadata_verified:boolean,independent_transcript_digest:digest,conformance_case_count:count
    oauth-popup-browser-report|identity.oauth.popup-complete|provider_id,popup_origin,state,callback_parameters,opener_origin|popup state is bound to provider, opener origin, and callback URI|valid callback posts one bounded result to the exact opener and closes; wrong origin, state, and replay disclose nothing|popup_state_digest:digest,posted_message_count:count,target_origin_digest:digest,popup_closed:boolean,replay_rejected:boolean
    oauth-provider-matrix-report|identity.oauth.signin-start,identity.oauth.callback|provider,response_mode,scopes,redirect_uri,state|all pinned provider catalog entries have exact endpoint and claim fixtures|every provider completes its declared query or form_post response and maps profile, denial, and provider error consistently|provider_count:count,successful_provider_count:count,form_post_provider_count:count,error_mapping_count:count,matrix_digest:digest
    oauth-proxy-no-write-report|identity.oauth.proxy-forward|provider_id,upstream_request,state,redirect_uri|proxy has no persistence writer and exact upstream allowlist is configured|authorize and callback relay bounded approved parameters while creating no identity, account, session, or credential rows|relayed_parameter_count:count,identity_row_delta:count,account_row_delta:count,session_row_delta:count,callback_digest:digest
    openapi-consumer-report|identity.openapi.document|selected_modules,extension_routes,security_profiles|all core and selected extension handlers are composed|OpenAPI 3.1.1 contains every handler operation, request, response, error, and security scheme and generated client compiles|operation_count:count,schema_count:count,security_scheme_count:count,generated_client_compiled:boolean,openapi_digest:digest
    operation-handler-openapi-bijection|identity.openapi.document|operation_catalog,handler_catalog,openapi_document|327 canonical operations and composed handler set are loaded|each exposed operation has exactly one handler and OpenAPI operation and no handler or OpenAPI row is orphaned|operation_count:count,handler_count:count,openapi_operation_count:count,duplicate_count:count,bijection_digest:digest
    operational-api-redaction-report|identity.reference.diagnostics,identity.reference.migration-check,identity.audit.get,identity.audit.search,identity.audit.list,identity.audit.export|tenant_id,investigation_id,record_id,indexed_predicates,cursor,limit,time_bounds,field_grants,export_byte_limit,secret_shaped_configuration|one immutable audit investigation contains permissioned and redacted records while reference diagnostics contain credentials, tokens, keys, DSNs, and safe identifiers|diagnostic and migration outputs retain safe identifiers with zero secret matches; audit get, indexed search, bounded chronological list, and immutable export share one investigation ID, enforce grants and tenant scope, emit access and export events, preserve stable cursors and tamper-evident digests, and make absent and forbidden indistinguishable|investigation_id:id,get_record_count:count,search_record_count:count,list_record_count:count,exported_record_count:count,redacted_field_count:count,access_event_count:count,export_event_count:count,cursor_digest:digest,content_digest:digest,cross_tenant_rejected:boolean,forbidden_indistinguishable:boolean,secret_match_count:count
    operations-runbook-index|identity.reference.diagnostics|runbook_catalog,operation_catalog,failure_class_catalog|the exact closed operational action catalog enumerates startup, shutdown, backup, restore, rotation, outage, and recovery actions|every exact action row has one versioned owner and independently resolvable trigger, procedure, verification, rollback, and escalation references|runbook_count:count,covered_action_count:count,missing_action_count:count,index_digest:digest,owner_digest:digest
    organization-archive-restore-delete-report|identity.organization.archive,identity.organization.restore,identity.organization.delete|tenant_id,organization_id,expected_version,retention_policy|organization is active with members, invitations, sessions, and SSO links|archive blocks active use, restore re-enables retained state, and confirmed delete removes or tombstones every declared cascade|organization_id:id,archive_version:version,restore_version:version,deleted_cascade_count:count,lifecycle_event_digest:digest
    organization-concurrency-report|identity.organization.update,identity.organization.member.update,identity.organization.active-set|tenant_id,organization_id,expected_version,member_id,role|two writers read the same organization version|exactly one conflicting update commits, the loser receives conflict, and member and active-organization invariants remain valid|organization_id:id,winner_version:version,conflict_count:count,member_state_digest:digest,active_state_digest:digest
    organization-invitation-lifecycle-report|identity.organization.invitation-send,identity.organization.invitation.accept,identity.organization.invitation.cancel,identity.organization.invitation.expire|tenant_id,organization_id,email,role,invitation_token|recipient is not a member and no live invitation exists|send creates one invitation; accept adds one member; cancel, expiry, and replay cannot add membership|invitation_id:id,member_id:id,accepted_count:count,replay_rejected:boolean,invitation_event_digest:digest
    otp-reservation-report|identity.otp.send,identity.otp.check|tenant_id,purpose,recipient,code,idempotency_key,attempt_id,attempt_fingerprint,wrong_code,commit_fault|no live reservation exists for tenant, purpose, and recipient|concurrent send reserves one code and correct check consumes it once; each wrong-code logical submission uses a server-issued attempt identity, increments exactly once, stores one aborted command result with exhaustion when required, and ambiguous denial commit returns unknown until primary reconciliation; retry, concurrency, and replay never duplicate an attempt increment|reservation_id:id,attempt_id:id,issued_code_count:count,failed_attempt_count:count,aborted_result_count:count,consumed_code_count:count,duplicate_delivery_count:count,duplicate_attempt_increment_count:count,exhausted:boolean,reservation_state_digest:digest,unknown_reconciliation_digest:digest
    passkey-browser-ceremony-report|identity.passkey.register-options,identity.passkey.register-verify,identity.passkey.signin-options,identity.passkey.signin-verify,identity.passkey.list,identity.passkey.update,identity.passkey.delete|tenant_id,user_id,origin,rp_id,challenge,credential_response,clone_or_counter_evidence,credential_id,credential_version,name|trusted HTTPS origin and relying-party ID are configured and passkey-first policy requires a discoverable credential|registration creates one discoverable credential; list, usernameless sign-in, update, and delete remain linked to its exact credential ID; deleting the final recovery path is denied; ceremonies verify exact challenge, origin, RP ID, user verification, and counter policy; committed creation maps only to identity.passkey.create_credential and confirmed compromise maps only to identity.passkey.mark_compromised, while ordinary failed assertions emit neither action|credential_id:id,registered_credential_id:id,listed_credential_id:id,signin_credential_id:id,updated_credential_id:id,deleted_credential_id:id,discoverable_required:boolean,usernameless_signin_verified:boolean,discoverable_lifecycle_step_count:count,deleted_credential_absent:boolean,last_recovery_path_delete_rejected:boolean,challenge_digest:digest,sign_count:count,user_verified:boolean,replay_rejected:boolean,security_action_ids:passkey_security_action_ids,security_action_transitions:passkey_security_action_transitions,creation_event_count:count,compromise_event_count:count,ordinary_failure_compromise_event_count:count,security_event_digest:digest
    passwordless-http-report|identity.magic-link.request,identity.magic-link.consume,identity.otp.signin|tenant_id,identifier,redirect_uri,token_or_code,remember|passwordless request is rate-eligible and identifier disclosure is suppressed|request returns equivalent response, valid token or code creates one policy-bound session, and replay creates none|challenge_id:id,session_id:id,equivalent_response_digest:digest,replay_rejected:boolean,security_event_digest:digest
    phone-reset-risk-evidence-report|identity.phone.password-reset-request,identity.phone.password-reset-complete,identity.risk.evaluate|tenant_id,subject_id,recovery_purpose,canonical_number,preauth_transaction_id,attempt_id,policy_version,reset_capability,phone_otp_proof,independent_factor,risk_evidence_id,new_password|phone recovery is enabled and identity/risk has issued fresh immutable one-use RiskEvidence bound to tenant, subject, purpose, canonical number, pre-auth transaction, attempt, and policy version|request discloses no account state and completion atomically consumes reset capability, OTP, independent factor, and RiskEvidence with password rotation and session revocation; mismatch, replay, concurrency loss, expiry, carrier positive or unknown or unavailable, rollback, and unknown commit never duplicate or bypass recovery|attempt_id:id,risk_evidence_id:id,risk_policy_version:version,risk_evidence_digest:digest,successful_consumption_count:count,credential_version:version,revoked_session_count:count,rollback_restored_evidence:boolean,unknown_reconciliation_digest:digest
    platform-authority-version-report|identity.reference.config-validate|permission_catalog,role_catalog,policy_version|platform authority catalog and built-in roles are loaded|every permission statement and role binding resolves to one immutable policy version and stale versions are rejected|policy_version:version,permission_count:count,role_count:count,stale_version_rejected:boolean,authority_digest:digest
    postgres-lifecycle-cascade-report|identity.admin.user-delete,identity.organization.delete|tenant_id,user_id,organization_id,expected_version|identity and organization aggregates have all declared dependent PostgreSQL rows|delete locks the aggregate version and executes every restrict, revoke, tombstone, anonymize, and delete cascade atomically|aggregate_id:id,cascade_row_count:count,restricted_row_count:count,tombstoned_row_count:count,transaction_digest:digest
    postgres-valkey-failure-report|identity.session.refresh,identity.risk.evaluate|tenant_id,session_id,cache_key,failure_point|PostgreSQL is authoritative and Valkey contains a current projection|each injected PostgreSQL or Valkey outage follows declared fail policy and recovery rebuilds projection without accepting stale authority|session_id:id,stable_error:error,postgres_state_digest:digest,valkey_state_digest:digest,recovery_digest:digest,recovered:boolean
    privacy-boundary-report|identity.audit.export,identity.privacy-export.request|tenant_id,subject_id,actor_id,requested_scope|data classes have owner, purpose, retention, export, and redaction classifications|unauthorized scope is forbidden and authorized output contains only subject-owned exportable fields with audit attribution|subject_id:id,authorized_field_count:count,excluded_field_count:count,stable_error:error,audit_event_digest:digest
    privacy-export-lifecycle-report|identity.privacy-export.request,identity.privacy-export.status,identity.privacy-export.cancel,identity.privacy-export.download|tenant_id,subject_id,export_id,idempotency_key,contributor_checkpoints,worker_interrupt_after_fragment,projection_or_staged_fragment_mode,locale_preference,erasure_operation_id,erasure_failure_point,legal_hold_version|subject has exportable records including an identity/i18n locale preference and no equivalent active job; each exact contributor can reproduce its recorded checkpoint through a versioned projection or immutable request-transaction fragment|request captures the immutable exact contributor set including identity/i18n and version vector; restart reconstructs every contributor at the recorded checkpoint without a long-lived exported PostgreSQL snapshot; mixed cuts are rejected; cancel immediately denies download then reports erasure-pending, erasure-outcome-unknown, erased, or retained-encrypted-under-hold; timeout never claims erasure; status and reconciliation reuse one operation generation; hold release resumes erasure; completed download is bounded and auditable|export_id:id,erasure_operation_id:id,job_version:version,erasure_generation:version,exported_record_count:count,contributor_checkpoint_count:count,i18n_contributor_count:count,restart_resumed_contributor_count:count,worker_restart_count:count,long_lived_postgres_snapshot_count:count,mixed_cut_rejected:boolean,download_denied_after_cancel:boolean,outcome_unknown_not_erased:boolean,hold_release_resumed_erasure:boolean,snapshot_version_vector_digest:digest,restart_reconstruction_digest:digest,erasure_evidence_digest:digest,temporary_artifact_count:count,lifecycle_event_digest:digest
    provider-evidence-index|identity.reference.diagnostics|provider_catalog,provider_fixture_digests,interop_report_digests|pinned provider and protocol catalogs are loaded|each provider has version, fixture source, checksum, supported modes, latest execution revision, and attributable result|provider_count:count,indexed_provider_count:count,missing_evidence_count:count,catalog_digest:digest,evidence_index_digest:digest
    reauthentication-proof-report|identity.password.verify,identity.session.last-method-record,identity.session.last-method-check|tenant_id,user_id,password,session_id,required_freshness|authenticated session exists but has no fresh authentication proof|successful verification records method and time; freshness check accepts within bound and rejects expired or different-subject proof|session_id:id,proof_id:id,authentication_method:auth_method,freshness_seconds:count,expired_proof_rejected:boolean
    reference-http-identity-lifecycle|identity.user.create,identity.user.get,identity.user.update-admin,identity.user.suspend,identity.user.restore,identity.admin.user-delete|tenant_id,user_fields,user_id,expected_version,idempotency_key|tenant has no user with the canonical identifiers|HTTP create, get, update, suspend, restore, and delete expose exact versions and lifecycle state without cross-tenant access|user_id:id,created_version:version,final_version:version,final_state:user_state,lifecycle_event_digest:digest
    reference-http-password-journey|identity.password.signup,identity.password.signin,identity.password.change,identity.password.reset-request,identity.password.reset-complete,identity.email.address-list,identity.email.address-get,identity.email.address-remove,identity.phone.password-signin|tenant_id,email,username,password,new_password,reset_token,address_id,address_cursor,address_limit,address_expected_version,canonical_phone,remember,preauth_transaction|canonical email and username are unused in tenant and any address or phone lookup remains tenant scoped|signup creates one credential; signin and change rotate session policy; reset is enumeration-safe and invalidates old password; email list/get return bounded safe projections, remove enforces last-identifier and concurrency policy, and phone password signin uses the password-signin pre-auth transaction with canonical phone and remember policy|user_id:id,credential_version:version,session_id:id,old_password_rejected:boolean,enumeration_equivalent:boolean,address_list_count:count,address_get_count:count,address_removed:boolean,phone_password_session_id:id
    remember-policy-propagation-report|identity.password.signin,identity.session.get,identity.session.refresh|tenant_id,user_id,remember,session_id|tenant remember duration and absolute lifetime are configured|signin, session read, refresh, and bearer continuation preserve the selected remember duration without exceeding absolute lifetime|session_id:id,remember:boolean,idle_expiry_seconds:count,absolute_expiry_seconds:count,policy_digest:digest
    risk-policy-report|identity.risk.evaluate|tenant_id,operation_id,signals,policy_version|ordered risk rules and action thresholds are configured|same canonical signals and version yield exactly one allow, deny, throttle, or step_up action and explainable matched rule IDs; presentation may render step_up as a challenge but revoke is never a risk action|decision_id:id,policy_version:version,risk_score_basis_points:basis_points,action:risk_action,matched_rules_digest:digest
    scim-connection-lifecycle-report|identity.scim.connection-create,identity.scim.connection-rotate,identity.scim.connection-token-revoke,identity.scim.connection-delete|tenant_id,base_url,bearer_token,mapping_version,delete_failure_point,cascade_id,cascade_generation|tenant has no SCIM connection for base_url|create stores only token digest, rotate invalidates old token, and revoke stops calls; delete atomically denies local bearer/provisioning authority and closes only after every scim_connection_delete consumer reports applied, not-applicable, or an eligible limitation, while pending and outcome-unknown remain visible and provider ambiguity never claims deletion|connection_id:id,cascade_id:id,token_digest:digest,mapping_version:version,cascade_generation:version,old_token_rejected:boolean,local_authority_denied:boolean,delete_required_consumer_count:count,delete_closed_consumer_count:count,delete_outcome_unknown_count:count,remaining_schedule_count:count,delete_cascade_digest:digest
    scim-reconciliation-report|identity.scim.connection-reconcile,identity.scim.bulk|tenant_id,connection_id,cursor,mapping_version,remote_changes|connection has a committed cursor and local authoritative ownership map|reconciliation applies each remote version once, preserves application-owned fields, reports conflicts, and advances cursor only after commit|connection_id:id,applied_change_count:count,conflict_count:count,previous_cursor_digest:digest,committed_cursor_digest:digest
    scim-rfc-conformance-report|identity.scim.service-provider-config,identity.scim.schemas-list,identity.scim.schema-get,identity.scim.resource-types-list,identity.scim.resource-type-get,identity.scim.search,identity.scim.user-list,identity.scim.user-search,identity.scim.user-create,identity.scim.user-get,identity.scim.user-replace,identity.scim.user-patch,identity.scim.user-delete,identity.scim.group-list,identity.scim.group-search,identity.scim.group-create,identity.scim.group-get,identity.scim.group-replace,identity.scim.group-patch,identity.scim.group-delete,identity.scim.bulk|schemas,search_request,attributes,excluded_attributes,filter,start_index,count,sort_by,sort_order,writable_user,writable_group,external_id,bulk_operations,fail_on_errors|pinned RFC 7643 and 7644 fixtures, resource schemas, exact advertised capabilities, and an independent SCIM client suite are loaded|every advertised discovery, User, Group, GET list, POST search, replace, PATCH, delete, and Bulk operation matches pinned RFC fixtures; collections use exact ListResponse envelopes; writable inputs reject id and meta; externalId remains non-unique; scimType is omitted without a registered exact condition; Bulk results include method and conditional bulkId; failOnErrors rejects values above 100; ETag, filter, sort, projection, and pagination claims are exact|conformance_case_count:count,passed_case_count:count,advertised_operation_count:count,covered_operation_count:count,schema_digest:digest,search_response_digest:digest,filter_result_digest:digest,sort_result_digest:digest,etag_digest:digest,bulk_response_digest:digest
    secret-redaction-report|identity.reference.diagnostics,identity.reference.secret-generate|secret_classes,encoded_variants,partial_variants,output_surfaces|the exact password, API-key, OAuth-token, private-key, cookie, DSN, and derived-fragment classes are crossed with errors, logs, traces, diagnostics, snapshots, and generated-artifact surfaces|every exact class and surface pair has an independent scan result with zero full, encoded, or partial secret matches|secret_class_count:count,output_surface_count:count,full_secret_match_count:count,partial_secret_match_count:count,scan_digest:digest
    session-profile-contract-report|identity.session.get,identity.session.list,identity.session.refresh,identity.session.revoke-one|tenant_id,user_id,session_id,profile|active session was created under a named profile|read and list expose safe metadata, refresh follows rotation and expiry profile, and revoke prevents all later use|session_id:id,profile_id:id,rotation_version:version,revoked:boolean,session_state_digest:digest
    session-transfer-report|identity.session.transfer-generate,identity.session.transfer-consume|tenant_id,session_id,target_device,transfer_token|source session is active and target device is untrusted|one short-lived transfer token creates one policy-equivalent target session and is invalid after consume, expiry, or source revocation|transfer_id:id,source_session_id:id,target_session_id:id,consumed_count:count,replay_rejected:boolean
    signer-jwks-profile-report|identity.oauth-server.jwks,identity.oauth-server.token|issuer,key_id,signing_algorithm,rotation_version|active and retiring signing keys exist for issuer|token uses active key and allowed algorithm; JWKS publishes verification keys; rotation preserves old-token verification until retirement|key_id:id,signing_algorithm:signing_algorithm,jwks_key_count:count,old_token_verified:boolean,jwks_digest:digest
    sso-enforcement-break-glass-report|identity.sso.enforcement.update,identity.sso.break-glass.issue,identity.sso.break-glass.consume|tenant_id,organization_id,domain,enforcement_version,actor_id,recipient_id,break_glass_reason,expires_at,preauth_transaction_id|verified domain enforces SSO, local signin is disabled, and a separately tested recovery exclusion prevents administrator lockout|lockout-unsafe enforcement is rejected; only an authorized fresh owner issues one organization and policy-version-bound reveal-once capability; consume creates one bounded recovery session, emits a distinct use audit event, rejects mismatch, expiry and replay, and never weakens enforcement policy|grant_id:id,enforcement_version:version,consumed_count:count,replay_rejected:boolean,audit_event_digest:digest
    sso-provider-lifecycle-report|identity.sso.provider.register-oidc,identity.sso.provider.register-oauth,identity.sso.provider.register-saml,identity.sso.provider.update,identity.sso.provider.credentials-rotate,identity.sso.provider.disable,identity.sso.provider.delete,identity.sso.oauth-callback,identity.sso.oidc-callback,identity.sso.saml-metadata,identity.sso.saml-start,identity.sso.saml-acs,identity.sso.saml-idp-init,identity.sso.saml-logout-start,identity.sso.saml-slo|tenant_id,provider_type,metadata,credentials,mapping_version,oauth_callback,oidc_callback,saml_request,saml_response,relay_state,logout_request,logout_response,delete_failure_point,cascade_id,cascade_generation|tenant has no provider with the canonical issuer or entity ID and pinned federation interoperability fixtures are available|register validates metadata, update versions mappings, rotate invalidates credentials, and disable blocks login; delete establishes local denial and closes only after every enterprise_provider_delete consumer reports applied, not-applicable, or an eligible limitation, while pending/outcome-unknown remain visible; exact OAuth, OIDC and SAML execution proves federation against the pinned independent implementation|provider_id:id,cascade_id:id,mapping_version:version,credential_version:version,cascade_generation:version,provider_state:provider_state,local_denial_preserved:boolean,delete_required_consumer_count:count,delete_closed_consumer_count:count,delete_outcome_unknown_count:count,oauth_callback_verified:boolean,oidc_callback_verified:boolean,saml_endpoint_ids:saml_endpoint_ids,saml_endpoint_count:count,saml_interoperability_revision:saml_interoperability_revision,saml_metadata_verified:boolean,saml_start_verified:boolean,saml_acs_verified:boolean,saml_idp_init_verified:boolean,saml_logout_start_verified:boolean,saml_slo_verified:boolean,delete_cascade_digest:digest,saml_interoperability_transcript_digest:digest,lifecycle_event_digest:digest
    supply-chain-release-report|make check MODULES=pkg/identity/reference|module_set,dependency_lockfiles,artifact_digests,provenance_records|release candidate contains all affected modules and immutable dependency inputs|vulnerability, secret, license, SBOM, provenance, and clean-consumer gates pass and every release artifact digest is bound|module_count:count,artifact_count:count,sbom_digest:digest,provenance_digest:digest,release_report_digest:digest
    typed-fields-schema-report|identity.reference.schema-generate,identity.user.create,identity.user.get|field_declarations,defaults,validation_rules,permissions,sensitivity|typed user, account, and session field declarations are versioned|generated schema matches declarations and runtime create and read preserve types, defaults, permissions, redaction, and migration version|field_count:count,schema_version:version,defaulted_field_count:count,redacted_field_count:count,schema_digest:digest
    typed-hooks-report|identity.hooks.before,identity.hooks.after|operation_id,hook_order,typed_context,deadline|before and after hooks are registered with explicit order and immutable context|before hooks may deny before state change; after hooks observe result without mutating principal truth; timeout follows declared policy|before_hook_count:count,after_hook_count:count,executed_order_digest:digest,denial_prevented_write:boolean,hook_result_digest:digest
    unknown-outcome-reconciliation-report|identity.reference.migration-apply,identity.session.refresh|operation_id,idempotency_key,reconciliation_id,commit_fault|response is lost exactly at a PostgreSQL commit or external acceptance boundary|reconciliation returns committed or not_committed from durable identity and retry never duplicates the mutation or event|reconciliation_id:id,committed_count:count,not_committed_count:count,duplicate_effect_count:count,reconciliation_digest:digest
    webauthn-conformance-report|identity.mfa.security-key-register-options,identity.mfa.security-key-register-verify,identity.mfa.security-key-assert-options,identity.mfa.security-key-assert-verify,identity.passkey.register-verify,identity.passkey.signin-verify|rp_id,origin,challenge,credential_response,user_verification,backup_eligible,backup_state,stored_sign_count,received_sign_count|pinned WebAuthn fixtures cover valid and hostile authenticator data for non-backup security keys and backup-eligible passkeys|exact BE and BS execution accepts false/false, rejects false/true, and accepts true/false and true/true; both non-backup and backup-eligible authenticator paths execute; the exact zero, positive increase, equal, decrease, and received-zero counter matrix follows the selected policy, persists backup state on every valid assertion, never rolls counters back, emits bounded risk evidence for backup-eligible non-increase, and denies non-backup positive non-increase|backup_state_matrix:webauthn_backup_state_matrix,authenticator_path_ids:webauthn_authenticator_path_ids,authenticator_path_results:webauthn_authenticator_path_results,signature_counter_matrix:webauthn_counter_matrix,backup_state_case_count:count,valid_backup_state_case_count:count,rejected_backup_state_case_count:count,authenticator_path_count:count,counter_case_count:count,conformance_case_count:count,passed_case_count:count,credential_id:id,sign_count:count,fixture_digest:digest
  ROWS

  ENUMS = {
    "auth_method" => %w[password passkey otp oauth sso recovery_code], "cookie_mode" => %w[strict lax none],
    "device_status" => %w[pending approved denied expired], "error" => %w[none invalid_input unauthenticated forbidden freshness_required not_found conflict rate_limited unavailable cancelled deadline_exceeded unknown_commit internal],
    "health_status" => %w[live failed], "http_status" => [400, 401, 403, 404, 409, 429, 503],
    "locale" => %w[explicit accept_language tenant_default platform_default fallback], "provider_state" => %w[active disabled deleted],
    "readiness_status" => %w[ready unavailable draining], "risk_action" => %w[allow deny throttle step_up],
    "signing_algorithm" => %w[ES256 EdDSA PS256 RS256], "timeout_status" => %w[deny unavailable],
    "token_mode" => %w[id_token opaque_access_token], "user_state" => %w[active suspended anonymized deleted]
  }.freeze

  ZERO_EVIDENCE_FIELDS = /(?:successful_provider_count|password_transmission_count|full_hash_transmission_count|secret_match_count|full_secret_match_count|partial_secret_match_count|redaction_match_count|redirect_follow_count|absent_match_count|registration_access_token_issued_count|duplicate_count|duplicate_effect_count|duplicate_delivery_count|duplicate_attempt_increment_count|ordinary_failure_compromise_event_count|stateless_verifier_durable_write_count|long_lived_postgres_snapshot_count|identity_row_delta|account_row_delta|session_row_delta|missing_evidence_count|missing_action_count|remaining_fixture_count|remaining_schedule_count|temporary_artifact_count)/.freeze
  EQUALITY_RULES = {
    "affected-gate-report" => [["passed_module_count", "selected_module_count"]],
    "api-key-postgres-valkey-race-report" => [["postgres_state_digest", "valkey_state_digest"]],
    "audit-retention-plan-confirm-report" => [["deleted_record_count", "planned_record_count"]],
    "backup-restore-rehearsal" => [["source_digest", "restore_digest"]],
    "captcha-four-provider-report" => [["verified_provider_count", "provider_count"], ["protected_target_count", "middleware_attached_target_count"], ["protected_target_count", "durable_reservation_count"], ["protected_target_count", "durable_apply_count"], ["protected_target_count", "durable_finalize_count"]],
    "clean-reference-stack-report" => [["applied_migration_count", "migration_count"]],
    "coverage-mutation-report" => [["killed_mutant_count", "viable_mutant_count"]],
    "distributed-rate-limit-report" => [["postgres_counter", "valkey_counter"]],
    "impersonation-quorum-report" => [["distinct_approval_count", "required_quorum"]],
    "instrumentation-report" => [["metric_record_count", "log_record_count"], ["metric_record_count", "span_record_count"]],
    "operations-runbook-index" => [["covered_action_count", "runbook_count"]],
    "otp-reservation-report" => [["aborted_result_count", "failed_attempt_count"]],
    "passkey-browser-ceremony-report" => [["credential_id", "registered_credential_id"], ["credential_id", "listed_credential_id"], ["credential_id", "signin_credential_id"], ["credential_id", "updated_credential_id"], ["credential_id", "deleted_credential_id"]],
    "privacy-export-lifecycle-report" => [["restart_resumed_contributor_count", "contributor_checkpoint_count"]],
    "provider-evidence-index" => [["indexed_provider_count", "provider_count"]],
    "scim-rfc-conformance-report" => [["passed_case_count", "conformance_case_count"], ["advertised_operation_count", "covered_operation_count"]],
    "webauthn-conformance-report" => [["passed_case_count", "conformance_case_count"]]
  }.freeze
  CONST_RULES = {
    "coverage-mutation-report" => {"statement_coverage_basis_points" => 10_000},
    "captcha-four-provider-report" => {"provider_count" => 4, "verified_provider_count" => 4},
    "device-authorization-report" => {"token_issued_count" => 1},
    "download-capability-report" => {"download_count" => 1},
    "instrumentation-report" => {"metric_record_count" => 1, "log_record_count" => 1, "span_record_count" => 1},
    "oauth-popup-browser-report" => {"posted_message_count" => 1},
    "oauth-oidc-conformance-report" => {"endpoint_operation_count" => 13},
    "organization-concurrency-report" => {"conflict_count" => 1},
    "organization-invitation-lifecycle-report" => {"accepted_count" => 1},
    "otp-reservation-report" => {"issued_code_count" => 1, "consumed_code_count" => 1},
    "passkey-browser-ceremony-report" => {"creation_event_count" => 1, "compromise_event_count" => 1, "discoverable_lifecycle_step_count" => 5},
    "privacy-export-lifecycle-report" => {"worker_restart_count" => 1, "i18n_contributor_count" => 1},
    "phone-reset-risk-evidence-report" => {"successful_consumption_count" => 1},
    "scim-rfc-conformance-report" => {"advertised_operation_count" => 21},
    "session-transfer-report" => {"consumed_count" => 1},
    "sso-provider-lifecycle-report" => {"saml_endpoint_count" => 6},
    "sso-enforcement-break-glass-report" => {"consumed_count" => 1},
    "webauthn-conformance-report" => {"backup_state_case_count" => 4, "valid_backup_state_case_count" => 3, "rejected_backup_state_case_count" => 1, "authenticator_path_count" => 2, "counter_case_count" => 11}
  }.freeze

  module_function

  BLOCKED_ACCEPTANCE_ARTIFACTS = %w[
    api-key-postgres-valkey-race-report mfa-recovery-report oauth-oidc-conformance-report
    operations-runbook-index sso-enforcement-break-glass-report
  ].freeze
  END_STATE_ACCEPTANCE_KEYS = %w[
    schema_version authority digest_algorithm digest_input
    evidence_record_contract artifact_observation_contract
    input_identity_contract journeys cross_cutting artifact_catalog
  ].freeze

  def artifact_result_status(artifact_id)
    provider_blocked = artifact_id == "oauth-provider-matrix-report" && provider_matrix_rows.any? do |provider|
      provider.dig("evidence", "interoperability") == "not-run" || provider.dig("evidence", "official_docs").to_s.include?("required") ||
        !Array(provider.dig("oidc_logout", "evidence_blocker")).empty?
    end
    provider_blocked || BLOCKED_ACCEPTANCE_ARTIFACTS.include?(artifact_id) ? "blocked" : "pass"
  end

  def source
    JSON.parse(File.read(SOURCE))
  end

  def synchronize_supplemental_operation_claims!
    document = source
    rows = document.fetch("artifact_catalog").to_h { |row| [row.fetch("id"), row] }
    supplemental_operation_claims.each do |artifact_id, operation_ids|
      row = rows.fetch(artifact_id)
      row["operation_claims"] = operation_ids
      row["observation_ids"] = claim_ids(row).map { |claim_id| "#{artifact_id}.#{claim_id}.behavior" }
    end
    File.write(SOURCE, canonical_json(order_end_state_acceptance(document)))
    @profiles = nil
  end

  def order_end_state_acceptance(document)
    missing = END_STATE_ACCEPTANCE_KEYS - document.keys
    extra = document.keys - END_STATE_ACCEPTANCE_KEYS
    raise KeyError, "END_STATE_ACCEPTANCE top-level keys drifted: missing=#{missing.inspect} extra=#{extra.inspect}" unless missing.empty? && extra.empty?

    END_STATE_ACCEPTANCE_KEYS.to_h { |key| [key, document.fetch(key)] }
  end

  def supplemental_operation_claims
    expanded = SUPPLEMENTAL_OPERATION_CLAIMS.transform_values(&:dup)
    SUPPLEMENTAL_OPERATION_PREFIXES.each do |artifact_id, prefixes|
      selected = api_operation_ids.select { |operation_id| prefixes.any? { |prefix| operation_id.start_with?(prefix) } }
      selected.reject! { |operation_id| operation_id.start_with?("identity.organization.invitation.") } if artifact_id == "organization-concurrency-report"
      expanded[artifact_id] = (Array(expanded[artifact_id]) + selected).uniq.sort_by(&:b)
    end
    expanded
  end

  def profiles
    @profiles ||= begin
      result = PROFILE_ROWS.lines(chomp: true).reject(&:empty?).to_h do |line|
      id, operations, inputs, initial_state, invariant, fields = line.split("|", 6)
      [id, {
        "operation_ids" => operations.split(","), "input_fields" => inputs.split(","),
        "initial_state" => initial_state, "invariant" => invariant,
        "evidence_fields" => fields.split(",").to_h { |field| field.split(":", 2) }
      }]
      end
      result.fetch("captcha-four-provider-report")["operation_ids"] = ["identity.risk.captcha-verify", *captcha_protected_target_ids]
      result.fetch("operation-handler-openapi-bijection")["operation_ids"] = api_operation_ids
      result.fetch("operation-handler-openapi-bijection")["evidence_fields"].merge!(
        "catalog_operation_ids" => "api_operation_ids", "handler_operation_ids" => "handler_operation_ids",
        "openapi_operation_ids" => "openapi_operation_ids", "direct_operation_ids" => "direct_operation_ids",
        "middleware_operation_ids" => "middleware_operation_ids", "operation_bindings" => "operation_bindings"
      )
      %w[oauth-provider-matrix-report provider-evidence-index].each do |artifact_id|
        result.fetch(artifact_id).fetch("evidence_fields")["provider_results"] = "provider_matrix_results"
      end
      result.fetch("oauth-oidc-conformance-report").fetch("evidence_fields").delete("rfc7592_management_unselected")
      supplemental_operation_claims.each do |artifact_id, operation_ids|
        result.fetch(artifact_id)["operation_ids"] = operation_ids
      end
      {
        "postgres-lifecycle-cascade-report" => ["cascade_transitions", "cascade_transition_cases"],
        "unknown-outcome-reconciliation-report" => ["delivery_outcome_cases", "delivery_outcome_cases"],
        "scim-reconciliation-report" => ["reconciliation_cases", "scim_reconciliation_cases"],
        "otp-reservation-report" => ["otp_operation_cases", "otp_operation_cases"],
        "bearer-continuation-report" => ["bearer_transition_cases", "bearer_transition_cases"],
        "api-key-http-lifecycle-report" => ["api_key_security_cases", "api_key_security_cases"],
        "session-profile-contract-report" => ["session_race_cases", "session_race_cases"],
        "risk-policy-report" => ["trusted_profile_cases", "trusted_risk_profile_cases"],
        "remember-policy-propagation-report" => ["issuer_variant_cases", "remember_issuer_variant_cases"],
        "sso-provider-lifecycle-report" => ["repeat_sync_cases", "sso_repeat_sync_cases"],
        "oauth-provider-matrix-report" => ["redirect_link_cases", "social_redirect_link_cases"],
        "impersonation-quorum-report" => ["authority_transition_cases", "impersonation_authority_cases"]
      }.each do |artifact_id, (field, kind)|
        result.fetch(artifact_id).fetch("evidence_fields")[field] = kind
      end
      result.fetch("hibp-interoperability-report").fetch("evidence_fields").merge!(
        "local_password_input_count" => "count", "local_sha1_computation_count" => "count",
        "password_transmission_count" => "count", "full_hash_transmission_count" => "count"
      )
      result.fetch("scim-rfc-conformance-report").fetch("evidence_fields")["wire_contract_cases"] = "scim_wire_contract_cases"
      result.fetch("coverage-mutation-report").fetch("evidence_fields").merge!(
        "affected_package_results" => "coverage_package_results",
        "viable_mutant_results" => "viable_mutant_results",
        "affected_package_manifest_sha256" => "digest",
        "viable_mutant_manifest_sha256" => "digest"
      )
      result.fetch("oauth-oidc-conformance-report").fetch("evidence_fields")["operation_results"] = "oauth_operation_results"
      result.fetch("oauth-oidc-conformance-report").fetch("evidence_fields")["grant_replay_results"] = "oauth_grant_replay_results"
      result.fetch("oauth-provider-matrix-report").fetch("evidence_fields")["social_logout_results"] = "social_logout_results"
      result.fetch("oauth-provider-matrix-report").fetch("evidence_fields").merge!(
        "provider_catalog_ready" => "boolean", "unresolved_provider_count" => "count"
      )
      result.fetch("mfa-recovery-report").fetch("evidence_fields").merge!(
        "factor_lifecycle_results" => "mfa_factor_lifecycle_results",
        "totp_profile_results" => "totp_profile_results"
      )
      result.fetch("webauthn-conformance-report").fetch("evidence_fields").merge!(
        "ceremony_validation_results" => "webauthn_ceremony_validation_results",
        "selected_profile_results" => "webauthn_selected_profile_results"
      )
      result.fetch("captcha-four-provider-report").fetch("evidence_fields")["provider_negative_results"] = "captcha_provider_negative_results"
      result.fetch("browser-security-report").fetch("evidence_fields")["operation_security_results"] = "browser_operation_security_results"
      result.fetch("risk-policy-report").fetch("evidence_fields")["action_results"] = "risk_action_results"
      result.fetch("secret-redaction-report").fetch("evidence_fields")["class_surface_results"] = "redaction_class_surface_results"
      result.fetch("sso-enforcement-break-glass-report").fetch("evidence_fields")["safety_results"] = "sso_break_glass_safety_results"
      result.fetch("operations-runbook-index").fetch("evidence_fields")["action_results"] = "operations_runbook_results"
      result.fetch("api-key-http-lifecycle-report").fetch("evidence_fields")["permission_reduction_results"] = "api_key_permission_reduction_results"
      result.fetch("postgres-lifecycle-cascade-report").fetch("evidence_fields").merge!(
        "command_id_integrity_results" => "command_id_integrity_results", "audit_stage_results" => "audit_stage_results",
        "scim_bulk_skip_results" => "scim_bulk_skip_results"
      )
      result.fetch("api-key-postgres-valkey-race-report").fetch("evidence_fields")["possibly_debited_results"] = "api_key_possibly_debited_results"
      result.fetch("postgres-valkey-failure-report").fetch("evidence_fields")["session_authority_cache_results"] = "session_authority_cache_results"
      result.fetch("privacy-export-lifecycle-report").fetch("evidence_fields")["privacy_lifecycle_results"] = "privacy_lifecycle_results"
      result.fetch("sso-provider-lifecycle-report").fetch("evidence_fields")["provider_cascade_results"] = "enterprise_provider_cascade_results"
      result.fetch("scim-connection-lifecycle-report").fetch("evidence_fields")["connection_cascade_results"] = "scim_connection_cascade_results"

      requirements = JSON.parse(File.read(PROTOCOL_CONFORMANCE)).fetch("clause_pins")
      operation_by_id = canonical_operation_rows.to_h { |operation| [operation.fetch("id"), operation] }
      artifact_consumers = result.to_h do |artifact_id, profile|
        producer_unit = source.fetch("artifact_catalog").find { |row| row.fetch("id") == artifact_id }.fetch("producer_unit")
        consumers = [producer_unit, *profile.fetch("operation_ids").filter_map { |operation_id| operation_by_id[operation_id]&.fetch("owners") }].flatten.uniq
        [artifact_id, consumers.freeze]
      end.freeze
      allocation = result.keys.to_h { |artifact_id| [artifact_id, []] }
      requirements.each do |requirement|
        requirement_tokens = requirement.fetch("requirement_id").split(/[.-]/).reject { |token| %w[source rfc required unsupported validation].include?(token) }
        requirement_consumers = requirement.fetch("consumers")
        preferred_artifact = case requirement.fetch("requirement_id")
                             when /^captcha\./ then "captcha-four-provider-report"
                             when /^hibp\./ then "hibp-interoperability-report"
                             when /^(oauth|oidc)\./ then "oauth-oidc-conformance-report"
                             when /^(saml|source\.saml|source\.exclusive-xml|source\.rfc-6931)/ then "sso-provider-lifecycle-report"
                             when /^scim\./ then "scim-rfc-conformance-report"
                             when /^(webauthn|fido)\./ then "webauthn-conformance-report"
                             when /^mfa\./ then "mfa-recovery-report"
                             when /^source\./ then "supply-chain-release-report"
                             end
        preferred_artifact ||= if requirement_consumers.any? { |consumer| consumer.start_with?("oauth-server") }
                                 "oauth-oidc-conformance-report"
                               elsif requirement_consumers.any? { |consumer| consumer.start_with?("sso/") }
                                 "sso-provider-lifecycle-report"
                               elsif requirement_consumers.any? { |consumer| %w[webauthn passkey].include?(consumer.split("/").first) }
                                 "webauthn-conformance-report"
                               elsif requirement_consumers.any? { |consumer| consumer.start_with?("scim") }
                                 "scim-rfc-conformance-report"
                               elsif requirement_consumers.any? { |consumer| consumer.start_with?("identity/risk/captcha") }
                                 "captcha-four-provider-report"
                               elsif requirement_consumers.any? { |consumer| consumer.start_with?("identity/risk/hibp") }
                                 "hibp-interoperability-report"
                               end
        if preferred_artifact
          allocation.fetch(preferred_artifact) << requirement
          next
        end
        candidates = artifact_consumers.filter_map do |artifact_id, consumers|
          covered = requirement_consumers.select do |requirement_consumer|
            consumers.include?(requirement_consumer) || consumers.any? do |consumer|
              consumer != "identity" && requirement_consumer.start_with?("#{consumer}/")
            end
          end
          artifact_tokens = artifact_id.split(/[-.]/)
          consumer_tokens = requirement_consumers.flat_map { |consumer| consumer.split("/") }.reject { |token| token == "identity" }.uniq
          semantic_overlap = (artifact_tokens & consumer_tokens).length
          next if covered.empty? && semantic_overlap.zero?

          requirement_overlap = (artifact_tokens & requirement_tokens).length
          conformance_match = artifact_tokens.include?("conformance") && artifact_tokens.include?(requirement.fetch("requirement_id").split(/[.-]/).first)
          score = [covered.length, requirement_overlap, conformance_match ? 1 : 0, semantic_overlap,
                   -result.fetch(artifact_id).fetch("operation_ids").length, artifact_id.b]
          [artifact_id, covered, score]
        end
        selected = candidates.max_by(&:last)
        raise "protocol requirement #{requirement.fetch('requirement_id')} is unallocated" unless selected

        allocation.fetch(selected.first) << requirement
      end
      @protocol_artifact_requirements = allocation.transform_values { |rows| rows.uniq.freeze }.freeze
      allocated = @protocol_artifact_requirements.values.flatten.map { |row| row.fetch("requirement_id") }.uniq.sort_by(&:b)
      expected = requirements.map { |row| row.fetch("requirement_id") }.uniq.sort_by(&:b)
      raise "protocol requirements are not exhaustively allocated to acceptance artifacts" unless allocated == expected
      @protocol_artifact_requirements.each do |artifact_id, selected|
        next if selected.empty?

        result.fetch(artifact_id).fetch("evidence_fields")["protocol_case_results"] = "protocol_case_results:#{artifact_id}"
      end
      external_profile_assignments.each do |artifact_id, rows|
        next if rows.empty?

        result.fetch(artifact_id).fetch("evidence_fields").merge!(
          "external_profile_ids" => "external_profile_ids:#{artifact_id}",
          "external_profile_results" => "external_profile_results:#{artifact_id}"
        )
      end
      result
    end
  end

  def api_operation_ids
    @api_operation_ids ||= File.readlines(API_OPERATIONS, chomp: true)
      .filter_map { |line| line[/^\| `(identity\.[a-z0-9.-]+)` \|/, 1] }.uniq.freeze
  end

  def canonical_operation_rows
    @canonical_operation_rows ||= JSON.parse(File.read(OPERATIONS)).fetch("operations").freeze
  end

  def handler_operation_ids
    @handler_operation_ids ||= canonical_operation_rows.select { |row| %w[both protocol].include?(row.fetch("exposure")) }.map { |row| row.fetch("id") }.freeze
  end

  def openapi_operation_ids
    @openapi_operation_ids ||= canonical_operation_rows.select { |row| %w[both protocol].include?(row.fetch("exposure")) }.map { |row| row.fetch("openapi_operation_id") }.uniq.freeze
  end

  def exposure_operation_ids(exposure)
    canonical_operation_rows.select { |row| row.fetch("exposure") == exposure }.map { |row| row.fetch("id") }
  end

  def provider_matrix_rows
    @provider_matrix_rows ||= begin
      configuration = JSON.parse(File.read(CONFIGURATION_CATALOGS))
      provider_ids = configuration.dig("providers", "ids")
      rows = configuration.dig("provider_matrix", "rows")
      row_ids = rows.map { |row| row.fetch("id") }
      raise "provider matrix must contain the exact 43-provider catalog" unless provider_ids.length == 43 && row_ids == provider_ids && row_ids.uniq == row_ids

      rows.freeze
    end
  end

  def protocol_artifact_requirements
    profiles unless @protocol_artifact_requirements
    @protocol_artifact_requirements
  end

  def external_profile_assignments
    @external_profile_assignments ||= begin
      manifest = JSON.parse(File.read(PROTOCOL_CONFORMANCE))
      sources = manifest.fetch("sources").to_h { |row| [row.fetch("id"), row] }
      tools = manifest.fetch("tools").to_h { |row| [row.fetch("id"), row] }
      rows = EXTERNAL_PROFILE_SOURCES.map do |profile_id, (artifact_id, kind, source_id)|
        source = (kind == "source" ? sources : tools).fetch(source_id)
        [artifact_id, {
          "profile_id" => profile_id, "source_kind" => kind, "source_id" => source_id,
          "source_sha256" => "sha256:#{source.fetch('sha256')}", "consumer_ids" => source.fetch("consumers")
        }]
      end
      provider_matrix_rows.each do |provider|
        rows << ["oauth-provider-matrix-report", {
          "profile_id" => "provider-#{provider.fetch('id')}", "source_kind" => "provider-catalog",
          "source_id" => provider.fetch("id"),
          "source_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider)))}",
          "consumer_ids" => ["identity/oauth", "identity/oauth/providers"]
        }]
      end
      rows.group_by(&:first).transform_values { |entries| entries.map(&:last).freeze }.freeze
    end
  end

  def protocol_operation_ids(artifact_id, requirement_id)
    operation_ids = profiles.fetch(artifact_id).fetch("operation_ids").select { |id| id.start_with?("identity.") }
    return operation_ids if requirement_id.start_with?("source.")

    requirement = protocol_artifact_requirements.fetch(artifact_id).find { |row| row.fetch("requirement_id") == requirement_id }
    operation_by_id = canonical_operation_rows.to_h { |operation| [operation.fetch("id"), operation] }
    selected = operation_ids.select do |operation_id|
      owners = operation_by_id.fetch(operation_id).fetch("owners")
      !(owners & requirement.fetch("consumers")).empty?
    end
    selected.empty? ? operation_ids : selected
  end

  def scim_bulk_max_operations
    line = File.readlines(REFERENCE_CONFIGURATION, chomp: true).find { |entry| entry.start_with?("| `scim.bulk.operations` |") }
    value = line&.match(/\| ([0-9,]+) operations \|/)&.captures&.first&.delete(",")&.to_i
    raise "configured SCIM Bulk operation limit is not exactly 1000" unless value == 1000

    value
  end

  def captcha_protected_target_ids
    @captcha_protected_target_ids ||= begin
      row = File.readlines(API_OPERATIONS, chomp: true).find { |line| line.start_with?("| `identity.risk.captcha-verify` |") }
      raise "canonical CAPTCHA middleware attachment row is missing" unless row

      identifiers = row.scan(/`([^`]+)`/).flatten
      raise "canonical CAPTCHA middleware attachment row is malformed" unless identifiers.shift == "identity.risk.captcha-verify"
      raise "canonical CAPTCHA middleware attachment targets are empty or duplicated" if identifiers.empty? || identifiers.uniq != identifiers

      identifiers.freeze
    end
  end

  def captcha_target_flow_contexts
    @captcha_target_flow_contexts ||= begin
      pre_auth = %w[pre_auth_transaction].freeze
      authenticated = %w[authenticated_subject_session].freeze
      administrative = %w[admin_actor].freeze
      mapping = captcha_protected_target_ids.to_h { |target| [target, pre_auth] }
      %w[identity.password.change].each { |target| mapping[target] = authenticated }
      mapping["identity.password.set"] = %w[pre_auth_transaction authenticated_subject_session admin_actor].freeze
      mapping["identity.admin.user-password-set"] = administrative
      %w[identity.otp.send identity.phone.send-verification].each { |target| mapping[target] = %w[pre_auth_transaction authenticated_subject_session].freeze }
      mapping.freeze
    end
  end

  def scenario_names(artifact_id)
    COMMON_SCENARIOS + EXTRA_SCENARIOS.filter_map { |name, ids| name if ids.include?(artifact_id) }
  end

  def scenario_names_for_claim(row, claim_id)
    return %w[success denial failure] if row.fetch("id") == "audit-retention-plan-confirm-report" && claim_id.end_with?(".plan")
    return %w[success denial failure replay concurrency cleanup unknown-outcome] if row.fetch("id") == "audit-retention-plan-confirm-report" && claim_id.end_with?(".confirm")
    if row.fetch("id") == "phone-reset-risk-evidence-report"
      return %w[success denial failure outage replay concurrency rollback expiry cleanup unknown-outcome] if claim_id.end_with?(".password-reset-request")
      return %w[success denial failure outage replay concurrency rollback expiry cleanup unknown-outcome] if claim_id.end_with?(".password-reset-complete")
      return %w[success denial failure outage replay expiry unknown-outcome] if claim_id == "identity.risk.evaluate"
    end
    return %w[success denial failure] if %w[identity.email.address-get identity.email.address-list].include?(claim_id)
    return %w[success denial failure replay concurrency cleanup unknown-outcome] if claim_id == "identity.email.address-remove"
    return %w[success denial failure outage replay] if claim_id == "identity.phone.password-signin"
    return %w[success denial failure replay concurrency cleanup unknown-outcome] if claim_id == "identity.admin.mfa-reset"
    return %w[success denial failure outage replay concurrency cleanup unknown-outcome] if claim_id == "identity.admin.mfa-recovery-issue"
    return %w[success denial failure outage replay cleanup] if claim_id == "identity.apikey.session-authenticate"
    return %w[success denial failure outage replay expiry cleanup unknown-outcome] if %w[identity.oauth.logout-start identity.oauth.logout-complete].include?(claim_id)
    return %w[success denial failure] if %w[identity.audit.get identity.audit.list identity.audit.search].include?(claim_id)
    return %w[success denial failure outage cleanup] if claim_id == "identity.audit.export"
    scenario_names(row.fetch("id"))
  end

  def claim_ids(row)
    (Array(row["claims"]) + Array(row["operation_claims"])).uniq.sort_by(&:b)
  end

  def operation_ids_for_claim(row, claim_id)
    return [claim_id] if Array(row["operation_claims"]).include?(claim_id)
    profiles.fetch(row.fetch("id")).fetch("operation_ids")
  end

  def input_fields_for_claim(row, claim_id)
    return %w[tenant_id policy_version cutoff] if claim_id == "identity.audit-retention.deletion.plan"
    return %w[tenant_id plan_id plan_digest] if claim_id == "identity.audit-retention.deletion.confirm"
    if row.fetch("id") == "phone-reset-risk-evidence-report"
      return %w[tenant_id subject_id recovery_purpose canonical_number preauth_transaction_id attempt_id policy_version] if claim_id == "identity.risk.evaluate"
      return %w[tenant_id subject_id recovery_purpose canonical_number preauth_transaction_id attempt_id policy_version] if claim_id.end_with?(".password-reset-request")
    end
    return %w[tenant_id address_cursor address_limit] if claim_id == "identity.email.address-list"
    return %w[tenant_id address_id] if claim_id == "identity.email.address-get"
    return %w[tenant_id address_id address_expected_version] if claim_id == "identity.email.address-remove"
    return %w[tenant_id canonical_phone password remember preauth_transaction] if claim_id == "identity.phone.password-signin"
    return %w[tenant_id user_id reset_generation approval_id reason] if claim_id == "identity.admin.mfa-reset"
    return %w[tenant_id user_id reset_generation approval_id approved_recovery_channel reason] if claim_id == "identity.admin.mfa-recovery-issue"
    return %w[tenant_id api_key_secret session_authentication_enabled revocation_version] if claim_id == "identity.apikey.session-authenticate"
    return %w[tenant_id provider_id session_id session_version post_logout_redirect_uri state] if claim_id == "identity.oauth.logout-start"
    return %w[tenant_id provider_id state provider_response] if claim_id == "identity.oauth.logout-complete"
    return %w[tenant_id investigation_id record_id field_grants] if claim_id == "identity.audit.get"
    return %w[tenant_id investigation_id indexed_predicates time_bounds field_grants] if claim_id == "identity.audit.search"
    return %w[tenant_id investigation_id cursor limit time_bounds field_grants] if claim_id == "identity.audit.list"
    return %w[tenant_id investigation_id indexed_predicates time_bounds field_grants export_byte_limit] if claim_id == "identity.audit.export"
    profiles.fetch(row.fetch("id")).fetch("input_fields")
  end

  def observation_specs(row)
    profile = profiles.fetch(row.fetch("id"))
    claim_ids(row).each_with_index.map do |claim_id, index|
      cases = scenario_names_for_claim(row, claim_id)
      operations = operation_ids_for_claim(row, claim_id)
      inputs = input_fields_for_claim(row, claim_id)
      stable = cases.map { |scenario| "#{scenario}=#{scenario_outcome(scenario).join('/')}" }.join(", ")
      expected = "Invariant: #{profile.fetch('invariant')}; stable outcomes: #{stable}; required artifact fields: #{profile.fetch('evidence_fields').keys.join(', ')}"
      {
        "observation_id" => row.fetch("observation_ids").fetch(index),
        "claim_id" => claim_id,
        "contract_reference" => row.fetch("schema"),
        "scenario" => cases.join(","),
        "preconditions" => "Operations: #{operations.join(', ')}; exact inputs: #{inputs.join(', ')}; initial state: #{profile.fetch('initial_state')}",
        "stimulus" => "Run #{operations.join(' then ')} for #{cases.join(', ')} cases using #{inputs.join(', ')} and capture every required artifact field",
        "expected_outcome" => expected,
        "actual_outcome" => expected,
        "result" => "pass"
      }
    end
  end

  def scenario_outcome(scenario)
    {
      "success" => %w[pass none], "denial" => %w[denied forbidden],
      "failure" => %w[failed internal], "outage" => %w[unavailable unavailable],
      "replay" => %w[denied conflict], "concurrency" => %w[pass conflict],
      "cleanup" => %w[pass none], "unknown-outcome" => %w[reconciled unknown_commit],
      "rollback" => %w[rolled_back internal], "expiry" => %w[denied invalid_input]
    }.fetch(scenario)
  end

  def catalog_document
    operations_by_id = JSON.parse(File.read(OPERATIONS)).fetch("operations").to_h { |operation| [operation.fetch("id"), operation] }
    artifacts = source.fetch("artifact_catalog").sort_by { |row| row.fetch("id") }.map do |row|
      id = row.fetch("id")
      covered_operations = profiles.fetch(id).fetch("operation_ids").map do |operation_id|
        if operation_id.start_with?("make check ")
          {"id" => operation_id, "owners" => [row.fetch("producer_unit")], "kind" => "repository_gate"}
        else
          operation = operations_by_id.fetch(operation_id)
          {"id" => operation_id, "owners" => operation.fetch("owners"), "kind" => "platform_operation"}
        end
      end
      {
        "artifact_id" => id,
        "schema_id" => row.fetch("schema"),
        "schema_path" => ".ai/identity-platform/acceptance/v1/schemas/#{id}.schema.json",
        "producer" => {
          "unit" => row.fetch("producer_unit"),
          "operation_kind" => "repository_gate",
          "operation" => row.fetch("gate")
        },
        "canonical_output_path" => row.fetch("path"),
        "covered_operations" => covered_operations,
        "claim_ids" => claim_ids(row),
        "required_observations" => observation_specs(row),
        "artifact_evidence_schema_ref" => ".ai/identity-platform/acceptance/v1/schemas/#{id}.schema.json#/$defs/artifact_evidence",
        "artifact_evidence_output_path" => ".ai/identity-platform/evidence/artifacts/#{id}.json",
        "artifact_evidence_fields" => profiles.fetch(id).fetch("evidence_fields").keys
      }
    end
    {
      "schema_version" => 1,
      "authority" => "END_STATE_ACCEPTANCE.json",
      "program_unit_catalog" => {"path" => ".ai/identity-platform/GOAL_MANIFEST.json", "required_count" => 67},
      "product_contract_unit_catalog" => {"path" => ".ai/identity-platform/PUBLIC_CONTRACTS.json", "required_count" => 61},
      "primitive_prerequisites" => PRIMITIVE_PREREQUISITES,
      "operation_catalog" => {"path" => ".ai/identity-platform/OPERATION_SEMANTICS.json", "required_count" => api_operation_ids.length},
      "artifacts" => artifacts
    }
  end


  def exact_variants(variants)
    {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
     "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
  end

  def field_schema(kind)
    return {"const" => api_operation_ids} if kind == "api_operation_ids"
    return {"const" => handler_operation_ids} if kind == "handler_operation_ids"
    return {"const" => openapi_operation_ids} if kind == "openapi_operation_ids"
    return {"const" => exposure_operation_ids("direct")} if kind == "direct_operation_ids"
    return {"const" => exposure_operation_ids("middleware")} if kind == "middleware_operation_ids"
    if kind.start_with?("external_profile_ids:")
      artifact_id = kind.split(":", 2).last
      return {"const" => external_profile_assignments.fetch(artifact_id).map { |row| row.fetch("profile_id") }}
    end
    if kind.start_with?("external_profile_results:")
      artifact_id = kind.split(":", 2).last
      variants = external_profile_assignments.fetch(artifact_id).map do |profile|
        properties = profile.transform_values { |value| {"const" => value} }
        properties["execution_outcome"] = {"const" => profile.fetch("source_kind") == "provider-catalog" ? "not-run" : "passed"}
        properties["transcript_sha256"] = field_schema("digest")
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "operation_bindings"
      exact = canonical_operation_rows.select { |row| %w[both protocol].include?(row.fetch("exposure")) }.map do |operation|
        operation_id = operation.fetch("id")
        {"operation_id" => operation_id, "handler_id" => operation_id.delete_prefix("identity.").tr(".", "/"),
         "openapi_operation_id" => operation.fetch("openapi_operation_id")}
      end
      properties = {
        "operation_id" => {"type" => "string", "enum" => handler_operation_ids},
        "handler_id" => {"type" => "string", "minLength" => 1},
        "openapi_operation_id" => {"type" => "string", "enum" => openapi_operation_ids},
        "executed" => {"const" => true}, "transcript_sha256" => field_schema("digest")
      }
      return {"type" => "array", "minItems" => exact.length, "maxItems" => exact.length, "uniqueItems" => true,
              "items" => {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties},
              "x-exact-operation-bindings" => exact}
    end
    if kind == "provider_matrix_results"
      variants = provider_matrix_rows.map do |provider|
        catalog_blocker_count = Array(provider.dig("oidc_logout", "evidence_blocker")).length
        catalog_verified = !provider.dig("evidence", "official_docs").to_s.include?("required") &&
          provider.dig("evidence", "interoperability") != "not-run" && catalog_blocker_count.zero?
        properties = {
          "provider_id" => {"const" => provider.fetch("id")},
          "catalog_row_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider)))}"},
          "protocol" => {"const" => provider.fetch("protocol")},
          "response_type" => {"const" => provider.dig("response", "type")},
          "response_mode" => {"const" => provider.dig("response", "mode")},
          "token_auth" => {"const" => provider.fetch("token_auth")},
          "pkce_status" => {"const" => provider.dig("pkce", "status")},
          "nonce_status" => {"const" => provider.dig("nonce", "status")},
          "state_status" => {"const" => provider.dig("state", "status")},
          "native_token_modes" => {"const" => provider.dig("native_token_signin", "modes")},
          "configuration_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider.fetch('configuration'))))}"},
          "endpoint_profile_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider.fetch('endpoints'))))}"},
          "scope_profile_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider.fetch('scopes'))))}"},
          "identity_source_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider.fetch('identity_sources'))))}"},
          "claim_mapping_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider.fetch('claims'))))}"},
          "account_policy_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider.fetch('account_policy'))))}"},
          "token_lifecycle_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider.fetch('token_lifecycle'))))}"},
          "incompatibility_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider.fetch('incompatibilities'))))}"},
          "source_locator" => {"const" => provider.dig("evidence", "source")},
          "official_docs_status" => {"const" => provider.dig("evidence", "official_docs")},
          "catalog_interoperability_status" => {"const" => provider.dig("evidence", "interoperability")},
          "catalog_blocker_count" => {"const" => catalog_blocker_count},
          "execution_outcome" => {"const" => catalog_verified ? "passed" : "not-run"}, "verified" => {"const" => catalog_verified},
          "transcript_sha256" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => 43, "maxItems" => 43, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "coverage_package_results"
      properties = {
        "package_id" => {"type" => "string", "pattern" => "^pkg/[a-z0-9][a-z0-9./-]*$"},
        "statement_count" => {"type" => "integer", "minimum" => 1, "maximum" => 2_147_483_647},
        "covered_statement_count" => {"type" => "integer", "minimum" => 1, "maximum" => 2_147_483_647},
        "statement_coverage_basis_points" => {"const" => 10_000},
        "profile_sha256" => field_schema("digest"), "transcript_sha256" => field_schema("digest")
      }
      return {"type" => "array", "minItems" => 1, "maxItems" => 10_000, "uniqueItems" => true,
              "items" => {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}}
    end
    if kind == "viable_mutant_results"
      properties = {
        "mutant_id" => {"type" => "string", "minLength" => 1, "maxLength" => 512},
        "package_id" => {"type" => "string", "pattern" => "^pkg/[a-z0-9][a-z0-9./-]*$"},
        "operator" => {"type" => "string", "minLength" => 1, "maxLength" => 128},
        "source_locator" => {"type" => "string", "minLength" => 1, "maxLength" => 1024},
        "outcome" => {"const" => "killed"}, "transcript_sha256" => field_schema("digest")
      }
      return {"type" => "array", "minItems" => 1, "maxItems" => 1_000_000, "uniqueItems" => true,
              "items" => {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}}
    end
    if kind == "oauth_operation_results"
      variants = profiles.fetch("oauth-oidc-conformance-report").fetch("operation_ids").map do |operation_id|
        properties = {
          "operation_id" => {"const" => operation_id}, "executed" => {"const" => true},
          "request_sha256" => field_schema("digest"), "response_sha256" => field_schema("digest"),
          "observed_outcome" => {"const" => "passed"}, "transcript_sha256" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "oauth_grant_replay_results"
      cases = [
        ["authorization-code-single-use", "authorization_code", "authorization_code", "replay-rejected", true, false],
        ["refresh-family-rotation", "refresh_token", "refresh_token", "successor-issued", true, false],
        ["refresh-family-reuse", "refresh_token", "refresh_token", "family-revoked", true, false],
        ["client-credentials", "client_credentials", "client_secret", "token-issued", false, false],
        ["private-key-jwt-jti-reuse", "client_credentials", "client_assertion", "replay-rejected", true, true]
      ]
      variants = cases.map do |case_id, grant_type, credential_kind, outcome, replay_rejected, jti_present|
        request_input = {"case_id" => case_id, "grant_type" => grant_type, "credential_kind" => credential_kind,
                         "credential_id" => credential_kind == "refresh_token" ? "refresh-family-credential" : "#{credential_kind}-credential"}
        request_sha256 = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(request_input)))}"
        credential_sha256 = "sha256:#{Digest::SHA256.hexdigest("oauth-credential-v1\n#{request_sha256}")}"
        family_sha256 = credential_kind == "refresh_token" ? "sha256:#{Digest::SHA256.hexdigest("refresh-family-v1\n#{credential_sha256}")}" : "sha256:#{'0' * 64}"
        properties = {"case_id" => {"const" => case_id}, "grant_type" => {"const" => grant_type},
          "credential_kind" => {"const" => credential_kind}, "jti_present" => {"const" => jti_present},
          "credential_id" => {"const" => credential_kind == "refresh_token" ? "refresh-family-credential" : "#{credential_kind}-credential"},
          "family_id" => {"const" => credential_kind == "refresh_token" ? "refresh-family" : "not-applicable"},
          "parent_credential_id" => {"const" => case_id == "refresh-family-rotation" ? "refresh-family-credential" : "not-applicable"},
          "successor_credential_id" => {"const" => case_id == "refresh-family-rotation" ? "refresh-family-successor" : "not-applicable"},
          "first_use_count" => {"const" => 1}, "replay_attempt_count" => {"const" => replay_rejected ? 1 : 0},
          "family_revoked_after_reuse" => {"const" => case_id == "refresh-family-reuse"},
          "successor_rejected_after_reuse" => {"const" => case_id == "refresh-family-reuse"},
          "canonical_request_sha256" => {"const" => request_sha256}, "credential_sha256" => {"const" => credential_sha256},
          "family_sha256" => {"const" => family_sha256}, "first_use_issuance_count" => {"const" => 1},
          "first_use_result_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest("first-use-result-v1\n#{credential_sha256}\n#{outcome}")}"},
          "replay_credential_sha256" => {"const" => credential_sha256}, "replay_family_sha256" => {"const" => family_sha256},
          "observed_outcome" => {"const" => outcome}, "replay_rejected" => {"const" => replay_rejected},
          "request_sha256" => field_schema("digest"), "response_sha256" => field_schema("digest"), "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "social_logout_results"
      variants = provider_matrix_rows.map do |provider|
        blocker_count = Array(provider.dig("oidc_logout", "evidence_blocker")).length
        properties = {"case_id" => {"const" => "#{provider.fetch('id')}-logout-profile"}, "provider_id" => {"const" => provider.fetch("id")},
          "catalog_row_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(provider)))}"},
          "logout_status" => {"const" => provider.dig("oidc_logout", "status")}, "catalog_blocker_count" => {"const" => blocker_count},
          "start_operation_id" => {"const" => "identity.oauth.logout-start"}, "complete_operation_id" => {"const" => "identity.oauth.logout-complete"},
          "state_single_use" => {"const" => true}, "state_bound" => {"const" => true},
          "local_session_revoked" => {"const" => true}, "replay_rejected" => {"const" => true}, "timeout_local_revocation_preserved" => {"const" => true},
          "unknown_reconciliation_required" => {"const" => true},
          "execution_outcome" => {"const" => blocker_count.zero? ? "passed" : "not-run"}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "mfa_factor_lifecycle_results"
      cases = [
        ["totp-enroll-verify", %w[identity.mfa.totp-uri identity.mfa.totp-verify], "passed", false],
        ["totp-replay", %w[identity.mfa.totp-verify], "denied", true],
        ["otp-send-verify", %w[identity.mfa.otp-send identity.mfa.otp-verify], "passed", false],
        ["otp-expired", %w[identity.mfa.otp-verify], "denied", true],
        ["security-key-register-assert", %w[identity.mfa.security-key-register-options identity.mfa.security-key-register-verify identity.mfa.security-key-assert-options identity.mfa.security-key-assert-verify], "passed", false],
        ["security-key-challenge-replay", %w[identity.mfa.security-key-assert-verify], "denied", true],
        ["recovery-regenerate-use", %w[identity.mfa.recovery-regenerate identity.mfa.recovery-use], "passed", false],
        ["recovery-code-replay", %w[identity.mfa.recovery-use], "denied", true],
        ["trusted-device-revoke", %w[identity.mfa.trusted-device-revoke], "passed", false],
        ["admin-reset-independent-approval", %w[identity.admin.mfa-reset identity.admin.mfa-recovery-issue], "passed", false]
      ]
      variants = cases.map do |case_id, operation_ids, outcome, replay_rejected|
        properties = {
          "case_id" => {"const" => case_id}, "operation_ids" => {"const" => operation_ids},
          "observed_outcome" => {"const" => outcome}, "replay_rejected" => {"const" => replay_rejected},
          "secret_disclosure_count" => {"const" => 0}, "transcript_sha256" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "totp_profile_results"
      profiles = [["sha1-6-30", "SHA-1", 6, 30]]
      conformance = JSON.parse(File.read(PROTOCOL_CONFORMANCE))
      rfc6238 = conformance.fetch("sources").find { |source| source.fetch("id") == "rfc-6238" }
      key_uri = conformance.fetch("sources").find { |source| source.fetch("id") == "google-authenticator-key-uri-8ba6e793" }
      variants = profiles.map do |profile_id, algorithm, digits, period|
        properties = {"profile_id" => {"const" => profile_id}, "algorithm" => {"const" => algorithm},
          "digits" => {"const" => digits}, "period_seconds" => {"const" => period},
          "rfc6238_vector_count" => {"const" => 6}, "rfc6238_vectors_passed" => {"const" => true},
          "independent_authenticator_app" => {"const" => "google-authenticator-compatible-independent-verifier"},
          "authenticator_app_version" => {"const" => key_uri.fetch("id")},
          "authenticator_app_binary_sha256" => {"const" => "sha256:#{key_uri.fetch('sha256')}"},
          "rfc6238_source_sha256" => {"const" => "sha256:#{rfc6238.fetch('sha256')}"},
          "independent_app_result" => {"const" => "passed"}, "provisioning_uri_verified" => {"const" => true},
          "vector_transcript_sha256" => field_schema("digest"), "app_transcript_sha256" => field_schema("digest"), "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "webauthn_ceremony_validation_results"
      cases = [
        ["registration-valid", "registration", "valid", true],
        ["registration-challenge-mismatch", "registration", "challenge", false],
        ["registration-origin-mismatch", "registration", "origin", false],
        ["registration-rp-id-hash-mismatch", "registration", "rp-id-hash", false],
        ["registration-user-verification-missing", "registration", "user-verification", false],
        ["registration-untrusted-metadata", "registration", "metadata", false],
        ["assertion-valid", "assertion", "valid", true],
        ["assertion-challenge-mismatch", "assertion", "challenge", false],
        ["assertion-origin-mismatch", "assertion", "origin", false],
        ["assertion-rp-id-hash-mismatch", "assertion", "rp-id-hash", false],
        ["assertion-user-presence-missing", "assertion", "user-presence", false],
        ["assertion-signature-invalid", "assertion", "signature", false]
      ]
      variants = cases.map do |case_id, ceremony, validation, accepted|
        properties = {
          "case_id" => {"const" => case_id}, "ceremony" => {"const" => ceremony},
          "validation" => {"const" => validation}, "accepted" => {"const" => accepted},
          "state_mutation_count" => {"const" => accepted ? 1 : 0}, "transcript_sha256" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "webauthn_selected_profile_results"
      dimensions = {
        "algorithm" => %w[ES256 EdDSA RS256-compatibility],
        "attestation" => %w[none packed-structural fido-u2f-structural],
        "extension" => %w[credProps credProtect unknown-discarded uvm-unselected appid-unselected],
        "resident-key" => %w[required preferred discouraged],
        "user-verification" => %w[required preferred discouraged]
      }
      variants = dimensions.flat_map do |dimension, values|
        values.map do |profile|
          properties = {"dimension" => {"const" => dimension}, "profile" => {"const" => profile},
            "selected" => {"const" => !profile.end_with?("unselected")}, "browser_result" => {"const" => "passed"},
            "authenticator_result" => {"const" => profile.include?("structural") || profile.end_with?("unselected") ? "rejected-as-untrusted-or-unselected" : "passed"},
            "independent_verifier" => {"const" => "web-platform-tests"},
            "verifier_revision_sha256" => {"const" => "sha256:559c6d1edbff75dfbacb10a3466a36da8897dc3f586da1866a7b718f928a837a"},
            "fixture_sha256" => field_schema("digest"), "transcript_sha256" => field_schema("digest")}
          {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
        end
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "captcha_provider_negative_results"
      cases = %w[recaptcha turnstile hcaptcha captchafox].product(%w[invalid-token hostname-mismatch replay]) +
              %w[recaptcha turnstile].product(%w[action-mismatch])
      variants = cases.map do |provider_id, failure_mode|
        properties = {
          "case_id" => {"const" => "#{provider_id}:#{failure_mode}"}, "provider_id" => {"const" => provider_id},
          "failure_mode" => {"const" => failure_mode}, "observed_outcome" => {"const" => "denied"},
          "identity_mutation_count" => {"const" => 0}, "transcript_sha256" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind.start_with?("protocol_case_results:")
      artifact_id = kind.split(":", 2).last
      variants = protocol_artifact_requirements.fetch(artifact_id).map do |requirement|
        operation_ids = protocol_operation_ids(artifact_id, requirement.fetch("requirement_id"))
        outcome = requirement.fetch("disposition") == "unsupported" ? "unsupported" : "passed"
        properties = {
          "case_id" => {"const" => "#{artifact_id}:#{requirement.fetch('requirement_id')}"},
          "suite_case_id" => {"const" => "#{requirement.fetch('source_id')}:#{requirement.fetch('requirement_id')}"},
          "requirement_id" => {"const" => requirement.fetch("requirement_id")},
          "consumer_ids" => {"const" => requirement.fetch("consumers")},
          "operation_ids" => {"const" => operation_ids}, "outcome" => {"const" => outcome},
          "source_id" => {"const" => requirement.fetch("source_id")}, "locator" => {"const" => requirement.fetch("locator")},
          "transcript_sha256" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "scim_wire_contract_cases"
      cases = [
        {"case_id" => "service-provider-config", "operation_id" => "identity.scim.service-provider-config",
         "schemas" => ["urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"], "patch_supported" => true,
         "bulk_supported" => true, "bulk_max_operations" => scim_bulk_max_operations},
        {"case_id" => "schemas-list-response", "operation_id" => "identity.scim.schemas-list",
         "schemas" => ["urn:ietf:params:scim:api:messages:2.0:ListResponse"], "total_results" => 2,
         "start_index" => 1, "items_per_page" => 2},
        {"case_id" => "invalid-filter-error", "operation_id" => "identity.scim.search",
         "schemas" => ["urn:ietf:params:scim:api:messages:2.0:Error"], "status" => "400", "scim_type" => "invalidFilter"},
        {"case_id" => "bulk-too-many-error", "operation_id" => "identity.scim.bulk",
         "schemas" => ["urn:ietf:params:scim:api:messages:2.0:Error"], "status" => "413", "scim_type" => "tooMany"}
      ]
      variants = cases.map do |entry|
        properties = entry.to_h { |key, value| [key, {"const" => value}] }
        properties["transcript_sha256"] = field_schema("digest")
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    artifact_cases = {
      "cascade_transition_cases" => [
        {"case_id" => "owner-admission", "generation" => 1, "owner_commit_count" => 1,
         "consumer_ids" => %w[api-key audit authorization-cache impersonation oauth-grants risk session], "acknowledged_consumer_ids" => [],
         "consumer_mutation_count" => 0, "consumer_ack_count" => 0, "terminalization_count" => 0, "state" => "pending"},
        {"case_id" => "generation-bound-closure", "generation" => 1, "owner_commit_count" => 1,
         "consumer_ids" => %w[api-key audit authorization-cache impersonation oauth-grants risk session],
         "acknowledged_consumer_ids" => %w[api-key audit authorization-cache impersonation oauth-grants risk session],
         "consumer_mutation_count" => 7, "consumer_ack_count" => 7, "terminalization_count" => 1, "state" => "terminal"}
      ],
      "delivery_outcome_cases" => [
        {"case_id" => "provider-outcome-unknown", "state" => "outcome-unknown", "resubmit_allowed" => false, "authoritative_proof_count" => 0, "terminalization_count" => 0},
        {"case_id" => "provider-outcome-reconciled", "state" => "confirmed", "resubmit_allowed" => false, "authoritative_proof_count" => 1, "terminalization_count" => 1}
      ],
      "scim_reconciliation_cases" => [
        {"case_id" => "matching-retry", "mapping_matches" => true, "applied_change_count" => 1, "cursor_advanced" => true, "retry_required" => false},
        {"case_id" => "mapping-mismatch", "mapping_matches" => false, "applied_change_count" => 0, "cursor_advanced" => false, "retry_required" => true}
      ],
      "otp_operation_cases" => [
        {"case_id" => "check-non-consuming", "operation_id" => "identity.otp.check", "valid" => true, "consumed_code_count" => 0},
        {"case_id" => "signin-consuming", "operation_id" => "identity.otp.signin", "valid" => true, "consumed_code_count" => 1}
      ],
      "bearer_transition_cases" => [
        {"case_id" => "authorize-source-session", "operation_id" => "identity.session.bearer-authorize", "sequence" => 1, "authorized" => true, "issued_count" => 0},
        {"case_id" => "issue-bound-continuation", "operation_id" => "identity.session.bearer-issue", "sequence" => 2, "authorized" => true, "issued_count" => 1}
      ],
      "api_key_security_cases" => [
        {"case_id" => "verify-no-browser-session", "session_count" => 0, "cookie_count" => 0, "secret_reveal_count" => 0, "outcome_unknown" => false, "resubmit_allowed" => false},
        {"case_id" => "create-reveal-once", "session_count" => 0, "cookie_count" => 0, "secret_reveal_count" => 1, "outcome_unknown" => false, "resubmit_allowed" => false},
        {"case_id" => "create-ambiguous-recovery", "session_count" => 0, "cookie_count" => 0, "secret_reveal_count" => 0, "outcome_unknown" => true, "resubmit_allowed" => false}
      ],
      "session_race_cases" => [
        {"case_id" => "refresh-rotates", "successor_count" => 1, "replay_rejected" => true, "cleanup_pending" => false, "outcome_unknown" => false, "resubmit_allowed" => false},
        {"case_id" => "refresh-unknown", "successor_count" => 0, "replay_rejected" => true, "cleanup_pending" => true, "outcome_unknown" => true, "resubmit_allowed" => false},
        {"case_id" => "cleanup-race", "successor_count" => 0, "replay_rejected" => true, "cleanup_pending" => false, "outcome_unknown" => false, "resubmit_allowed" => false}
      ],
      "trusted_risk_profile_cases" => [
        {"case_id" => "trusted-operation-profile", "profile_source" => "API_OPERATIONS.md", "limit_override_allowed" => false, "trusted_profile_loaded" => true},
        {"case_id" => "untrusted-caller-limit", "profile_source" => "caller-input", "limit_override_allowed" => false, "trusted_profile_loaded" => false}
      ],
      "sso_repeat_sync_cases" => [
        {"case_id" => "repeat-login-current-mapping", "mapping_version_current" => true, "duplicate_membership_count" => 0, "terminal" => true},
        {"case_id" => "directory-sync-retry", "mapping_version_current" => true, "duplicate_membership_count" => 0, "terminal" => false}
      ],
      "social_redirect_link_cases" => [
        {"case_id" => "redirect-link", "operation_id" => "identity.account.link-start", "state_bound" => true, "account_link_count" => 1, "session_count" => 0},
        {"case_id" => "redirect-unlink", "operation_id" => "identity.account.unlink", "state_bound" => true, "account_link_count" => 0, "session_count" => 0}
      ],
      "impersonation_authority_cases" => [
        {"case_id" => "explicit-denial", "operation_id" => "identity.impersonation.deny", "grant_count" => 0, "session_count" => 0, "state" => "denied"},
        {"case_id" => "quorum-reached", "operation_id" => "identity.impersonation.quorum", "grant_count" => 1, "session_count" => 0, "state" => "approved"},
        {"case_id" => "active-revocation", "operation_id" => "identity.impersonation.revoke", "grant_count" => 0, "session_count" => 0, "state" => "revoked"}
      ]
    }
    if artifact_cases.key?(kind)
      variants = artifact_cases.fetch(kind).map do |entry|
        properties = entry.to_h { |key, value| [key, {"const" => value}] }
        properties["transcript_sha256"] = field_schema("digest")
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "remember_issuer_variant_cases"
      cases = supplemental_operation_claims.fetch("remember-policy-propagation-report").product([false, true])
      variants = cases.map do |operation_id, remember|
        properties = {"case_id" => {"const" => "#{operation_id}:remember=#{remember}"}, "operation_id" => {"const" => operation_id},
                      "remember" => {"const" => remember}, "cookie_mode" => {"const" => remember ? "persistent" : "browser-session"},
                      "session_issued" => {"const" => true}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "captcha_provider_ids"
      return {"const" => %w[recaptcha turnstile hcaptcha captchafox]}
    end
    if kind == "captcha_provider_results"
      native = {
        "recaptcha" => ["https://www.google.com/recaptcha/api/siteverify", "remoteip", "hostname", true],
        "turnstile" => ["https://challenges.cloudflare.com/turnstile/v0/siteverify", "remoteip", "hostname", true],
        "hcaptcha" => ["https://api.hcaptcha.com/siteverify", "remoteip", "hostname", false],
        "captchafox" => ["https://api.captchafox.com/siteverify", "remoteIp", "hostname", false]
      }
      variants = native.map do |provider_id, (endpoint, remote_ip_field, hostname_field, action_supported)|
        {
          "type" => "object", "additionalProperties" => false,
          "required" => %w[provider_id endpoint request_content_type remote_ip_field remote_ip_disclosure_configured remote_ip_sent hostname_field action_supported evidence_mode verified execution_outcome request_digest response_digest evidence_digest result_digest transcript_sha256],
          "properties" => {
            "provider_id" => {"const" => provider_id},
            "endpoint" => {"const" => endpoint}, "request_content_type" => {"const" => "application/x-www-form-urlencoded"},
            "remote_ip_field" => {"const" => remote_ip_field}, "hostname_field" => {"const" => hostname_field},
            "remote_ip_disclosure_configured" => {"const" => false}, "remote_ip_sent" => {"const" => false},
            "action_supported" => {"const" => action_supported},
            "evidence_mode" => {"type" => "string", "enum" => %w[sandbox fixture]},
            "verified" => {"const" => true}, "execution_outcome" => {"const" => "passed"},
            "request_digest" => field_schema("digest"), "response_digest" => field_schema("digest"), "evidence_digest" => field_schema("digest"),
            "result_digest" => field_schema("digest"), "transcript_sha256" => field_schema("digest")
          }
        }
      end
      return {
        "type" => "array", "minItems" => 4, "maxItems" => 4, "uniqueItems" => true,
        "items" => {"oneOf" => variants},
        "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }
      }
    end
    if kind == "browser_operation_security_results"
      variants = canonical_operation_rows.select { |row| %w[both protocol middleware].include?(row.fetch("exposure")) }.map do |operation|
        properties = {"operation_id" => {"const" => operation.fetch("id")}, "http_method" => {"const" => operation.fetch("http_method")},
          "http_path" => {"const" => operation.fetch("http_path")}, "access" => {"const" => operation.fetch("access")},
          "csrf_origin_policy" => {"const" => operation.fetch("csrf_origin")}, "policy_applied" => {"const" => true},
          "positive_result" => {"const" => "passed"}, "negative_result" => {"const" => "rejected"},
          "cookie_attributes_verified" => {"const" => true}, "ambient_cookie_substitution_rejected" => {"const" => true},
          "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "redaction_class_surface_results"
      classes = %w[password api-key oauth-token private-key cookie dsn derived-fragment]
      surfaces = %w[error log trace diagnostic snapshot generated-artifact]
      variants = classes.product(surfaces).map do |secret_class, surface|
        properties = {"secret_class" => {"const" => secret_class}, "output_surface" => {"const" => surface},
          "full_match_count" => {"const" => 0}, "encoded_match_count" => {"const" => 0}, "partial_match_count" => {"const" => 0},
          "scanner_revision" => {"type" => "string", "minLength" => 1, "maxLength" => 128}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "risk_action_results"
      variants = %w[allow step_up throttle deny].map do |action|
        properties = {"action" => {"const" => action}, "decision_observed" => {"const" => true},
          "alias_accepted" => {"const" => false}, "session_revocation_requested" => {"const" => false},
          "response_policy" => {"const" => action == "step_up" ? "independent-factor" : action},
          "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "sso_break_glass_safety_results"
      cases = [
        ["lockout-unsafe-policy", "identity.sso.enforcement.update", "rejected", false, false],
        ["authorized-issue", "identity.sso.break-glass.issue", "issued", true, false],
        ["consume-once", "identity.sso.break-glass.consume", "recovery-session-created", true, true],
        ["wrong-organization", "identity.sso.break-glass.consume", "rejected", true, false],
        ["wrong-policy-version", "identity.sso.break-glass.consume", "rejected", true, false],
        ["expired", "identity.sso.break-glass.consume", "rejected", true, false],
        ["replay", "identity.sso.break-glass.consume", "rejected", true, false]
      ]
      variants = cases.map do |case_id, operation_id, outcome, audited, session_created|
        binding = {"grant_id" => "break-glass-grant", "organization_id" => "organization-a", "policy_version" => 1,
                   "actor_id" => "owner-a", "recipient_id" => "recovery-admin-a"}
        binding_digest = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(binding)))}"
        properties = {"case_id" => {"const" => case_id}, "operation_id" => {"const" => operation_id}, "observed_outcome" => {"const" => outcome},
          "organization_bound" => {"const" => true}, "policy_version_bound" => {"const" => true}, "audited" => {"const" => audited},
          "audit_event_type" => {"const" => operation_id.end_with?("issue") ? "identity.sso.issue_break_glass" : operation_id.end_with?("consume") ? "identity.sso.use_break_glass" : "identity.sso.enforcement_rejected"},
          "audit_event_id" => {"const" => "#{case_id}-audit-event"}, "grant_id" => {"const" => binding.fetch("grant_id")},
          "organization_id" => {"const" => binding.fetch("organization_id")}, "policy_version" => {"const" => binding.fetch("policy_version")},
          "actor_id" => {"const" => binding.fetch("actor_id")}, "recipient_id" => {"const" => binding.fetch("recipient_id")},
          "grant_binding_digest" => {"const" => binding_digest},
          "recovery_session_created" => {"const" => session_created}, "policy_weakened" => {"const" => false}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "operations_runbook_results"
      runbook_catalog = runbook_catalog_document
      variants = runbook_catalog.fetch("actions").map do |action|
        action_id = action.fetch("action_id")
        owner = action.fetch("owner")
        components = action.fetch("components")
        properties = {"action_id" => {"const" => action_id}, "owner" => {"const" => owner},
          "version" => {"const" => action.fetch("version")},
          "trigger_ref" => {"const" => components.fetch("trigger").fetch("locator")},
          "trigger_content_sha256" => {"const" => components.fetch("trigger").fetch("content_sha256")},
          "procedure_ref" => {"const" => components.fetch("procedure").fetch("locator")},
          "procedure_content_sha256" => {"const" => components.fetch("procedure").fetch("content_sha256")},
          "verification_ref" => {"const" => components.fetch("verification").fetch("locator")},
          "verification_content_sha256" => {"const" => components.fetch("verification").fetch("content_sha256")},
          "rollback_ref" => {"const" => components.fetch("rollback").fetch("locator")},
          "rollback_content_sha256" => {"const" => components.fetch("rollback").fetch("content_sha256")},
          "escalation_ref" => {"const" => components.fetch("escalation").fetch("locator")},
          "escalation_content_sha256" => {"const" => components.fetch("escalation").fetch("content_sha256")},
          "canonical_locator" => {"const" => ".ai/identity-platform/OPERATIONS_RUNBOOK_CATALOG.json"},
          "canonical_content_sha256" => {"const" => "sha256:#{Digest::SHA256.hexdigest(canonical_json(runbook_catalog))}"},
          "resolver" => {"const" => "coordinator-owned-runbook-resolver/v1"},
          "resolver_receipt_sha256" => field_schema("digest"),
          "independently_resolved" => {"const" => true}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "api_key_permission_reduction_results"
      cases = [
        ["narrow-committed", "narrow", true, false, "api_key.permissions_changed.v1", "committed", false, false],
        ["revoke-committed", "revoke", false, true, "api_key.revoked.v1", "committed", false, false],
        ["narrow-unknown-reconciled", "narrow", true, false, "api_key.permissions_changed.v1", "reconciled-committed", true, false],
        ["revoke-unknown-reconciled", "revoke", false, true, "api_key.revoked.v1", "reconciled-committed", true, false],
        ["unknown-fingerprint-mismatch", "narrow", false, false, "none", "conflict", true, true]
      ]
      variants = cases.map do |case_id, policy, narrowed, revoked, event_type, outcome, reconciled, mismatch|
        stored_input = {"tenant_id" => "tenant-a", "api_key_id" => "api-key-a", "policy" => policy,
                        "policy_version" => 1, "authority_version" => 1, "command_id" => "permission-reduction-command"}
        query_input = mismatch ? stored_input.merge("authority_version" => 2) : stored_input
        stored_input_sha256 = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(stored_input)))}"
        query_input_sha256 = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(query_input)))}"
        stored_fingerprint = "sha256:#{Digest::SHA256.hexdigest("api-key-permission-reduction-v1\n#{stored_input_sha256}")}"
        query_fingerprint = "sha256:#{Digest::SHA256.hexdigest("api-key-permission-reduction-v1\n#{query_input_sha256}")}"
        properties = {"case_id" => {"const" => case_id}, "policy" => {"const" => policy}, "policy_version" => field_schema("version"),
          "narrowed" => {"const" => narrowed}, "revoked" => {"const" => revoked}, "event_type" => {"const" => event_type},
          "lifecycle_event_count" => {"const" => mismatch ? 0 : 1}, "cascade_outcome" => {"const" => outcome},
          "same_command_id" => {"const" => true}, "stored_canonical_input_sha256" => {"const" => stored_input_sha256},
          "query_canonical_input_sha256" => {"const" => query_input_sha256}, "stored_fingerprint" => {"const" => stored_fingerprint},
          "query_fingerprint" => {"const" => query_fingerprint}, "fingerprint_match" => {"const" => !mismatch},
          "fingerprint_derivation" => {"const" => "sha256(api-key-permission-reduction-v1\\n+canonical-input-sha256)"},
          "terminal_reconciliation" => {"const" => reconciled}, "reapplication_count" => {"const" => 0},
          "authority_version" => field_schema("version"), "command_id" => {"const" => "permission-reduction-command"}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
              "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "command_id_integrity_results"
      cases = [
        ["cross-tenant-collision", "rejected", false], ["cross-purpose-collision", "rejected", false],
        ["matching-replay-after-key-rotation", "exact-recorded-result", true], ["retired-tombstone", "rejected", false],
        ["missing-scoped-ledger", "integrity-failure", false]
      ]
      variants = cases.map do |case_id, outcome, replayed|
        properties = {"case_id" => {"const" => case_id}, "command_id" => {"const" => "global-command-id"},
          "tenant_scope" => {"const" => case_id == "cross-tenant-collision" ? "different-tenant" : "same-tenant"},
          "purpose_scope" => {"const" => case_id == "cross-purpose-collision" ? "different-purpose" : "same-purpose"},
          "key_version_changed" => {"const" => case_id == "matching-replay-after-key-rotation"}, "replayed" => {"const" => replayed},
          "tombstone_present" => {"const" => case_id == "retired-tombstone"}, "scoped_ledger_present" => {"const" => case_id != "missing-scoped-ledger"},
          "observed_outcome" => {"const" => outcome}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return exact_variants(variants)
    end
    if kind == "audit_stage_results"
      cases = [["commit", "committed", 1, false], ["rollback", "rolled-back", 0, false], ["unknown", "unknown", 0, true]]
      variants = cases.map do |case_id, outcome, event_count, reconcile|
        properties = {"case_id" => {"const" => case_id}, "audit_plan_id" => {"const" => "audit-plan"}, "stage_audit_called" => {"const" => true},
          "event_count" => {"const" => event_count}, "outcome" => {"const" => outcome}, "reconciliation_required" => {"const" => reconcile},
          "duplicate_event_count" => {"const" => 0}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return exact_variants(variants)
    end
    if kind == "scim_bulk_skip_results"
      variants = [
        {"case_id" => "fail-on-errors-dependent", "status" => "skipped", "skip_reason" => "fail-on-errors", "side_effect_count" => 0},
        {"case_id" => "independent-before-threshold", "status" => "committed", "skip_reason" => "none", "side_effect_count" => 1}
      ].map do |entry|
        properties = entry.to_h { |key, value| [key, {"const" => value}] }.merge("transcript_sha256" => field_schema("digest"))
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return exact_variants(variants)
    end
    if kind == "api_key_possibly_debited_results"
      variants = [["initial-unknown", "possibly-debited", false], ["same-attempt-reconcile", "reconciled", true]].map do |case_id, outcome, resolved|
        attempt_input = {"operation_id" => "identity.apikey.verify", "verification_attempt_id" => "verification-attempt",
                         "tenant_id" => "tenant-a", "api_key_id" => "api-key-a"}
        attempt_digest = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_value(attempt_input)))}"
        properties = {"case_id" => {"const" => case_id}, "verification_attempt_id" => {"const" => "verification-attempt"},
          "operation_id" => {"const" => "identity.apikey.verify"}, "canonical_attempt_input_sha256" => {"const" => attempt_digest},
          "journal_attempt_sha256" => {"const" => attempt_digest}, "result_attempt_sha256" => {"const" => attempt_digest},
          "outcome" => {"const" => outcome}, "same_attempt_id" => {"const" => true}, "resolved" => {"const" => resolved},
          "retry_blocked" => {"const" => true},
          "duplicate_debit_count" => {"const" => 0}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return exact_variants(variants)
    end
    if kind == "session_authority_cache_results"
      variants = [["authoritative-load", false, "accepted"], ["stale-valkey-fill", true, "rejected-and-rebuilt"]].map do |case_id, stale, outcome|
        properties = {"case_id" => {"const" => case_id}, "authority_snapshot_fields" => {"const" => %w[tenant_version user_version credential_version session_family_version session_version authorization_version]},
          "valkey_projection_stale" => {"const" => stale}, "outcome" => {"const" => outcome}, "authoritative_snapshot_digest" => field_schema("digest"),
          "projection_snapshot_digest" => field_schema("digest"), "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return exact_variants(variants)
    end
    if kind == "privacy_lifecycle_results"
      cases = [["localized-export", "ready", true, false], ["destruction-pending", "erasure-pending", true, true],
        ["destruction-unknown", "erasure-outcome-unknown", true, true], ["legal-hold", "retained-encrypted-under-hold", false, false]]
      variants = cases.map do |case_id, state, localized, reconcile|
        properties = {"case_id" => {"const" => case_id}, "state" => {"const" => state}, "i18n_catalog_applied" => {"const" => localized},
          "destruction_evidence_bound" => {"const" => case_id != "localized-export"}, "reconciliation_required" => {"const" => reconcile},
          "hold_manifest_present" => {"const" => case_id == "legal-hold"}, "authority_restored" => {"const" => false}, "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return exact_variants(variants)
    end
    if kind == "enterprise_provider_cascade_results" || kind == "scim_connection_cascade_results"
      prefix = kind.start_with?("enterprise") ? "provider" : "connection"
      variants = %w[disable credential-rotate delete].map do |transition|
        properties = {"transition" => {"const" => transition}, "resource_kind" => {"const" => prefix},
          "cascade_started" => {"const" => true}, "authority_version_advanced" => {"const" => true},
          "old_authority_rejected" => {"const" => true}, "cleanup_outcome" => {"const" => "committed-or-reconcilable"},
          "transcript_sha256" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return exact_variants(variants)
    end
    return {"const" => captcha_protected_target_ids} if kind == "captcha_target_ids"
    if kind == "captcha_target_results"
      variants = captcha_protected_target_ids.map do |target_id|
        {
          "type" => "object", "additionalProperties" => false,
          "required" => %w[target_id middleware_attached flow_contexts evidence_digest],
          "properties" => {
            "target_id" => {"const" => target_id},
            "middleware_attached" => {"const" => true},
            "flow_contexts" => {"const" => captcha_target_flow_contexts.fetch(target_id)},
            "evidence_digest" => field_schema("digest")
          }
        }
      end
      return {
        "type" => "array", "minItems" => variants.length, "maxItems" => variants.length, "uniqueItems" => true,
        "items" => {"oneOf" => variants},
        "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }
      }
    end
    if kind == "hibp_outcome_cases"
      common_properties = {
        "range_endpoint" => field_schema("hibp_endpoint"), "transmitted_prefix" => field_schema("hibp_prefix"),
        "user_agent" => field_schema("hibp_user_agent"), "user_agent_secret_match_count" => {"const" => 0},
        "password_transmission_count" => {"const" => 0}, "full_hash_transmission_count" => {"const" => 0},
        "redirect_follow_count" => {"const" => 0}, "add_padding_header" => field_schema("hibp_add_padding"),
        "padding_row_count" => {"type" => "integer", "minimum" => 1, "maximum" => 2_147_483_647},
        "padded_response_valid" => {"const" => true}, "request_digest" => field_schema("digest"), "response_digest" => field_schema("digest")
      }
      variants = [
        ["known-compromised", {"type" => "integer", "minimum" => 1, "maximum" => 2_147_483_647}, true],
        ["absent-no-match", {"const" => 0}, false]
      ].map do |outcome_id, match_schema, compromised|
        properties = common_properties.merge("outcome_id" => {"const" => outcome_id}, "matched_suffix_count" => match_schema, "compromised" => {"const" => compromised})
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {
        "type" => "array", "minItems" => 2, "maxItems" => 2, "uniqueItems" => true,
        "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }
      }
    end
    if kind == "webauthn_backup_state_matrix"
      variants = [
        ["non-backup-valid", false, false, true, true],
        ["invalid-bs-without-be", false, true, false, false],
        ["backup-eligible-not-backed-up", true, false, true, true],
        ["backup-eligible-backed-up", true, true, true, true]
      ].map do |case_id, backup_eligible, backup_state, accepted, persisted|
        properties = {
          "case_id" => {"const" => case_id}, "backup_eligible" => {"const" => backup_eligible},
          "backup_state" => {"const" => backup_state}, "accepted" => {"const" => accepted},
          "backup_state_persisted" => {"const" => persisted}, "evidence_digest" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => 4, "maxItems" => 4, "uniqueItems" => true, "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    return {"const" => %w[identity.passkey.create_credential identity.passkey.mark_compromised]} if kind == "passkey_security_action_ids"
    if kind == "passkey_security_action_transitions"
      variants = [
        ["committed-creation", "identity.passkey.create_credential", "credential-created"],
        ["confirmed-compromise", "identity.passkey.mark_compromised", "credential-compromised"]
      ].map do |transition, action_id, resulting_state|
        properties = {
          "transition" => {"const" => transition}, "action_id" => {"const" => action_id},
          "resulting_state" => {"const" => resulting_state}, "emitted_count" => {"const" => 1},
          "evidence_digest" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => 2, "maxItems" => 2, "uniqueItems" => true, "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    return {"const" => %w[non-backup backup-eligible]} if kind == "webauthn_authenticator_path_ids"
    if kind == "webauthn_authenticator_path_results"
      variants = %w[non-backup backup-eligible].map do |path_id|
        properties = {"path_id" => {"const" => path_id}, "registration_executed" => {"const" => true}, "assertion_executed" => {"const" => true}, "evidence_digest" => field_schema("digest")}
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => 2, "maxItems" => 2, "uniqueItems" => true, "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "webauthn_counter_matrix"
      cases = [
        ["non-backup-zero-zero", "non-backup", 0, 0, "accept-unsupported-counter", 0, false, false],
        ["non-backup-zero-positive", "non-backup", 0, 1, "accept-and-advance", 1, false, false],
        ["non-backup-positive-increase", "non-backup", 1, 2, "accept-and-advance", 2, false, false],
        ["non-backup-positive-equal", "non-backup", 2, 2, "deny-suspected-clone-or-reset", 2, true, false],
        ["non-backup-positive-decrease", "non-backup", 2, 1, "deny-suspected-clone-or-reset", 2, true, false],
        ["non-backup-positive-received-zero", "non-backup", 2, 0, "deny-suspected-clone-or-reset", 2, true, false],
        ["backup-zero-zero", "backup-eligible", 0, 0, "risk-evidence", 0, false, true],
        ["backup-positive-increase", "backup-eligible", 1, 2, "accept-and-advance", 2, false, false],
        ["backup-positive-equal", "backup-eligible", 2, 2, "risk-evidence", 2, false, true],
        ["backup-positive-decrease", "backup-eligible", 2, 1, "risk-evidence", 2, false, true],
        ["backup-positive-received-zero", "backup-eligible", 2, 0, "risk-evidence", 2, false, true]
      ]
      variants = cases.map do |case_id, path_id, stored, received, disposition, persisted, denied, risk|
        properties = {
          "case_id" => {"const" => case_id}, "path_id" => {"const" => path_id},
          "stored_count" => {"const" => stored}, "received_count" => {"const" => received},
          "disposition" => {"const" => disposition}, "persisted_count" => {"const" => persisted},
          "denied" => {"const" => denied}, "risk_evidence_emitted" => {"const" => risk},
          "evidence_digest" => field_schema("digest")
        }
        {"type" => "object", "additionalProperties" => false, "required" => properties.keys, "properties" => properties}
      end
      return {"type" => "array", "minItems" => cases.length, "maxItems" => cases.length, "uniqueItems" => true, "items" => {"oneOf" => variants}, "allOf" => variants.map { |variant| {"contains" => variant, "minContains" => 1} }}
    end
    if kind == "oauth_oidc_operation_ids"
      return {"const" => %w[identity.oauth-server.authorize identity.oauth-server.continue identity.oauth-server.token identity.oauth-server.session-token identity.oauth-server.userinfo identity.oauth-server.introspect identity.oauth-server.revoke identity.oauth-server.dynamic-register identity.oauth-server.discovery-oauth identity.oauth-server.discovery-oidc identity.oauth-server.jwks identity.oauth-server.end-session identity.oauth-server.protected-resource-metadata]}
    end
    if kind == "oauth_oidc_interoperability_claims"
      return {"const" => %w[authorization-code-pkce authorization-continue session-token oidc-userinfo introspection-active introspection-inactive token-revocation dynamic-client-registration rfc7592-management-unsupported oauth-discovery oidc-discovery jwks rp-initiated-logout rfc9728-protected-resource-metadata]}
    end
    return {"const" => "openid-foundation-conformance-suite@3f2bc78770e9ebdbb8165b6be86ae85b99bb2fc8"} if kind == "oauth_oidc_suite_revision"
    if kind == "saml_endpoint_ids"
      return {"const" => %w[identity.sso.saml-metadata identity.sso.saml-start identity.sso.saml-acs identity.sso.saml-idp-init identity.sso.saml-logout-start identity.sso.saml-slo]}
    end
    return {"const" => "simplesamlphp@v2.5.3.1/e049be1819327c76403fc0d6fa648d6dcfbc8516"} if kind == "saml_interoperability_revision"
    return {"const" => "https://api.pwnedpasswords.com/range/{PREFIX}"} if kind == "hibp_endpoint"
    return {"type" => "string", "pattern" => "^[0-9A-F]{5}$"} if kind == "hibp_prefix"
    return {"type" => "string", "pattern" => "^golib-identity-reference/[A-Za-z0-9._-]+ \\([+]https://[^ ]+\\)$", "minLength" => 1, "maxLength" => 256} if kind == "hibp_user_agent"
    return {"const" => "true"} if kind == "hibp_add_padding"
    return {"type" => "boolean"} if kind == "boolean"
    return {"type" => "integer", "minimum" => 0, "maximum" => 2_147_483_647} if kind == "count"
    return {"type" => "integer", "minimum" => 0, "maximum" => 10_000} if kind == "basis_points"
    return {"type" => "integer", "minimum" => 1, "maximum" => 9_223_372_036_854_775_807} if kind == "version"
    return {"type" => "string", "pattern" => "^sha256:[0-9a-f]{64}$"} if kind == "digest"
    return {"type" => "string", "pattern" => "^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$"} if kind == "id"
    values = ENUMS.fetch(kind)
    type = values.first.is_a?(Integer) ? "integer" : "string"
    {"type" => type, "enum" => values}
  end

  def semantic_rules(artifact_id)
    fields = profiles.fetch(artifact_id).fetch("evidence_fields")
    positive = fields.filter_map { |name, kind| name if %w[count version].include?(kind) && !name.match?(ZERO_EVIDENCE_FIELDS) }
    zero = fields.filter_map { |name, _kind| name if name.match?(ZERO_EVIDENCE_FIELDS) }
    truthy = fields.filter_map { |name, kind| name if kind == "boolean" && !%w[remember provider_catalog_ready].include?(name) }
    rules = []
    rules << {"kind" => "positive", "fields" => positive} unless positive.empty?
    rules << {"kind" => "zero", "fields" => zero} unless zero.empty?
    rules << {"kind" => "true", "fields" => truthy} unless truthy.empty?
    CONST_RULES.fetch(artifact_id, {}).each { |field, value| rules << {"kind" => "const", "field" => field, "value" => value} }
    if artifact_id == "operation-handler-openapi-bijection"
      {"operation_count" => api_operation_ids.length, "handler_count" => handler_operation_ids.length,
       "openapi_operation_count" => openapi_operation_ids.length, "duplicate_count" => 0}.each do |field, value|
        rules << {"kind" => "const", "field" => field, "value" => value}
      end
    end
    if artifact_id == "oauth-provider-matrix-report"
      verified_count = provider_matrix_rows.count do |provider|
        !provider.dig("evidence", "official_docs").to_s.include?("required") &&
          provider.dig("evidence", "interoperability") != "not-run" && Array(provider.dig("oidc_logout", "evidence_blocker")).empty?
      end
      rules << {"kind" => "const", "field" => "provider_count", "value" => provider_matrix_rows.length}
      rules << {"kind" => "const", "field" => "successful_provider_count", "value" => verified_count}
      rules << {"kind" => "const", "field" => "unresolved_provider_count", "value" => provider_matrix_rows.length - verified_count}
      rules << {"kind" => "const", "field" => "provider_catalog_ready", "value" => verified_count == provider_matrix_rows.length}
    elsif artifact_id == "provider-evidence-index"
      rules << {"kind" => "const", "field" => "provider_count", "value" => provider_matrix_rows.length}
      rules << {"kind" => "const", "field" => "indexed_provider_count", "value" => provider_matrix_rows.length}
    end
    if artifact_id == "hibp-interoperability-report"
      rules << {"kind" => "const", "field" => "local_password_input_count", "value" => 1}
      rules << {"kind" => "const", "field" => "local_sha1_computation_count", "value" => 1}
    end
    if artifact_id == "operations-runbook-index"
      runbook_catalog = runbook_catalog_document
      rules << {"kind" => "const", "field" => "runbook_count", "value" => runbook_catalog.fetch("actions").length}
      rules << {"kind" => "const", "field" => "covered_action_count", "value" => runbook_catalog.fetch("actions").length}
      rules << {"kind" => "const", "field" => "index_digest", "value" => "sha256:#{Digest::SHA256.hexdigest(canonical_json(runbook_catalog))}"}
      owner_rows = runbook_catalog.fetch("actions").map { |row| [row.fetch("action_id"), row.fetch("owner"), row.fetch("version")] }
      rules << {"kind" => "const", "field" => "owner_digest", "value" => "sha256:#{Digest::SHA256.hexdigest(canonical_json(owner_rows))}"}
    elsif artifact_id == "secret-redaction-report"
      rules << {"kind" => "const", "field" => "secret_class_count", "value" => 7}
      rules << {"kind" => "const", "field" => "output_surface_count", "value" => 6}
    end
    rules << {"kind" => "const", "field" => "protected_target_count", "value" => captcha_protected_target_ids.length} if artifact_id == "captcha-four-provider-report"
    EQUALITY_RULES.fetch(artifact_id, []).each { |left, right| rules << {"kind" => "equal", "left" => left, "right" => right} }
    rules
  end

  def execution_receipt_path(artifact)
    ".ai/identity-platform/evidence/executions/#{artifact.fetch('artifact_id')}.json"
  end

  def execution_schema(artifact)
    producer_argv = Shellwords.split(artifact.dig("producer", "operation"))
    verifier_argv = ["ruby", ".ai/identity-platform/acceptance/schema_validation.rb", "verify", artifact.fetch("artifact_id")]
    version_argv = [producer_argv.first, "--version"]
    verifier_version_argv = [verifier_argv.first, "--version"]
    environment_probe_argv = ["uname", "-a"]
    argv_schema = lambda { |value| {"type" => "array", "minItems" => value.length, "maxItems" => value.length, "items" => {"type" => "string", "minLength" => 1}, "const" => value} }
    schema = {
      "type" => "object", "additionalProperties" => false,
      "required" => %w[execution_identity execution_receipt_path execution_receipt_sha256 capture_authority producer_argv verifier_argv tested_revision input_root sandbox_environment_sha256 canonical_workdir executable_realpath executable_sha256 verifier_executable_realpath verifier_executable_sha256 verifier_driver_realpath verifier_driver_sha256 version_argv version_stdout version_stderr verifier_version_argv verifier_version_stdout verifier_version_stderr environment_probe_argv environment_probe_stdout environment_probe_stderr verifier_exit_status verifier_stdout verifier_stderr verifier_attestation_sha256 started_at completed_at exit_status stdout stdout_byte_length stdout_sha256 stderr stderr_byte_length stderr_sha256 captured_output_sha256 raw_capture_sha256 output_artifact_path artifact_payload_binding],
      "properties" => {
        "execution_identity" => field_schema("digest"),
        "execution_receipt_path" => {"const" => execution_receipt_path(artifact)},
        "execution_receipt_sha256" => field_schema("digest"),
        "capture_authority" => {"const" => "coordinator-owned-execution-runner/v3"},
        "producer_argv" => argv_schema.call(producer_argv),
        "verifier_argv" => argv_schema.call(verifier_argv),
        "tested_revision" => {"type" => "string", "pattern" => "^[0-9a-f]{40}$"},
        "input_root" => field_schema("digest"),
        "sandbox_environment_sha256" => field_schema("digest"),
        "canonical_workdir" => {"type" => "string", "pattern" => "^/", "minLength" => 2, "maxLength" => 4096},
        "executable_realpath" => {"type" => "string", "pattern" => "^/", "minLength" => 2, "maxLength" => 4096},
        "executable_sha256" => field_schema("digest"),
        "verifier_executable_realpath" => {"type" => "string", "pattern" => "^/", "minLength" => 2, "maxLength" => 4096},
        "verifier_executable_sha256" => field_schema("digest"),
        "verifier_driver_realpath" => {"type" => "string", "pattern" => "^/", "minLength" => 2, "maxLength" => 4096},
        "verifier_driver_sha256" => field_schema("digest"),
        "version_argv" => argv_schema.call(version_argv),
        "version_stdout" => {"type" => "string", "minLength" => 1, "maxLength" => 65_536},
        "version_stderr" => {"type" => "string", "maxLength" => 65_536},
        "verifier_version_argv" => argv_schema.call(verifier_version_argv),
        "verifier_version_stdout" => {"type" => "string", "minLength" => 1, "maxLength" => 65_536},
        "verifier_version_stderr" => {"type" => "string", "maxLength" => 65_536},
        "environment_probe_argv" => argv_schema.call(environment_probe_argv),
        "environment_probe_stdout" => {"type" => "string", "minLength" => 1, "maxLength" => 65_536},
        "environment_probe_stderr" => {"type" => "string", "maxLength" => 65_536},
        "verifier_exit_status" => {"const" => 0},
        "verifier_stdout" => {"type" => "string", "maxLength" => 1_048_576},
        "verifier_stderr" => {"type" => "string", "maxLength" => 1_048_576},
        "verifier_attestation_sha256" => field_schema("digest"),
        "started_at" => {"type" => "string", "format" => "date-time"},
        "completed_at" => {"type" => "string", "format" => "date-time"},
        "exit_status" => {"const" => 0},
        "stdout" => {"type" => "string", "minLength" => 1, "maxLength" => 1_048_576},
        "stdout_byte_length" => {"type" => "integer", "minimum" => 1, "maximum" => 1_048_576},
        "stdout_sha256" => field_schema("digest"),
        "stderr" => {"type" => "string", "maxLength" => 1_048_576},
        "stderr_byte_length" => {"type" => "integer", "minimum" => 0, "maximum" => 1_048_576},
        "stderr_sha256" => field_schema("digest"),
        "captured_output_sha256" => field_schema("digest"),
        "raw_capture_sha256" => field_schema("digest"),
        "output_artifact_path" => {"const" => artifact.fetch("artifact_evidence_output_path")},
        "artifact_payload_binding" => {"const" => "record.artifact_hashes[path,sha256]"}
      },
      "x-execution-proof" => 2
    }
    if artifact.fetch("artifact_id") == "coverage-mutation-report"
      package_argv = ["ruby", ".ai/identity-platform/acceptance/schema_validation.rb", "discover-packages", artifact.fetch("artifact_id")]
      mutation_tool_argv = ["ruby", ".ai/identity-platform/acceptance/schema_validation.rb", "mutation-tool", "pkg/identity/reference"]
      schema.fetch("required").concat(%w[package_discovery_argv mutation_tool_argv mutation_tool_output_sha256 package_manifest_sha256 mutant_manifest_sha256])
      schema.fetch("properties").merge!(
        "package_discovery_argv" => argv_schema.call(package_argv),
        "mutation_tool_argv" => argv_schema.call(mutation_tool_argv),
        "mutation_tool_output_sha256" => field_schema("digest"),
        "package_manifest_sha256" => field_schema("digest"),
        "mutant_manifest_sha256" => field_schema("digest")
      )
    end
    schema
  end

  def artifact_evidence_schema(artifact)
    id = artifact.fetch("artifact_id")
    profile = profiles.fetch(id)
    source_row = source.fetch("artifact_catalog").find { |row| row.fetch("id") == id }
    observation_by_claim = artifact.fetch("required_observations").to_h { |row| [row.fetch("claim_id"), row.fetch("observation_id")] }
    cases = claim_ids(source_row).flat_map do |claim_id|
      scenario_names_for_claim(source_row, claim_id).map { |scenario| [claim_id, scenario] }
    end
    observed_value_properties = profile.fetch("evidence_fields").filter_map do |name, kind|
      value_schema = field_schema(kind)
      next if value_schema.fetch("type", nil) == "array" || value_schema["const"].is_a?(Array)

      [name, value_schema]
    end.to_h
    case_specs = cases.map do |claim_id, scenario|
      status, error = scenario_outcome(scenario)
      observation_id = observation_by_claim.fetch(claim_id)
      operation_ids = operation_ids_for_claim(source_row, claim_id)
      {
        "case_id" => "#{observation_id}##{scenario}", "observation_id" => observation_id, "claim_id" => claim_id,
        "scenario" => scenario, "operation_ids" => operation_ids, "input_fields" => input_fields_for_claim(source_row, claim_id),
        "status" => status, "stable_error" => error,
        "artifact_contract_sha256" => "sha256:#{Digest::SHA256.hexdigest([id, claim_id, scenario, profile.fetch('invariant')].join("\0"))}",
        "expected_operation_set" => operation_ids.sort_by(&:b), "observed_operation_set" => operation_ids.sort_by(&:b)
      }
    end
    exact_case_fields = case_specs.first.keys
    artifact_operation_ids = profile.fetch("operation_ids")
    scenario_properties = {
      "case_id" => {"type" => "string", "enum" => case_specs.map { |spec| spec.fetch("case_id") }},
      "observation_id" => {"type" => "string", "enum" => observation_by_claim.values},
      "claim_id" => {"type" => "string", "enum" => claim_ids(source_row)},
      "invariant_id" => {"const" => "#{id}.invariant"},
      "scenario" => {"type" => "string", "enum" => cases.map(&:last).uniq},
      "operation_ids" => {"type" => "array", "minItems" => 1, "uniqueItems" => true, "items" => {"type" => "string", "enum" => artifact_operation_ids}},
      "input_fields" => {"type" => "array", "minItems" => 1, "uniqueItems" => true, "items" => {"type" => "string", "minLength" => 1}},
      "initial_state" => {"const" => profile.fetch("initial_state")},
      "status" => {"type" => "string", "enum" => cases.map { |_claim_id, scenario| scenario_outcome(scenario).first }.uniq},
      "stable_error" => {"type" => "string", "enum" => cases.map { |_claim_id, scenario| scenario_outcome(scenario).last }.uniq},
      "expected_invariant" => {"const" => profile.fetch("invariant")},
      "observed_state_digest" => field_schema("digest"), "observed_event_digest" => field_schema("digest"),
      "transcript_sha256" => field_schema("digest"), "artifact_contract_sha256" => field_schema("digest"),
      "observed_value_count" => {"const" => observed_value_properties.length},
      "ordered_steps" => {"const" => %w[precondition-established stimulus-executed outcome-observed transcript-bound]},
      "expected_operation_set" => {"type" => "array", "minItems" => 1, "uniqueItems" => true, "items" => {"type" => "string", "enum" => artifact_operation_ids}},
      "observed_operation_set" => {"type" => "array", "minItems" => 1, "uniqueItems" => true, "items" => {"type" => "string", "enum" => artifact_operation_ids}},
      "observed_values" => {"type" => "object", "additionalProperties" => false,
                            "required" => observed_value_properties.keys, "properties" => observed_value_properties}
    }
    scenario_case_schema = {"type" => "object", "additionalProperties" => false,
                            "required" => scenario_properties.keys, "properties" => scenario_properties}
    properties = {
      "artifact_id" => {"const" => id},
      "invariant_id" => {"const" => "#{id}.invariant"},
      "invariant" => {"const" => profile.fetch("invariant")},
      "operation_ids" => {"const" => profile.fetch("operation_ids")},
      "input_digest" => field_schema("digest"),
      "execution" => execution_schema(artifact),
      "scenario_cases" => {"type" => "array", "minItems" => cases.length, "maxItems" => cases.length, "uniqueItems" => true,
                           "items" => scenario_case_schema, "x-exact-scenario-case-fields" => exact_case_fields,
                           "x-exact-scenario-cases" => case_specs}
    }
    profile.fetch("evidence_fields").each { |name, kind| properties[name] = field_schema(kind) }
    {
      "type" => "object", "additionalProperties" => false,
      "required" => properties.keys,
      "properties" => properties,
      "x-semantic-rules" => semantic_rules(id)
    }
  end

  def schema_document(artifact)
    observations = artifact.fetch("required_observations")
    observation_variants = observations.map do |observation|
      {
        "type" => "object",
        "additionalProperties" => false,
        "required" => %w[observation_id claim_id contract_reference scenario preconditions stimulus expected_outcome actual_outcome result artifact_sha256],
        "properties" => {
          "observation_id" => {"const" => observation.fetch("observation_id")},
          "claim_id" => {"const" => observation.fetch("claim_id")},
          "contract_reference" => {"const" => observation.fetch("contract_reference")},
          "scenario" => {"const" => observation.fetch("scenario")},
          "preconditions" => {"const" => observation.fetch("preconditions")},
          "stimulus" => {"const" => observation.fetch("stimulus")},
          "expected_outcome" => {"const" => observation.fetch("expected_outcome")},
          "actual_outcome" => {"const" => observation.fetch("actual_outcome")},
          "result" => {"const" => "pass"},
          "artifact_sha256" => {"type" => "string", "pattern" => "^sha256:[0-9a-f]{64}$"}
        }
      }
    end
    {
      "$schema" => "https://json-schema.org/draft/2020-12/schema",
      "$id" => artifact.fetch("schema_id"),
      "title" => "#{artifact.fetch("artifact_id")} acceptance evidence",
      "type" => "object",
      "additionalProperties" => false,
      "required" => %w[schema_version artifact_id result tested_revision gate_execution_revision revalidation_revision input_manifest input_root observations artifact_hashes recorded_at],
      "properties" => {
        "schema_version" => {"const" => 2},
        "artifact_id" => {"const" => artifact.fetch("artifact_id")},
        "result" => {"const" => {"status" => artifact_result_status(artifact.fetch("artifact_id")),
          "gate" => artifact.dig("producer", "operation")}},
        "tested_revision" => {"type" => "string", "pattern" => "^[0-9a-f]{40}$"},
        "gate_execution_revision" => {"type" => "string", "pattern" => "^[0-9a-f]{40}$"},
        "revalidation_revision" => {"type" => ["string", "null"], "pattern" => "^[0-9a-f]{40}$"},
        "input_manifest" => {"type" => "array", "minItems" => 1, "uniqueItems" => true, "items" => {
          "type" => "object", "additionalProperties" => false,
          "required" => %w[path_or_environment_id kind content_identity owner reason],
          "properties" => %w[path_or_environment_id kind content_identity owner reason].to_h { |field| [field, {"type" => "string", "minLength" => 1}] }
        }},
        "input_root" => {"type" => "string", "pattern" => "^sha256:[0-9a-f]{64}$"},
        "observations" => {"type" => "array", "minItems" => observations.length, "maxItems" => observations.length, "uniqueItems" => true, "items" => {"oneOf" => observation_variants}},
        "artifact_hashes" => {"type" => "array", "minItems" => 2, "uniqueItems" => true, "items" => {
          "type" => "object", "additionalProperties" => false, "required" => %w[path sha256],
          "properties" => {"path" => {"type" => "string", "pattern" => "^\\.ai/[A-Za-z0-9._/-]+$"}, "sha256" => {"type" => "string", "pattern" => "^sha256:[0-9a-f]{64}$"}}
        }, "contains" => {"type" => "object", "required" => ["path"], "properties" => {"path" => {"const" => artifact.fetch("artifact_evidence_output_path")}}}, "minContains" => 1},
        "recorded_at" => {"type" => "string", "format" => "date-time"}
      },
      "allOf" => observations.map { |observation| {"properties" => {"observations" => {"contains" => {"type" => "object", "required" => ["observation_id"], "properties" => {"observation_id" => {"const" => observation.fetch("observation_id")}}}, "minContains" => 1}}} } + [
        {"properties" => {"artifact_hashes" => {"contains" => {"type" => "object", "required" => ["path"], "properties" => {"path" => {"const" => execution_receipt_path(artifact)}}}, "minContains" => 1}}}
      ],
      "x-cross-field-rules" => [
        "every observations[].artifact_sha256 value must equal one artifact_hashes[].sha256 value",
        "artifact_hashes must bind the artifact-specific evidence object at #{artifact.fetch('artifact_evidence_output_path')}",
        "artifact_hashes must bind the separately captured coordinator execution receipt at #{execution_receipt_path(artifact)}",
        "revalidation_revision is null exactly when tested_revision equals gate_execution_revision; otherwise it equals gate_execution_revision",
        "input_root is SHA-256 over RFC 8785 canonical JSON of input_manifest"
      ],
      "$defs" => {"artifact_evidence" => artifact_evidence_schema(artifact)}
    }
  end

  def canonical_json(value)
    JSON.pretty_generate(value) + "\n"
  end

  def canonical_value(value)
    case value
    when Hash then value.keys.sort.to_h { |key| [key, canonical_value(value.fetch(key))] }
    when Array then value.map { |entry| canonical_value(entry) }
    else value
    end
  end
end
