# Embedded web UI guide

The optional console is a same-origin client of the versioned administrative
API. Enable it with `QUEUE_CONTROL_UI_ENABLED=true`, start a serving replica,
and open `/ui/`. It adds no private transport, persistence, authorization, or
backend behavior.

## Start an operator session

Enter one tenant, API-key ID, and API key, then select **Connect**. The key is
held only in JavaScript memory, the password field is cleared immediately, and
neither local nor session storage is used. Reload the page to end the session.
Use a short-lived, least-privilege key delivered outside the browser history.

The subject behind the key still needs explicit `view` ACL entries for each
surface. The console cannot elevate permissions and displays the API's stable
error code when authentication, authorization, source availability, or bounds
reject a request.

## Diagnostic workflows

The tabs call the same bounded endpoints documented in the API reference:

- **Workers** shows identity, version, heartbeat-derived state, queues,
  concurrency, current jobs, drain status, backend, and compatibility.
- **Queues** shows backend-neutral current measurements. Read each
  `supported` flag before interpreting zero.
- **Failures** and **Dead letters** request `payload=hidden`; this console does
  not offer privileged payload reveal.
- **Audit** shows the append-only compatible tenant event history.
- **Commands** shows durable command envelopes and outcomes.

Select **Refresh** to replace the current bounded page. **Load next page** is
shown only when the API returns a continuation token. Changing tabs discards
the previous token so cursors never cross resources or tenants.

## Audited command workflow

The command panel covers every public action: pause, resume, drain, terminate,
retry, bulk retry, delete, purge, replay, and scale. Choose the action and
target, provide a concrete operator reason and unique idempotency key, then set
any action-specific fields:

- bulk retry requires a bounded selection limit;
- replay requires an explicit destination and duplicate policy;
- scale requires a replica count;
- destructive or otherwise guarded operations require confirmation.

The form generates a fresh UUID after connection and after each accepted
request. Reuse the original key only when retrying the exact same envelope.
When an outcome is unknown, inspect command history before issuing anything
new. An accepted command is evidence of durable admission, not proof that an
unavailable data-plane transport enforced it.

All critical inputs and buttons have accessible names and are keyboard
reachable. API-provided worker, queue, record, and error values are inserted as
text rather than markup. The Chromium gate injects executable-looking worker
values and verifies that no element or script is created.

## Security and troubleshooting

The UI policy permits scripts, styles, and connections only from the serving
origin and denies framing, referrers, MIME guessing, caching, plugins, and all
other resource classes. Deploy it behind TLS and the same ingress policy as the
API. Do not add inline scripts, third-party analytics, remote fonts, or browser
storage.

If a tab is unavailable, check `/v1/capabilities` and the production mappings.
If a command returns `dispatch_failed`, follow the incident guidance rather
than assuming a queue mutation occurred. Reload immediately if a workstation
or key may be compromised.
