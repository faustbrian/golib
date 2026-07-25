# Backend support

| Capability | Ring | Redis Pub/Sub | Redis Streams | Valkey Streams | Core NATS | NSQ | RabbitMQ |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Persistent broker storage | No | No | Yes | Yes | No | Yes | Yes |
| Explicit ack after handler | N/A | No | Yes | Yes | No | Yes | Yes |
| Consumer groups | Process only | No | Yes | Yes | Queue group | Channel | Queue |
| Strict global ordering | No | No | Per stream, affected by groups | Per stream, affected by groups | No | No | Per queue, affected by consumers |
| Native delayed delivery | No | No | No | No | No | Requeue delay | TTL/DLX, not wrapped |
| Pending reclaim | N/A | No | Bounded `XAUTOCLAIM` | Bounded `XAUTOCLAIM` | No | Server-managed | Broker requeue |
| Terminal dead letter | N/A | No | Append-before-ack stream | Append-before-ack stream | No | Package-owned terminal publish before FIN | Confirmed package-owned terminal publish before source ack |
| Failure/dead-letter management | No | No | List, inspect, retry, bulk retry, replay to allowlisted streams, delete, record purge | List, inspect, retry, bulk retry, replay to allowlisted streams, delete, record purge | No | No | No |
| Depth available | In process | No | Redis commands | Valkey group stats | Server monitoring | Stats | Management API |
| Maximum encoded delivery | 1 MiB through queue API | 1 MiB | 1 MiB | 1 MiB | 1 MiB | 1 MiB | 1 MiB |
| Confirmed publish | In process | Redis command result | Redis command result | Valkey command result | Client write only | nsqd response | Publisher confirm |
| Same-worker reconnect | N/A | Yes, without replay | Client reconnect; backlog retained | Subsequent commands reconnect | Yes, without replay | Yes | No; replace worker |
| Proven topology | Process | Standalone, Sentinel, cluster | Standalone, cluster | Standalone Valkey 9 only | Core server | nsqd | Standalone broker |

“Supported” means the implementation is owned in this module. It does not mean
identical guarantees. Redis Streams and Valkey Streams remain separate durable
paths with native clients. Redis Pub/Sub is supported for transient delivery
only. No Valkey cluster or managed-failover guarantee is implied.
