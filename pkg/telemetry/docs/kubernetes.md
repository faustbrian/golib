# Kubernetes adoption

## Topology

Send OTLP to a Collector sidecar, node agent, or cluster service. A cluster
service is the simplest default; an agent reduces cross-node traffic; a
sidecar provides the strongest per-pod isolation at higher resource cost.

Telemetry availability must not gate application readiness. The runtime starts
without dialing synchronously, exporters have finite queues and retry elapsed
time, and readiness should describe the application dependency graph rather
than the Collector.

## Application configuration

The library does not read environment variables. Map deployment values into
`Config` explicitly:

```go
config := telemetry.DefaultConfig("orders", buildVersion)
config.Environment = "production"
config.Service.Namespace = "commerce"
config.Service.Instance = os.Getenv("POD_NAME")
config.Traces.Exporter.Endpoint = os.Getenv("OTEL_COLLECTOR_ENDPOINT")
config.Metrics.Exporter.Endpoint = os.Getenv("OTEL_COLLECTOR_ENDPOINT")
```

Use the downward API for a stable pod instance identifier. Do not add pod UID,
request ID, user ID, order ID, or container restart count as metric attributes.
Resource attributes are attached once per process and must still avoid secrets.

## Deployment example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orders
spec:
  replicas: 3
  selector:
    matchLabels: {app: orders}
  template:
    metadata:
      labels: {app: orders}
    spec:
      terminationGracePeriodSeconds: 30
      containers:
        - name: orders
          image: example/orders:1.2.3
          env:
            - name: OTEL_COLLECTOR_ENDPOINT
              value: otel-collector.observability.svc:4317
            - name: POD_NAME
              valueFrom:
                fieldRef: {fieldPath: metadata.name}
          ports:
            - {name: http, containerPort: 8080}
          resources:
            requests: {cpu: 100m, memory: 128Mi}
            limits: {memory: 256Mi}
```

Set the pod grace period longer than HTTP/worker drain plus telemetry shutdown.
Stop new work first, wait for in-flight work, then call `Runtime.Shutdown` with
a deadline shorter than Kubernetes' remaining grace period.

## TLS and credentials

For a same-cluster plaintext endpoint, retain `TLS.Insecure = true` only when
network policy and workload identity protect the path. Otherwise mount a CA
and optional client certificate from a Secret, set `TLS.Insecure = false`, and
set `CAFile`, `CertificateFile`, `PrivateKeyFile`, and `ServerName`. Plaintext
mode rejects TLS-only fields so credentials cannot be silently ignored. Never
place bearer tokens in resource attributes; use exporter headers sourced from
a Secret.

## Capacity

Budget process memory for the 2,048-span queue, metric cardinality, SDK state,
and connection buffers. Tune only after measuring the included benchmarks and
Collector rejection metrics. Keep queues finite and prefer Collector buffering
for sustained outages.
