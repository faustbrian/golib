GO ?= go
BENCH_COUNT ?= 10
BENCH_TIME ?= 10x
BENCH_PATTERN ?= ^BenchmarkEquivalent(Synchronous(Produce|BatchProduce)|AsynchronousProduce|MultiPartitionProduce|ConsumerHandling)$$

.PHONY: analyze benchmark capture environment test verify

test:
	@$(GO) test ./...

verify:
	@$(GO) test -tags=integration -run '^Test(Equivalent(Producer(Batch)?|AsynchronousProducer|MultiPartitionProducer|Consumer)Outcomes|Benchmark(MultiPartitionRecordBytes|ConsumerOperationBytes|ConsumerTopicReuse))$$' -count=1

benchmark:
	@$(GO) test -tags=integration -run '^$$' -bench '$(BENCH_PATTERN)' \
		-benchmem -benchtime='$(BENCH_TIME)' -count='$(BENCH_COUNT)'

capture:
	@test -n '$(OUTPUT)'
	@$(GO) test -tags=integration -run '^$$' -bench '$(BENCH_PATTERN)' \
		-benchmem -benchtime='$(BENCH_TIME)' -count='$(BENCH_COUNT)' \
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
