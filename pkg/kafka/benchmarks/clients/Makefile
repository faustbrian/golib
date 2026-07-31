GO ?= go
BENCH_COUNT ?= 10
BENCH_TIME ?= 10x
BENCH_PATTERN ?= ^BenchmarkEquivalent(Synchronous(Produce|BatchProduce)|AsynchronousProduce|MultiPartitionProduce|ConsumerHandling|CrossPartitionConsumerHandling|TransactionalProduce|ConsumeTransformProduce|Replay|Inspection)$$
REBALANCE_COUNT ?= 10
REBALANCE_TIME ?= 1x
REBALANCE_PATTERN ?= ^BenchmarkEquivalentConsumerRebalance$$
RESOURCE_COUNT ?= 10
RESOURCE_TIME ?= 1x
RESOURCE_PATTERN ?= ^BenchmarkEquivalentInspection(Reconnect|IdleResources)$$
TLS_COUNT ?= 10
TLS_TIME ?= 100x
TLS_PATTERN ?= ^BenchmarkEquivalentTLS(SynchronousProduce|ConnectProduceClose)$$

.PHONY: analyze benchmark capture environment rebalance-benchmark rebalance-capture resource-benchmark resource-capture test tls-benchmark tls-capture verify

test:
	@$(GO) test ./...

verify:
	@$(GO) test -tags=integration -run '^Test(Equivalent(Producer(Batch)?|AsynchronousProducer|MultiPartitionProducer|Consumer|CrossPartitionConsumer|ConsumerRebalance|TLSProducer|TransactionalProducer|ConsumeTransformProduce|Replay|Inspection|InspectionReconnect|InspectionIdleResource)Outcomes|Benchmark(MultiPartitionRecordBytes|ConsumerOperationBytes|ConsumerTopicReuse|CrossPartitionOperationBytes)|RunCrossPartitionHandlersConcurrency|TransactionalBenchmarkRecordBytes)$$' -count=1

benchmark:
	@$(GO) test -tags=integration -run '^$$' -bench '$(BENCH_PATTERN)' \
		-benchmem -benchtime='$(BENCH_TIME)' -count='$(BENCH_COUNT)'

capture:
	@test -n '$(OUTPUT)'
	@$(GO) test -tags=integration -run '^$$' -bench '$(BENCH_PATTERN)' \
		-benchmem -benchtime='$(BENCH_TIME)' -count='$(BENCH_COUNT)' \
		> '$(OUTPUT)'
	@cat '$(OUTPUT)'

rebalance-benchmark:
	@$(GO) test -tags=integration -run '^$$' -bench '$(REBALANCE_PATTERN)' \
		-benchtime='$(REBALANCE_TIME)' -count='$(REBALANCE_COUNT)'

rebalance-capture:
	@test -n '$(OUTPUT)'
	@$(GO) test -tags=integration -run '^$$' -bench '$(REBALANCE_PATTERN)' \
		-benchtime='$(REBALANCE_TIME)' -count='$(REBALANCE_COUNT)' \
		> '$(OUTPUT)'
	@cat '$(OUTPUT)'

resource-benchmark:
	@$(GO) test -tags=integration -run '^$$' -bench '$(RESOURCE_PATTERN)' \
		-benchtime='$(RESOURCE_TIME)' -count='$(RESOURCE_COUNT)'

resource-capture:
	@test -n '$(OUTPUT)'
	@$(GO) test -tags=integration -run '^$$' -bench '$(RESOURCE_PATTERN)' \
		-benchtime='$(RESOURCE_TIME)' -count='$(RESOURCE_COUNT)' \
		> '$(OUTPUT)'
	@cat '$(OUTPUT)'

tls-benchmark:
	@$(GO) test -tags=integration -run '^$$' -bench '$(TLS_PATTERN)' \
		-benchmem -benchtime='$(TLS_TIME)' -count='$(TLS_COUNT)'

tls-capture:
	@test -n '$(OUTPUT)'
	@$(GO) test -tags=integration -run '^$$' -bench '$(TLS_PATTERN)' \
		-benchmem -benchtime='$(TLS_TIME)' -count='$(TLS_COUNT)' \
		> '$(OUTPUT)'
	@cat '$(OUTPUT)'

analyze:
	@test -n '$(INPUT)'
	@cd '$(dir $(INPUT))' && $(GO) tool benchstat '$(notdir $(INPUT))'

environment:
	@$(GO) version
	@$(GO) env GOOS GOARCH GOARM64
	@uname -srm
	@git rev-parse HEAD
	@docker version --format \
		'client={{.Client.Version}} server={{.Server.Version}} api={{.Server.APIVersion}}'
	@find . -maxdepth 1 -type f \( \
		-name '*.go' -o -name 'go.mod' -o -name 'go.sum' -o \
		-name 'Makefile' -o -name 'README.md' \
	\) -print | sed 's|^./||' | LC_ALL=C sort | xargs shasum -a 256
	@shasum -a 256 ../../../../go.work
	@$(GO) list -m \
		github.com/faustbrian/golib/pkg/kafka \
		github.com/twmb/franz-go \
		github.com/segmentio/kafka-go \
		github.com/IBM/sarama \
		github.com/testcontainers/testcontainers-go/modules/kafka \
		golang.org/x/perf
	@echo 'broker-image=confluentinc/confluent-local:7.5.0@sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd'
	@echo 'tls-broker-image=apache/kafka:4.3.1@sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837'
