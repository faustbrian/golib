#!/usr/bin/env bash
set -euo pipefail

go test ./queueservice ./redisstream ./valkeystream \
  -run 'Test(RedisStreamStatsRemainSourceCompatible|ValkeyStreamStatsRemainPackageOwned)$'
