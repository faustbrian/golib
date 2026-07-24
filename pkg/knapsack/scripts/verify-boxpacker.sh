#!/bin/sh
set -eu

composer install --working-dir integration/boxpacker --no-dev --no-interaction \
	--prefer-dist --no-progress --no-scripts
BOXPACKER_INTEGRATION=1 go test . -run '^TestBoxPackerCommonSubset$' -count=1
