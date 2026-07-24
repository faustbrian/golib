# Integration

Normalize transport or model data outside the engine, then construct typed
facts. Keep the path vocabulary in the owning integration package. Do not pass
models for reflection-based discovery.

Persist canonical JSON plus its hash. Compile on write or controlled refresh,
not on every request. Cache only immutable plans and cap cache cardinality.

Authorization integrations retain subjects, resources, permissions, policy
combining, and deny defaults. Feature-flag integrations retain targeting,
percentage rollout, stickiness, prerequisites, and disabled defaults.
Validation integrations retain field attribution and error messages. Workflow
integrations retain state, actions, retries, and side effects.

In every integration, handle `Indeterminate` before `Matched` and map it to the
domain's explicit safe failure behavior.
