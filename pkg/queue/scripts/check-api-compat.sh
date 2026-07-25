#!/usr/bin/env bash
set -euo pipefail

go test ./redisstream ./valkeystream \
  -run 'Test(RedisStreamStatsRemainSourceCompatible|ValkeyStreamStatsRemainPackageOwned)$'
