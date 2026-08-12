# Identity Platform Preflight Evidence

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

This coordinator-owned file is the durable preflight and resource registry for
one identity-platform execution. The coordinator MUST replace every `pending`
value, add rows as requirements are discovered, and commit the completed
record on the integration branch before the first worker assignment. It MUST
NOT contain credentials, tokens, provider payloads, secret-shaped values, or
unsanitized logs.

## Execution identity

| Field | Value |
| --- | --- |
| Recorded committed `main` base | pending |
| Identity-platform base tree object | pending |
| Identity-platform base tree digest | pending |
| Integration branch | `feature/identity-platform` |
| Integration worktree | pending |
| Task-owned worktree parent | pending |
| Preflight input revision before the record commit | pending |
| Preflight recorded at (RFC3339) | pending |

The committed base and preflight input MUST be existing full commit hashes.
The recorded identity-platform tree object and SHA-256 digest MUST be derived
from the exact committed base. The integration branch MUST be registered, its
registered worktree MUST have the same branch and HEAD, and that HEAD MUST
descend from the exact base and contain the preflight input. The integration worktree and
task-owned parent MUST be absolute, distinct from the repository root and the
home directory itself, registered by Git, and resolved before any destructive
cleanup.
Every post-commit execution check MUST use
`validate.rb --execution --clean-integration` plus the required previous
fixture quartet after the initial snapshot; a dirty, unreadable, detached,
or branch-HEAD-divergent integration worktree blocks spawn, merge, gates,
restart recovery, and final acceptance. Pre-commit proposed snapshots use
explicit fixture paths without clean mode.

## Platform evidence trust

| Field | Value |
| --- | --- |
| Trust document path | `.ai/identity-platform/PLATFORM_EVIDENCE_TRUST.json` |
| Trust document commit | pending |
| Trust document digest | pending |
| Platform thread ID | pending |
| Classification | pending |
| Blocker or attestation | pending |

The capture policy MUST already exist on the recorded committed `main` base;
the coordinator MUST NOT create or replace it on the integration branch.
`Classification` is `available` only when the validator verifies the committed
policy and its required Git-custody controls. It means the bounded coordinator
capture lane is usable; it MUST NOT mean platform authenticity, runtime
realization, or delivery was independently attested.

Repository rows, coordinator captures, worker text, deterministic IDs, and
hashes are custody evidence only. They MUST NOT be represented as platform
signatures, immutable runtime attestations, or proof of delivery.

`PLATFORM_EVIDENCE_TRUST.json` retains its historical filename but is a capture
policy, not a cryptographic trust store. It is canonical compact JSON with
ordered fields `schema_version`, `schema`, `capture_source`, `limitations`, and
`required_controls`. Its schema is
`identity-platform.evidence-capture-policy.v1`; it records that the
collaboration API exposes tool-visible identities, statuses, and messages but
does not export signatures or immutable delivery receipts. The file pins the
required append-only first-commit custody, exact-byte digests, isolated worker
branch/worktree ownership, coordinator re-verification, independent package
worker, and explicit user semantic-authority controls.

Every tool-visible capture binding is
`<path>@<first-commit-containing-exact-bytes>@sha256:<canonical-envelope-digest>`.
The path is beneath `.ai/identity-platform/evidence/platform-events/`. An
envelope is canonical compact JSON with ordered fields `schema_version`,
`schema`, `capture_source`, `thread_id`, `event_id`, `event_type`, `role`,
`actor_id`, `task_id`, `agent_id`, `turn_ordinal`, `recorded_at`, `content`,
`content_length`, `content_sha256`, and `claims`.
`schema` is `identity-platform.tool-visible-capture.v1`; content is exact UTF-8 text;
its length counts bytes. Claims are a canonical object containing every
capture-specific identity named below. The event ID is globally unique within
the execution. The coordinator transcribes only tool-visible output and commits
the exact bytes immediately. The artifact proves committed custody and
ordering, not platform authenticity, and MUST NOT be called signed evidence.

Coordinator execution captures use event type
`coordinator-gate-capture` for the producer and
`coordinator-verifier-capture` for the independent verifier. Their `content`
is the exact canonical receipt or verifier-attestation bytes, excluding the
companion event binding itself to avoid a digest fixed point. Claims are an
ordered canonical object containing `execution_receipt_path`,
`execution_receipt_sha256`, `execution_identity`, `tested_revision`,
`input_root`, `capture_authority`, `producer_argv`, `verifier_argv`,
`canonical_workdir`, both producer and verifier executable realpaths and
SHA-256 digests, the version and environment-probe argv/stdout/stderr, producer
and verifier stdout/stderr/exit fields, byte lengths and digests,
`captured_output_sha256`, `raw_capture_sha256`, `output_artifact_path`,
`artifact_payload_binding`, `verifier_attestation_sha256`, `started_at`,
`completed_at`, and `scope`.
Coverage/mutation receipts additionally bind `package_discovery_argv`,
`mutant_discovery_argv`, `package_manifest_sha256`, and
`mutant_manifest_sha256`. `scope` contains ordered keys `kind`, `unit`,
`artifact_id`, `profile_id`, and `claim_ids`; inapplicable nullable values are
JSON `null`, and `claim_ids` is bytewise sorted and unique. `kind` is exactly
`local`, `external`, `acceptance`, or `final-gate`. Producer and verifier
captures MUST use distinct capture IDs and bind the distinct coordinator-owned
process results. They are not platform signatures.

Because receipt schemas are closed and self-embedding a capture creates
a fixed point, bindings live in the companion table below. The table is keyed
by the immutable execution identity and exact receipt path/digest. A row is
valid only when both committed captures reproduce every receipt execution field,
the producer and verifier executable/argv/workdir/environment/input-root,
stdout/stderr/exit/timestamps, the independent verifier-attestation digest,
and the complete scope. A missing, replayed, or mismatched capture
leaves that gate unverified even when its receipt JSON says `passed`.
The receipt payload intentionally omits its own `execution_identity`,
`execution_receipt_path`, `execution_receipt_sha256`, and the fixed
`artifact_payload_binding`; the validator derives the first three claims from
the companion row and exact receipt binding and requires the last to equal
`record.artifact_hashes[path,sha256]` before verifying the capture. All
other claims come from the receipt bytes.

## Platform gate capture bindings

| Execution identity | Scope | Receipt binding | Producer capture event binding | Verifier capture event binding | Recorded at |
| --- | --- | --- | --- | --- | --- |

`Scope` is canonical compact JSON with ordered keys `kind`, `unit`,
`artifact_id`, `profile_id`, and `claim_ids`. The row is append-only; its two
event bindings and receipt binding are immutable for the execution identity.

## Final gate bindings

| Gate ID | Resolved argv | Evidence record binding | Runner receipt binding | Producer capture event binding | Verifier capture event binding | Final input revision | Input root | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |

This append-only table is the sole custody index for the seven program-final
gates. `Gate ID` MUST exactly match the final-gate catalog. `Resolved argv` is
canonical compact JSON containing the exact absolute executable and argument
array; placeholders, shell strings, and unresolved base revisions are
forbidden. The executable MUST exist and be executable, and its exact realpath
MUST equal the current coordinator-owned `which make`, `which codex`, or
`RbConfig.ruby` identity, as applicable; a task-owned same-basename executable
is forbidden. Its complete argument array MUST equal this closed mapping:
`affected-release-gates` is `make ci-changed BASE=<recorded-base>`;
`complete-repository-gate` is `make ci`; `final-complete-diff-review` is
`ruby <absolute-final_review_gate.rb> <recorded-base>`;
`inventory-validation` is `make inventory`;
`pinned-upstream-validation` is `ruby
<absolute-generate_upstream_leaves.rb> --check
<absolute-BETTER_AUTH_REPOSITORY-realpath>`;
`program-final-input-acceptance` is `ruby
<absolute-identity-platform-validate.rb> --execution --clean-integration`;
and `structural-validation` is `ruby
<absolute-identity-platform-validate.rb>`.
The repository-owned review gate invokes the installed `codex review` with the
exact recorded base and validator-owned prompt, then writes its captured output
to the runner-owned raw-output path. The review prompt requires canonical
compact JSON with ordered keys `critical` and `important`. Its independent
verifier MUST be the repository-owned `final_review_verifier.rb`; it accepts
only exact `{"critical":[],"important":[]}` bytes and emits a digest-bound
zero-count attestation. Process exit alone never proves a clean review. Extra flags,
wrappers, shell launchers, aliases, and a command assigned to another gate are
forbidden. The affected-release command MUST contain the exact recorded
committed program base from `Execution identity`. Evidence records live at
`.ai/identity-platform/evidence/final-gates/<gate-id>.json`; runner-v3 receipts
live at `.ai/identity-platform/evidence/final-gates/executions/<execution-identity>.json`.
Both bindings use `<path>@<first-commit-containing-exact-bytes>@sha256:<digest>`.
Each final-gate evidence record is canonical compact JSON with ordered fields
`schema_version`, `schema`, `gate_id`, `outcome`, `resolved_argv`,
`tested_revision`, `gate_execution_revision`, `input_root`,
`execution_identity`, `receipt_path`, `receipt_sha256`, and `record_digest`.
`schema_version` is `1`, `schema` is `identity-platform.final-gate.v1`, and
`outcome` is `passed`. `record_digest` is the SHA-256 of canonical compact JSON
over the preceding fields with `record_digest` omitted. The record's gate ID,
argv, revisions, input root, execution identity, and receipt path/digest MUST
equal this row and the committed capture exactly.
The producer and verifier event bindings MUST equal the matching verified
`Platform gate capture bindings` row whose scope kind is `final-gate`, unit and
profile are JSON `null`, `artifact_id` is the gate ID, and `claim_ids` is the
one-element array containing that gate ID. `Final input revision` and `Input
root` MUST equal the canonical report and receipt; all seven rows MUST be current
before `PROGRAM-COMPLETE`.

`BETTER_AUTH_REPOSITORY` MUST name an existing absolute local Git repository
whose `origin` is exactly `https://github.com/better-auth/better-auth.git` and
which contains the pinned commit, SHA-1 object format, and every source object
declared by `UPSTREAM_SURFACE.json`. The resolved realpath MUST be the final
argument captured by the upstream gate. A missing checkout, missing pinned
object, different remote, different object format, or source-object mismatch
fails the gate; checked-in `UPSTREAM_SURFACE.json` and `UPSTREAM_LEAVES.json`
alone are not evidence. The repository may be a normal clone or local mirror
and the gate performs no network access. It MUST be supplied before
orchestration as a persistent user-owned prerequisite, MUST remain available
through terminal validation, and MUST NOT be entered in the task-owned resource
registry or deleted by the coordinator.

## Blocked audit binding

| Persistent goal ID | Platform event binding | Threshold state | Execution identity | Recorded at |
| --- | --- | --- | --- | --- |

This append-only row is required only for `PROGRAM-BLOCKED`. Its coordinator
capture type is `persistent-goal-blocked-audit`. Its exact claims bind the current thread, persistent goal ID,
threshold state, consecutive blocked-audit count, execution identity, exact
current `final_input_revision` and `input_root`, remaining blocker IDs,
reconciled assignment IDs and state root, reconciled resource IDs and state
root, and exhausted independent frontier. `Threshold state` is canonical compact JSON with ordered fields
`required_count`, `observed_count`, `history_event_ids`,
`final_input_revision`, `input_root`, `blockers`,
`reconciled_assignment_ids`, `reconciled_resource_ids`,
`reconciled_assignments_root`, `reconciled_resources_root`, and
`exhausted_frontier`. `blockers` is sorted by the canonical compact JSON bytes
of each closed object and every object has exactly the ordered fields `id`,
`category`, `unit`, `evidence`, and `required_user_action`. Its `id` is the
safe blocker ID without the inventory `blocker:` prefix; `category` is one of
`credential`, `external-infrastructure`, `product-decision`, or
`user-authority`; `unit` is the exact unfinished unit; and the evidence and
required action are nonempty newline-free strings. The committed blocker objects,
not later final-report prose, are the authority for every
reported blocker detail. Their `(id, unit)` pairs MUST equal the complete
unfinished inventory blocker set exactly. The execution identity is SHA-256 over
`<final_input_revision>\0<input_root>`.
`reconciled_assignments_root` is SHA-256 over canonical JSON rows containing
every assigned ledger row's unit, generation, inventory status and owner, task,
assignment, worker and integration commits, gate revision and fingerprint,
external disposition, and transition. `reconciled_resources_root` is SHA-256
over canonical JSON rows containing every resource's ID, type, owner, state,
target, and cleanup evidence. Rows are sorted by unit or resource ID and object
keys are byte-sorted before hashing. Both roots MUST equal independently
recomputed current projections; matching IDs alone are insufficient. The final
input revision MUST equal the
last behavior-affecting input revision. It MUST be an ancestor of the committed
terminal audit capture and current clean `HEAD`, and its recomputed input root
MUST equal the captured root. Only the validated append-only ledger, inventory,
preflight, and evidence/report bookkeeping paths permitted by the final-input
contract MAY change after that revision. The state is the validator's
projection of committed repository facts, not platform-authenticated state.
Missing or unverifiable committed capture evidence means the blocked threshold
is unproved and stopping is forbidden.
`PROGRAM-BLOCKED` is terminal only from a successfully inspected, clean
repository state after every permitted bookkeeping/evidence change has been
committed and every task-owned resource and assignment has the exact reconciled
disposition reported here. Uncommitted semantic or orchestration state makes
the blocked predicate false; it is work to reconcile, not a durable blocker.

Final-report claims derived from the validator's committed cleanup,
parity-disposition, or aggregate predicate inputs use the exact binding
`program:<claim-id>@<final-input-revision>@<input-root>@<terminal-history-root>`.
The validator independently recomputes the behavior input root from tracked
inputs plus the five attested environment identities. It separately computes
the terminal-history root from the committed execution ledger, inventory,
preflight history, and evidence tree, excluding only the final report itself to
avoid circular self-digestion. It accepts the binding only for a claim derived
true from those complete committed histories. Bare `program:<claim-id>`,
a binding to another input or history root, or substitution for a final-gate
receipt is invalid.

Authorization summaries, every authorization boundary, and
`program-complete.7` MUST instead reference the committed
`final-gate:structural-validation` evidence record. That record's independently
captured runner receipt, record and receipt commits, exact SHA-256 digests,
tested revision, gate-execution revision, and input root are the evidence that
the validator accepted the complete authorization, transition, and assignment
topology histories. A `program:` projection for any of these claims is
synthetic self-certification and is invalid.

Every `history_event_ids` entry names exactly one committed coordinator capture
of a tool-visible blocked observation in the current thread. The IDs are in
observation order and unique; their capture ordinals MUST
be consecutive, MUST precede the terminal blocked-audit event, and their
timestamps MUST NOT follow it. Each observation's claims bind the same
`persistent_goal_id`, execution identity, and canonical `blocked_state`; that
state is the threshold object with `required_count`, `observed_count`, and
`history_event_ids` removed. Its content is the canonical compact JSON for that
exact state. `observed_count` MUST equal the number of resolved observation
captures and `required_count` MUST be positive. A count without those exact
committed captures is not threshold evidence. This proves repository
persistence, not a platform-wide blocked-turn audit.

## No-push coordinator assertion

| Program base | Final input revision | Git-observable scope | Coordinator event binding | Outcome | Recorded at |
| --- | --- | --- | --- | --- | --- |

The coordinator MUST still not push. It records at most one explicit assertion
over the interval from `Program base` through `Final input revision`, but the
row is not a command audit and is not completion evidence. The scope is
canonical compact JSON with ordered fields `remote_names`, `refspecs`,
`observed_local_branch`, and `observed_local_head`; remote/ref arrays are
nonempty, sorted, and unique, remotes equal the locally configured repository
remotes, refspecs are exactly `["refs/*"]`, and the local branch/head equal the
Git state observed by the validator. `Outcome` is exactly
`coordinator-asserts-no-push`. The event is coordinator-authored and MUST use
the same assertion text and bounded Git facts. It MUST expose
`assertion_verified=false` and the literal limitation
`no-complete-command-audit-or-remote-non-delivery-proof`. Neither this row nor
its zero/absence claim may satisfy a verified terminal requirement. It does not
prove complete command history, remote state, network non-delivery, or actions
by another actor; the final report MUST surface that limitation unchanged.

## Worker assignment authorizations

| Unit | Generation | Integration baseline | Assignment commit | Assignment goal path | Rendered prompt | Prompt digest | Model | Reasoning | Fork turns | Subagents | Package scope | Reserved descendants | Goal digest | Authorized by | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

Each proposed worker spawn MUST have exactly one durable `authorized` row
before spawn. This row is pre-spawn authorization, not proof of the runtime
visible through the collaboration tool. The coordinator MUST commit the complete rendered prompt bytes at
the attributable repository path and bind their exact SHA-256 digest, unit,
generation, integration baseline, assignment commit, immutable assignment-time
goal path, `gpt-5.6-sol`, `medium`,
`none` fork turns, `false` subagents, exact canonical package scope, complete
reserved-descendant set, pinned goal-body digest, and `coordinator`
authorization. Pending or missing authorization blocks spawn. The first commit
that adds the exact row and rendered prompt is the immutable assignment
authorization checkpoint. Returned-scope validation MUST validate the baseline-
to-assignment-authorization-to-runtime-capture-to-release-row envelope
separately and MUST validate only the release-row-checkpoint-to-tip range as
worker-authored package work, with explicit allowance only for the mechanical
merges that import those exact coordinator checkpoints.

## Worker runtime attestations

The historical heading is retained for table compatibility; rows are
coordinator captures, not cryptographic runtime attestations.

| Unit | Generation | Worker task | Agent ID | Model | Reasoning | Fork turns | Subagents | Platform event binding | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

Immediately after the exact readiness receipt from the spawn turn, the
coordinator MUST append exactly one row from the tool-visible spawn result.
`Worker task` MUST be returned by the collaboration tool. `Agent ID` is the
coordinator's stable assignment-local identifier when the tool does not expose
one; it MUST NOT be described as immutable platform identity.
`Model`, `Reasoning`, `Fork turns`, and `Subagents` MUST equal the requested
values `gpt-5.6-sol`, `medium`, `none`, and `false`. `Platform event binding`
MUST bind one canonical coordinator capture of that result, including its
thread ID, worker task, agent ID, settings, timestamp, and exact content digest.
The coordinator
MUST commit this row and merge that exact runtime-capture commit into the
worker branch before authorizing package work. Worker self-report is not a
substitute. A missing, duplicate, identity-drifted, or settings-drifted runtime
row blocks release, worker-return acceptance, and integration. The worker MUST
remain readiness-only until the committed capture and release row are merged
and the later activation directive is sent.

## Worker release handshakes

| Unit | Generation | Worker task | Readiness event binding | Assignment authorization checkpoint | Runtime-attestation commit | Release event binding | Released at |
| --- | --- | --- | --- | --- | --- | --- | --- |

The first worker turn is readiness-only. `Readiness event binding` MUST bind a
coordinator capture of the tool-visible `worker-message` from the exact worker in
the recorded thread whose role is `assistant` and whose complete UTF-8 content
is exactly `READY-AND-WAITING <unit> g<generation>
<assignment-authorization-checkpoint>` with no prefix, suffix, or newline.
Before recording the runtime capture, the coordinator MUST validate that
capture against the base-pinned capture policy. After committing and merging
it, the coordinator sends the exact non-authorizing `PREPARE-RELEASE` directive
defined by `WORKER_PROMPT.md`. `Release event binding` captures the coordinator
send invocation/result for those exact bytes, same thread, worker, runtime capture, and
authorization checkpoint. The coordinator then commits the row, merges it into
the worker branch, and sends `ACTIVATE` naming that row commit. No worker-
authored package commit may precede that activation. An early worker mutation,
missing capture, duplicate capture ID, wrong actor/thread, preparation before runtime, or
identity/commit mismatch permanently
invalidates that assignment generation and requires safe abandonment; a later
row cannot retroactively authorize the early work.

The collaboration API provides no delivery receipt for `PREPARE-RELEASE` or `ACTIVATE`; the
validator therefore does not claim to prove delivery. The enforceable release
boundary is the worker branch containing the exact committed row plus the
worker's instruction to wait for the matching activation. Coordinator
acceptance MUST reject any worker-authored commit whose ancestry excludes that
row; the remaining send/receive ordering is an explicit tool limitation.

## Worker return attestations

| Unit | Generation | Worker task | Return event binding | Report binding | Worker tip | Ordered worker commits | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- |

The coordinator MUST record exactly one return row before accepting or
integrating worker output. `Return event binding` binds a coordinator capture
of the tool-visible `worker-message` from the assigned assistant in the recorded thread.
Its exact content bytes MUST equal the committed worker report named by
`Report binding`; the report binding is
`<repository-path>@<first-commit-containing-exact-bytes>@sha256:<digest>`.
The capture claims MUST bind the unit, generation, worker task, tool-visible agent ID,
assignment authorization checkpoint, runtime event ID, release event ID,
report binding, exact worker tip, and complete ordered worker-commit list.
The list is oldest to newest, contains exactly the worker-authored commits
reachable from the released checkpoint through the tip, and contains no
coordinator custody commit. The return capture ordinal MUST follow the PREPARE
send result. The worker asserts activation was observed; the API cannot prove
delivery. Missing, replayed, stale, or mismatched return capture
blocks acceptance and integration. This coordinator-authored capture is
custody evidence only; acceptance also requires independent diff review and
authoritative checks at the exact returned committed worker tip.

## Semantic authorization requests

| Approval ID | Proposal manifest binding | Affected closure | Status | User message event binding | Blocker or action | Recorded at |
| --- | --- | --- | --- | --- | --- | --- |

This append-only request lifecycle is `awaiting-user -> authorized -> applied`
or `awaiting-user -> superseded`. `Affected closure` is canonical compact JSON
with ordered keys `units`, `contract_ids`, `claim_ids`, `artifact_ids`, and
`source_paths`; every value is a bytewise sorted unique string array. Source
paths and contract IDs MUST equal the proposal manifest closure, and units,
claims, and artifacts MUST equal the transitive consumers derived from the
authoritative catalogs. Until an executable catalog-specific derivation proves
a narrower closure, `units` MUST conservatively contain all 67 program units;
the coordinator MUST NOT self-certify a smaller set. An
`awaiting-user` row uses `—` for the user event and
`blocker:platform-user-authority` until a valid event is available. While
awaiting authority, no affected unit may advance assignment, integration, or
verification state and no affected authoritative source may change; independent
lanes continue. Missing platform trust or an unverifiable user event never
falls back to coordinator-authored text and remains awaiting user authority.

## User semantic authorizations

| Approval ID | Proposal manifest binding | Change classes | Previous semantic root | Proposed semantic root | User message event binding | Status | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- |

This append-only table is the only execution-time authority for semantic
changes classified by `PROGRAM.md`. The coordinator MUST present the exact
canonical statement below and the user MUST send it back byte-for-byte as a
standalone line; conversational agreement or approval of a summary is not
authorization:

`AUTHORIZE identity-platform <approval-id> proposal=<repository-path>@<sha256-manifest-digest> previous=<sha256-root> proposed=<sha256-root> classes=<sorted-comma-separated-classes>`

`Approval ID` MUST be `user:<approval-id>` using the exact safe ID in that user
statement; `<approval-id>` MUST match `[a-zA-Z0-9._-]+`, and each root token
MUST be `sha256:<64-lowercase-hex>`. `Change classes` MUST be one or
more sorted values from `scope`, `ownership`, `dependency`, `public-api`,
`operation-semantics`, `parity`, `protocol`, `acceptance`, `configuration`,
`security`, `transaction`, `lifecycle`, or `goal`.

Before asking the user, the coordinator MUST commit one canonical proposal
manifest at
`.ai/identity-platform/evidence/semantic-proposals/<approval-id>.json` and every
full proposed source blob it names beneath
`.ai/identity-platform/evidence/semantic-proposals/<approval-id>/`. The manifest
uses schema `identity-platform.semantic-proposal.v1`, canonical UTF-8 JSON with
no insignificant whitespace or trailing newline, and ordered fields
`schema_version`, `schema`, `approval_id`, `change_classes`,
`previous_semantic_root`, `proposed_semantic_root`, and `entries`.
`schema_version` MUST be `1`; `approval_id` MUST be `user:<approval-id>`;
classes MUST be bytewise sorted and unique. Entries MUST be sorted and unique
by `source_path` and use ordered fields `source_path`, `contract_ids`,
`previous_content_sha256`, `proposed_blob_path`, and
`proposed_content_sha256`. Contract IDs MUST be exhaustive, bytewise sorted and
unique, using an empty array only when the complete file rather than named rows
is the semantic authority. Each source path MUST be a semantic source governed
by `PROGRAM.md`; each proposal blob path MUST remain inside that approval's
proposal directory and contain the complete exact replacement bytes for its
source, not a patch or summary.

At the proposal-manifest commit, every source MUST still equal its recorded
previous digest and every committed proposal blob MUST equal its proposed
digest. `previous_semantic_root` and `proposed_semantic_root` are SHA-256 roots
over canonical JSON projections with ordered fields `change_classes` and
`entries`; each projected entry uses ordered fields `source_path`,
`contract_ids`, and `content_sha256`, with the last field set to the
corresponding previous or proposed content digest. The line's proposal path,
manifest digest, roots, classes, and approval ID MUST reproduce the committed
manifest exactly. `Proposal manifest binding` records
`<path>@<first-commit-containing-exact-bytes>@sha256:<manifest-digest>`; that
commit MUST precede the authorized row. `User message event binding` MUST bind
the committed capture carrying the exact
standalone UTF-8 user line including no trailing newline. `Status` MUST be `authorized`,
`applied`, or `superseded`; an `applied` or `superseded` row requires an exact
earlier committed `authorized` row for the same approval and roots.

`User message event binding` MUST identify one committed coordinator capture
of the tool-visible `conversation-message` whose thread equals the recorded thread, role is
`user`, timestamp follows the exact proposal-manifest commit, and complete
UTF-8 content equals the standalone line byte-for-byte with no prefix, suffix,
or newline. The capture ID MUST be unique across the execution and its payload
digest and byte length MUST verify under the base-pinned capture policy. The
same binding is retained byte-for-byte in terminal rows. This establishes the
exact user-authority bytes used by the coordinator, not cryptographic platform
authenticity.

The coordinator MUST NOT create an `authorized` row unless the user explicitly
provided that exact user message binding the proposal manifest. Coordinator judgment, worker
findings, source control authorship, a validator repair, or a mechanically
generated digest is not authorization. The authorized row MUST be committed
while the previous semantic root is current; the applied row is committed only
after every authoritative source equals its exact committed proposed blob and
the proposed root is current. Any affected source or contract ID absent from
the user-bound manifest, any different proposed byte, or any unlisted semantic
byte change is unauthorized and blocks assignment, integration, verification,
and final acceptance for its affected closure. If the platform cannot supply a
verifiable user-message event, the request remains `awaiting-user`; authority is
unavailable rather than inferred.

## Tool and environment lanes

| Requirement/profile | Required by units or claims | Classification | Version/environment identity | Evidence path or blocking claim |
| --- | --- | --- | --- | --- |
| Go toolchain and repository gate tools | all units | pending | pending | pending |
| PostgreSQL profile | pending | pending | pending | pending |
| Valkey profile | pending | pending | pending | pending |
| race, fuzz, leak, and mutation tooling | pending | pending | pending | pending |
| browser and interoperability harnesses | pending | pending | pending | pending |

`Classification` MUST be exactly `available`, `unavailable`, or
`not-yet-needed`. Each unavailable row MUST name every acceptance claim it can
block. Availability is preflight information only and MUST NOT be reused as a
passing interoperability result.

## External evidence lanes

| Safe profile ID | Consuming units | Exact acceptance claim IDs | Classification | Credential source metadata | Evidence path or blocker | Evidence record commit | Evidence digest or blocker |
| --- | --- | --- | --- | --- | --- | --- | --- |
| pending | pending | pending | pending | pending | pending | pending | pending |

The coordinator MUST create one row per required external provider or
independent implementation. Consumer units and stable claim IDs MUST be exact,
sorted, and unique. Credential source metadata records presence and a
safe source identifier only. It MUST NOT contain the credential, its hash, a
prefix, or a provider response. A unit with any row here MUST use the lane's
attributable JSON evidence-record path before becoming `verified`; `not-needed`,
`available`, and blockers are not acceptance evidence. The digest MUST bind the
exact committed record bytes. `Evidence record commit` MUST be an existing
commit that contains those exact bytes at the recorded repository path and is
an ancestor of the coordinator commit that marks a consumer verified.
Working-tree-only, untracked, modified-after-commit, or path-exists-only
evidence is invalid. Schema version 2 records MUST contain an RFC3339 time no
earlier than preflight, exact profile, claim and unit attribution, and a passing
unit result with distinct `tested_revision`, `gate_execution_revision`, and
`revalidation_revision` fields, a complete input manifest and canonical input root,
tool/environment identity, and nonempty sanitized artifact SHA-256 hashes. The
revalidation field MUST be null when the tested and gate revisions are equal.
When they differ, it MUST equal the gate execution revision, and validation
MUST recompute the same complete input root from both committed trees before
reusing the original result.
Each external record's artifact hashes MUST bind its separately committed
coordinator execution receipt by path and byte digest. That receipt MUST
contain the same immutable direct-execution, tool-binary, environment,
stdout/stderr, raw-capture, verifier, and final-artifact fields required for a
local gate. The artifact-specific verifier MUST be independent of the consuming
package worker. Protocol and security claims MUST additionally bind the
selected independent suite or implementation, or a separately owned verifier
over raw observations when the protocol manifest selects no external suite.
Producer self-reports, mocks, package-only checks, and unbound provider logs are
not passing external evidence.

The record MUST NOT embed its own containing commit; this table and the ledger bind
that later commit. Every verified unit result MUST match its recorded gate
execution revision and gate fingerprint, while the integration checkpoint
remains a separate worker-merge identity.

## Existing primitive contracts

| Primitive | Consuming units | Registered module/package | API input fingerprint | Gate fingerprint and result | Evidence path |
| --- | --- | --- | --- | --- | --- |
| pending | pending | pending | pending | pending | pending |

The coordinator MUST derive each primitive's consuming units as the exact,
sorted, unique union of every goal's `Consumes existing primitives` section.
A fingerprint MUST cover the complete behavior-affecting inputs required by
repository evidence policy. A `pass` result permits the normal unit lifecycle.
A `failed`, `blocked`, or `stale` result permits a consumer to remain only
`proposed` with owner `—`, before assignment, or `blocked` as a paused retained
assignment with the exact owner claim
`blocker:primitive-<primitive-with-slashes-replaced-by-hyphens>`. Such a
consumer MUST NOT be `ready`, `in-progress`, `implemented-unverified`, or
`verified`, and MUST NOT receive new work or assignment until the primitive
result is `pass`. Such a unit is explicitly excluded from dependency-frontier
promotion even when every unit in its inventory `Requires` list is `verified`;
primitive start gates are additional to, and not represented by, DAG edges.

## Task-owned resource registry

| Resource ID | Type | Owning unit/task | Exact path or safe external ID | State | Cleanup trigger | Last reconciled at | Cleanup evidence or attestation |
| --- | --- | --- | --- | --- | --- | --- | --- |
| pending | pending | coordinator | pending | pending | pending | pending | pending |

Every worktree, disposable cache, temporary directory, process, container,
image, volume, database payload, browser artifact, and provider fixture created
for the program MUST be registered before use. The integration worktree is the
only bootstrap exception: it MUST be registered immediately after creation and
workspace verification, before any operation other than completing and
committing this preflight record. `State` MUST be `active`,
`retained-for-recovery`, `removal-pending-after-final-commit`, or `removed`.
Only the integration worktree MAY use
`removal-pending-after-final-commit`, under the two-phase cleanup in
`ORCHESTRATOR_GOAL.md`; its `Cleanup trigger` MUST be exactly
`user-authorized-post-report-removal`, expressing why it remains and who may
authorize physical removal. `Type` MUST be `worktree`, `go-cache`,
`temporary-directory`, `browser-artifact`, `process`, `container`, `image`,
`volume`, `database-payload`, or `provider-fixture`. Live local paths MUST
exist, removed local paths MUST be absent, and worktrees MUST additionally
match Git registration. Removed local resources MUST name an attributable
repository evidence path. External resources MUST name either an attributable
repository evidence path or the exact sanitized
`attestation:<state>:<safe-external-id>` for their current state. Evidence MUST
be captured into its durable record before the disposable source resource is
removed. Removed entries remain as sanitized audit records. A retained entry
MUST name an exact cleanup trigger; the coordinator MUST reconcile this table
after interruption and before final or blocked reporting. Once every inventory
unit is `verified`, every resource MUST be `removed` except the exact integration
worktree, which MAY remain `removal-pending-after-final-commit` until the final
record commit is created.

## Acceptance evidence bindings

| Artifact ID | Evidence path | Evidence blob digest | Evidence record commit | Bound at |
| --- | --- | --- | --- | --- |

At final acceptance this table MUST contain exactly one row for every
`END_STATE_ACCEPTANCE.json` artifact. The path and artifact ID MUST match the
catalog, the digest MUST bind the exact blob read from the named existing
commit, and that commit MUST descend from the payload's tested revision and be
an ancestor of the binding/finalization commit. The payload MUST NOT contain or
predict its own containing commit. Rows are append-only; replacement evidence
requires a new attributable artifact generation rather than rewriting a prior
binding.

Local package and reverse-dependant gate records use
`identity-platform.local-gate.v2`. Each record MUST contain the exact tested
revision, current gate execution revision, nullable revalidation revision,
exhaustive sorted behavior-input manifest, canonical input root, canonical
record path, commands, outcome, tool/environment identity, artifact hashes, and
record digest. The coordinator captures the clean committed integration `HEAD`
immediately before execution. On reuse, that current HEAD is both gate execution
and revalidation revision while tested revision remains the original execution;
the complete manifest and root MUST validate identically at both revisions.
The later append-only `EXECUTION_LEDGER.md` binding supplies the record commit
and exact canonical-byte digest without self-reference.

Every such record's artifact hashes MUST include a canonical coordinator
execution-receipt path and exact receipt-byte digest. The receipt MUST be
created from a direct coordinator-run subprocess capture and bind the absolute
executable path, executable version, executable-byte SHA-256, exact argument
vector, working directory, sorted redacted environment identity, start and
completion times, exit status, exact stdout/stderr bytes with lengths and
digests, raw-capture path and digest, artifact-specific verifier command and
capture, and final artifact path and digest. The coordinator MUST commit the
receipt and record before adding either ledger binding. A receipt supplied or
finalized by a package worker, producer, or final artifact is invalid.

Affected-package and reverse-dependant manifests MUST come from independent
repository-native discovery at the tested revision and be receipt-bound. For a
coverage or mutation gate, separate discovery receipts MUST capture the exact
gate with `ACCEPTANCE_DISCOVERY=affected-packages` and
`ACCEPTANCE_DISCOVERY=viable-mutants`; the mutation result receipt MUST also
bind the mutation tool's native machine-readable output. Worker-authored or
producer-normalized identity lists MUST NOT replace these captures.

Only `INVENTORY.md`, `EXECUTION_LEDGER.md`, this file, and files under
`.ai/identity-platform/evidence/` are explicitly non-behavioral execution or
evidence bookkeeping excluded from the behavior-input manifest. Their commit
ancestry and evidence bindings remain provenance. The authoritative current
`Requires` closure still selects module roots and `DEPENDENCIES.md` remains a
behavior input. All other selected
identity-platform files remain behavior inputs.

## Goal digest revisions

| Revision ID | Unit | Previous goal digest | Current goal digest | Status | Authorized by | Recorded at |
| --- | --- | --- | --- | --- | --- | --- |

Rows are append-only. Before changing any `GOAL_MANIFEST.json` digest, the
coordinator MUST append an `authorized` row while the previous digest is still
current. That row MUST name the exact matching `user:<safe-approval-id>` from a
prior committed `User semantic authorizations` row; `coordinator` is forbidden
in `Authorized by`. The later manifest-changing commit MUST append the matching
`applied` row with identical revision ID, unit, old/new digests, and user
approval; an abandoned change MUST instead append `superseded`. A terminal row
without its preceding user authorization is invalid. Revision IDs use
`goal:<unit-with-slashes-replaced-by-hyphens>:g<positive-sequence>` and each
digest is the exact `sha256:<hex>` identity of the goal body.
The first execution snapshot MAY omit prior fixtures only while every ledger
row is `initial` and every append-only execution-history table is empty. Every
later execution validation MUST receive the previous committed inventory,
ledger, execution snapshot, and goal manifest together through the four
`--previous-*-fixture` options. This lets the validator prove append-only goal
authorization and recovery lifecycles even when a candidate claims not to
change them.

## Conflict-recovery baselines

| Recovery epoch | Unit | Generation | Integration baseline | Authorization input root | Worker checkpoint | Authorization checkpoint | Conflict evidence path | Result worker commit | Result integration checkpoint | Status | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

Rows are append-only. `Recovery epoch` MUST be
`recovery:<unit-with-slashes-replaced-by-hyphens>:g<generation>:e<positive-sequence>`;
the sequence starts at one and increases by exactly one for each later recovery
of the same assignment generation. `Status` MUST be `authorized`, `effective`,
`superseded`, or `completed`. The `authorized` row MUST be committed with the
retained assignment's `blocked -> in-progress` transition, name the exact
integration baseline and worker checkpoint, record the repaired unit's
canonical complete-input root at that baseline, use `pending` for Authorization
checkpoint, and use `—` for both result commits. Its commit's first parent MUST
be the integration baseline. An immediate recovery-finalization `effective` row MUST bind
that authorization commit as Authorization checkpoint, copy every other
identity byte, and still use `—` for result commits. Only the committed
effective row authorizes the recovery merge and package edit.

An advanced integration `HEAD` does not by itself supersede an effective row.
Before retry, the coordinator MUST recompute the canonical complete-input
manifest and root for the repaired unit at the recorded baseline and advanced
`HEAD`. Byte-identical roots retain the effective authority; unequal roots
require `superseded` and the next epoch. Path comparison, ancestry, or a claim
that unrelated work advanced is insufficient. Successful integration appends a
`completed` row in a later commit and MUST bind the exact descendant result
worker commit and exact non-fast-forward result integration checkpoint. A
`superseded` row MUST retain `—` result commits. Every `effective`, `superseded`,
or `completed` row MUST be preceded by the matching `authorized` row for the
same epoch and exact commit identity in an earlier committed preflight
snapshot; authorization, effectuation, and a terminal row MUST be separate
commits. A later row cannot create or inherit authorization. Completion is terminal only for that epoch, not for
the assignment generation: if the retained assignment later re-enters
`blocked` because a repaired tip conflicts again, the coordinator MUST append
the next epoch and MAY authorize the new clean checkpoint only when the prior
checkpoint is its ancestor on the exact worker branch. The table contains only
sanitized evidence paths, not conflict contents or secrets.

## Integrated-repair authorizations

| Repair epoch | Unit | Generation | Integration baseline | Integration checkpoint | Worker checkpoint | Goal path | Goal digest | Rendered repair prompt | Prompt digest | Reserved descendants | Result worker commit | Result integration checkpoint | Status | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

Rows are append-only. A repair epoch MUST be
`repair:<unit-with-slashes-replaced-by-hyphens>:g<generation>:e<positive-sequence>`
and sequence independently within an assignment generation. An `authorized`
row MUST be committed with the exact `implemented-unverified -> in-progress`
transition, and its commit's first parent MUST equal `Integration baseline`.
It MUST bind the already integrated checkpoint, the clean worker-branch
checkpoint, the canonical current goal path, exact rendered repair-prompt bytes
and digest, the exact current goal-body digest, and the complete reserved nested-root set. The worker MUST merge
that exact authorization commit before editing. The coordinator MUST validate
the authorization-checkpoint-to-repaired-tip package-only range, integrate the
repair with a non-fast-forward merge, record the new integration checkpoint,
and append a matching `completed` terminal row in a later commit. An
authorization or `superseded` row MUST use `—` for both result fields. A
`completed` row MUST bind the exact repaired worker commit and resulting
non-fast-forward integration checkpoint, and the same commit MUST record those
values in the unit ledger. A terminal row MUST otherwise have an identical
epoch identity and MUST be preceded by its committed authorization. A changed baseline, checkpoint, goal, prompt, reservation set,
or worker checkpoint requires `superseded` followed by the next epoch; restart
recovery MUST reconstruct the active epoch from this table and Git ancestry.
