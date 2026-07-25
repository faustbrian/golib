# Kubernetes and Infisical recipes

## ConfigMap and Secret environment variables

Inject ConfigMap and Secret keys normally, then use
`environment.ProcessFor[T]`. Environment variables are fixed for the process
lifetime; a Kubernetes Secret or ConfigMap update requires a workload restart
to change them. Readiness should remain false until `config.Load` and all
validators succeed.

```yaml
envFrom:
  - configMapRef:
      name: worker-config
  - secretRef:
      name: worker-secrets
```

The default plan places process environment above dotenv and files. Do not copy
the complete environment into diagnostics.

## Mounted ConfigMap or Secret

Mount a complete JSON, YAML, or TOML document read-only and use
`filesystem.FromPath`. Each load reopens the path, so Kubernetes atomic symlink
replacement is observed. A reload controller must rebuild and validate the
entire plan; it must not patch fields in an existing snapshot. Automatic hot
reload is intentionally outside v1.

```yaml
volumeMounts:
  - name: configuration
    mountPath: /var/run/app-config
    readOnly: true
volumes:
  - name: configuration
    projected:
      sources:
        - configMap:
            name: worker-config
        - secret:
            name: worker-secrets
```

Use separate documents when ConfigMap and Secret ownership differs, and mark the
secret source sensitive. Set parser limits for the expected document size.

## Infisical Operator

The Operator is the preferred boot-only path when Infisical values should
become Kubernetes Secrets or environment variables. Configure the Operator to
synchronize the Secret and enable its workload auto-reload annotation when
rotations must trigger a rolling restart. `config` has no Infisical
dependency in this mode and performs an ordinary boot load.

Authentication, machine identity, token renewal, secret rotation, and Infisical
outages remain platform concerns. Required missing or invalid values still fail
application startup closed.

## Infisical CSI

Use CSI when static secret changes should appear as mounted files without
creating Kubernetes Secret objects. Enable Secrets Store CSI rotation and
choose/document its poll interval and expected propagation delay. Infisical CSI
currently applies to static secrets.

Consume the mounted complete document through `filesystem.FromPath`. If the
application adds reload orchestration, combine filesystem events with periodic
reconciliation, debounce bursts, and discard candidates from partial/truncated
reads. Publish only a completely loaded and validated replacement snapshot.

## Agent Injector

Agent-rendered shared-volume files follow the same mounted-file contract. The
template must produce one strict supported format and replace it atomically.
Agent authentication, renewal, rendering, and outage behavior are not core
library responsibilities.

## Native adapter status

There is no native Infisical adapter in this module. If one is later justified
for non-Kubernetes tools or services, it must live in a separately imported and
preferably separately versioned module so the official SDK never inflates the
core dependency graph.
