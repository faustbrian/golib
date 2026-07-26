#!/usr/bin/env bash
set -euo pipefail

fuzz_time="${1:-${FUZZ_TIME:-2s}}"
fuzz_workers="${FUZZ_WORKERS:-1}"
targets=(
  "./job:FuzzDecodeE"
  "./job:FuzzMessageValidation"
  "./managementhttp:FuzzStatusHandlerFailsClosed"
  "./managementhttp:FuzzHandlerCommandBody"
  "./nats:FuzzRequestDelivery"
  "./nsq:FuzzRequestDelivery"
  "./rabbitmq:FuzzRequestDelivery"
  "./redisdb:FuzzRequestDelivery"
  "./redisstream:FuzzRequestDelivery"
  "./valkeystream:FuzzDeliveryEnvelope"
  "./valkeystream:FuzzOptions"
  "./valkeystream:FuzzNativeResponses"
  "./valkeystream:FuzzSettlementStateTransitions"
)

for target in "${targets[@]}"; do
  package="${target%%:*}"
  name="${target#*:}"
  go test "$package" -run '^$' -fuzz "^${name}$" -fuzztime "$fuzz_time" -parallel="$fuzz_workers"
done
