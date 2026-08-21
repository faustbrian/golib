#!/usr/bin/env bash
set -euo pipefail

for heading in \
    '## Five-minute setup' \
    '## Cardinality and data policy' \
    '## Semantic conventions' \
    '## Trace timing and propagation boundary' \
    '## Failure and lifecycle behavior' \
    '## Metric reference' \
    '## Attribute reference and privacy' \
    '## API reference' \
    '## Migration' \
    '## FAQ' \
    '## Verification'
do
    grep -qF -- "${heading}" README.md
done

for required in \
    'MessagingSemanticConventionVersion' \
    'TraceContextPropagation' \
    'messaging.system' \
    'messaging.operation.name' \
    'messaging.operation.type' \
    'messaging.client.id' \
    'messaging.destination.name' \
    'messaging.consumer.group.name' \
    'messaging.client.operation.duration' \
    'kafka.client.operations' \
    'messaging.destination.partition.id' \
    'messaging.kafka.offset' \
    'error.type' \
    'kafka.operation' \
    'kafka.outcome' \
    'kafka.client.id' \
    'kafka.topic' \
    'kafka.consumer.group' \
    'kafka.broker.id' \
    'kafka.authentication.method' \
    'kafka.request.bytes' \
    'kafka.response.bytes' \
    'kafka.request.queue.duration' \
    'kafka.throttle.duration' \
    'kafka.throttled_after_response' \
    'kafka.record.count' \
    'kafka.partition.count' \
    'kafka.broker.count' \
    'kafka.topic.count' \
    'kafka.consumer_group.count' \
    'kafka.consumer_group.member.count' \
    'kafka.record.processed_count' \
    'kafka.record.committed_count' \
    'kafka.record.size' \
    'kafka.replay.processed' \
    'kafka.replay.skipped' \
    'kafka.replay.failed' \
    'kafka.replay.remaining' \
    'kafka.dependency.healthy' \
    'kafka.readiness.ready' \
    'kafka.readiness.consecutive_failures' \
    'kafka.readiness.consecutive_successes' \
    'kafka.observation.truncated' \
    'messaging.batch.message_count' \
    'kafka.protocol.api_key' \
    'kafka.request.direction'
do
    grep -qF -- "${required}" README.md
done

span_rows="$({
    awk '
        /^\| Kafka observation \|/ { in_table = 1; next }
        in_table && /^$/ { exit }
        in_table && /^\|/ && $0 !~ /^\| ---/ { rows++ }
        END { print rows + 0 }
    ' README.md
})"
test "${span_rows}" -ge 35

go doc . >/dev/null
go test ./... -run '^Example' -count=1
