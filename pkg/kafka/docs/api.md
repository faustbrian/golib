# API

Use `NewProducer`, `NewConsumer`, `NewReplayReader`, and `NewInspector` as the
composition roots. Every constructor validates identities and bounded resource
policy before franz-go is configured.

`Producer.Publish` is synchronous. `Consumer.RunOnce` returns one bounded poll
result. `Consumer.Run` exits cleanly when its context is canceled.
`ReplayReader.Replay` completes only after every requested offset is processed.
`Inspector.Topics` and `Inspector.ConsumerGroupLag` require explicit bounded
target lists.

The canonical machine-checked exported API is in `api/baseline.txt`.
